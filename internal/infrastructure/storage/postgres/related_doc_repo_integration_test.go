//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/application/readaccess"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain"
	"metapus/internal/metadata"
)

func TestRelatedDocRepoStopsAtUnregisteredBasis(t *testing.T) {
	ctx, txm := relatedDocIntegrationContext(t)
	errRollback := errors.New("rollback related document fixture")
	var result *domain.RelatedDocumentsResult

	err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		documentID := id.New()
		_, err := txm.GetQuerier(txCtx).Exec(txCtx, `
			INSERT INTO doc_crypto_sweeps(
				id, number, token_id, hot_wallet_id, basis_type, basis_id
			) VALUES ($1, 'CS-RELATED-1', $2, $3, 'InternalSweepWorkflow', $4)
		`, documentID, id.New(), id.New(), id.New())
		if err != nil {
			return err
		}

		registry := metadata.NewRegistry()
		registry.Register(cryptoSweepRelatedDef())
		txCtx = readaccess.WithService(appctx.WithUser(txCtx, &appctx.UserContext{UserID: id.New().String(), Permissions: []string{"document:crypto_sweep:read"}}), NewAuthorizedReadService(registry, nil))
		result, err = NewRelatedDocRepo(registry).FindRelatedDocuments(txCtx, domain.RelatedDocumentsRequest{
			EntityName: "CryptoSweep",
			EntityID:   documentID,
		})
		if err != nil {
			return err
		}
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
	require.NotNil(t, result)
	require.NotNil(t, result.Tree)
	require.Equal(t, "CryptoSweep", result.Tree.EntityName)
	require.True(t, result.Tree.IsCurrent)
	require.Empty(t, result.Tree.Children)
}

func TestRelatedDocRepoReturnsChildQueryErrors(t *testing.T) {
	ctx, txm := relatedDocIntegrationContext(t)
	errRollback := errors.New("rollback related document fixture")
	var findErr error

	err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		documentID := id.New()
		_, err := txm.GetQuerier(txCtx).Exec(txCtx, `
			INSERT INTO doc_crypto_sweeps(id, number, token_id, hot_wallet_id)
			VALUES ($1, 'CS-RELATED-2', $2, $3)
		`, documentID, id.New(), id.New())
		if err != nil {
			return err
		}

		registry := metadata.NewRegistry()
		registry.Register(cryptoSweepRelatedDef())
		registry.Register(metadata.EntityDef{
			Name:        "MissingDocument",
			Key:         "missing_document",
			Type:        metadata.TypeDocument,
			TableName:   "doc_missing_related_documents",
			RoutePrefix: "missing-document",
		})
		_, findErr = NewRelatedDocRepo(registry).FindRelatedDocuments(txCtx, domain.RelatedDocumentsRequest{
			EntityName: "CryptoSweep",
			EntityID:   documentID,
		})
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
	require.ErrorContains(t, findErr, "query related document children")
}

func relatedDocIntegrationContext(t *testing.T) (context.Context, *TxManager) {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := NewTxManagerFromRawPool(pool)
	return tenant.WithTxManager(ctx, txm), txm
}

func cryptoSweepRelatedDef() metadata.EntityDef {
	return metadata.EntityDef{
		Name:           "CryptoSweep",
		Key:            "crypto_sweep",
		Type:           metadata.TypeDocument,
		TableName:      "doc_crypto_sweeps",
		RoutePrefix:    "crypto-sweep",
		ReadPermission: "document:crypto_sweep:read",
		ReadModel:      relatedSweepPresentation{},
		Fields:         []metadata.FieldDef{{Name: "id", DBColumn: "id"}, {Name: "number", DBColumn: "number"}},
		Presentation: metadata.Presentation{
			Singular: "Крипто-свип",
			Plural:   "Крипто-свипы",
		},
	}
}

type relatedSweepPresentation struct {
	ID     id.ID  `db:"id" json:"id"`
	Number string `db:"number" json:"number"`
}

func (p *relatedSweepPresentation) GetID() id.ID { return p.ID }
