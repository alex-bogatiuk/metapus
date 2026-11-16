//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto/publicref"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

func TestInvoiceCheckoutCommitRollbackAndIdempotentDelivery(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	ctx := context.Background()
	newPool := func() *pgxpool.Pool {
		config, err := pgxpool.ParseConfig(dsn)
		require.NoError(t, err)
		config.MaxConns = 1 // Keep each TEMP table on its own session.
		pool, err := pgxpool.NewWithConfig(ctx, config)
		require.NoError(t, err)
		t.Cleanup(pool.Close)
		return pool
	}
	tenantPool, metaPool := newPool(), newPool()
	// TEMP tables isolate the fixture from existing invoices and registry rows.
	_, err := tenantPool.Exec(ctx, `CREATE TEMP TABLE sys_outbox (
		id UUID PRIMARY KEY,aggregate_type TEXT,aggregate_id UUID,event_type TEXT,
		payload JSONB,status TEXT,created_at TIMESTAMPTZ,traceparent TEXT,
		correlation_id TEXT,causation_id TEXT)`)
	require.NoError(t, err)
	_, err = metaPool.Exec(ctx, `CREATE TEMP TABLE public_checkout_refs (
		ref_hash TEXT PRIMARY KEY,ref_prefix TEXT,tenant_id UUID,merchant_id UUID,
		resource_type TEXT,resource_id UUID,status TEXT,expires_at TIMESTAMPTZ,
		updated_at TIMESTAMPTZ DEFAULT NOW(),
		CONSTRAINT uq_public_checkout_refs_resource UNIQUE(tenant_id,resource_type,resource_id))`)
	require.NoError(t, err)
	txm := postgres.NewTxManagerFromRawPool(tenantPool)
	ctx = tenant.WithTxManager(ctx, txm)
	ctx = tenant.WithTenant(ctx, &tenant.Tenant{ID: id.New().String()})
	invoice := crypto_invoice.NewCryptoInvoice(id.New(), id.New(), types.NewRawAmountFromInt64(1000000))
	publisher := postgres.NewOutboxPublisher()
	rollback := errors.New("rollback invoice transaction")
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		require.NoError(t, publishInvoiceCheckout(txCtx, publisher, invoice))
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	var count int
	require.NoError(t, tenantPool.QueryRow(ctx, `SELECT count(*) FROM sys_outbox`).Scan(&count))
	require.Zero(t, count, "rolled back invoice must not publish a reference")

	require.NoError(t, txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		return publishInvoiceCheckout(txCtx, publisher, invoice)
	}))
	var msg postgres.OutboxMessage
	require.NoError(t, tenantPool.QueryRow(ctx,
		`SELECT aggregate_type,aggregate_id,event_type,payload FROM sys_outbox`).Scan(
		&msg.AggregateType, &msg.AggregateID, &msg.EventType, &msg.Payload))
	refs := crypto_repo.NewCheckoutReferenceRepo(metaPool)
	require.NoError(t, NewInvoiceCheckoutHandler(refs).Handle(ctx, &msg))
	require.NoError(t, NewInvoiceCheckoutHandler(refs).Handle(ctx, &msg))
	hash, err := publicref.Hash(invoice.PublicRef)
	require.NoError(t, err)
	ref, err := refs.ResolveActive(ctx, hash)
	require.NoError(t, err)
	require.NotNil(t, ref)
	require.Equal(t, invoice.ID, ref.ResourceID)
	require.Equal(t, invoice.MerchantID, ref.MerchantID)
	require.Equal(t, tenant.GetTenantID(ctx), ref.TenantID)
	require.Nil(t, ref.ExpiresAt)
	require.NoError(t, metaPool.QueryRow(ctx, `SELECT count(*) FROM public_checkout_refs`).Scan(&count))
	require.Equal(t, 1, count, "redelivery must not duplicate registry rows")
}
