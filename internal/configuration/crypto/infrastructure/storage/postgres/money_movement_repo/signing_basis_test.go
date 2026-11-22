package money_movement_repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/pkg/sdk/id"
)

func TestSigningBasisRejectsUntrustedIdentifiersBeforeDatabaseAccess(t *testing.T) {
	repo := New()
	// No tenant transaction manager is configured: rejection must precede SQL.
	ctx := context.Background()
	for _, basis := range []string{"", "doc_withdrawal_requests; DROP TABLE cat_wallets", "CryptoInvoice"} {
		t.Run(basis, func(t *testing.T) {
			_, err := repo.SigningBasisStatus(ctx, basis, id.New(), false)
			require.ErrorContains(t, err, "unsupported signing job basis type")
			_, err = repo.SigningBasisStatus(ctx, basis, id.New(), true)
			require.ErrorContains(t, err, "unsupported signing job basis type")
			err = repo.PublishSigningBasis(ctx, basis, id.New(), "approved")
			require.ErrorContains(t, err, "unsupported signing job basis type")
		})
	}
	require.ErrorContains(t, repo.PublishSigningBasis(ctx, withdrawal_request.DocumentType, id.New(), "completed"), "invalid signing publication status")
}
