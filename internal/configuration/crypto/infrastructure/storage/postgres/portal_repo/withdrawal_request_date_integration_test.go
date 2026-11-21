//go:build integration

package portal_repo

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/storage/postgres"
)

// The debit-first movement is posted with the document date, and the
// execution guard (fn_crypto_assert_withdrawal_parent) requires
// period = request.date. The request row must therefore store the domain
// date, not the database clock of its insert.
func TestCreateWithdrawalRequestStoresTheDocumentDate(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_DATABASE_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)
	rollback := errors.New("rollback withdrawal date fixture")
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		var tokenID, networkID id.ID
		err := q.QueryRow(ctx, `SELECT id, network_id FROM cat_tokens WHERE _deleted_at IS NULL AND deletion_mark = FALSE ORDER BY created_at LIMIT 1`).Scan(&tokenID, &networkID)
		if errors.Is(err, pgx.ErrNoRows) {
			t.Skip("test database has no crypto token")
		}
		require.NoError(t, err)
		merchantID, addressID := id.New(), id.New()
		_, err = q.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Withdrawal date merchant')`, merchantID, "wd-"+id.Compact(merchantID))
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO reg_withdrawal_addresses(id,merchant_id,network_id,address,address_key) VALUES($1,$2,$3,'external-destination','external-destination')`, addressID, merchantID, networkID)
		require.NoError(t, err)

		doc := withdrawal_request.NewWithdrawalRequest(merchantID, tokenID, addressID, "external-destination", types.MustRawAmount("100"))
		doc.Number = "WR-" + id.Compact(doc.GetID())
		// A date the database clock can never produce for this insert.
		doc.Date = time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond).Add(123 * time.Microsecond)
		doc.Posted, doc.PostedVersion = true, 1
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_merchant_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
			VALUES(gen_random_uuid_v7(),$1,'WithdrawalRequest',1,$2,'expense',$3,$4,100)`, doc.GetID(), doc.Date, merchantID, tokenID)
		require.NoError(t, err)

		require.NoError(t, NewDashboardRepo().CreateWithdrawalRequest(ctx, doc, aml.Class("")))

		var stored time.Time
		var debitMatches bool
		require.NoError(t, q.QueryRow(ctx, `SELECT r.date, EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements m
			WHERE m.recorder_id=r.id AND m.recorder_type='WithdrawalRequest' AND m.record_type='expense' AND m.period=r.date)
			FROM doc_withdrawal_requests r WHERE r.id=$1`, doc.GetID()).Scan(&stored, &debitMatches))
		require.True(t, doc.Date.Equal(stored), "stored %s, document %s", stored, doc.Date)
		require.True(t, debitMatches, "the original debit must match the request date")
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
