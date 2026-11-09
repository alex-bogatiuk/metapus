//go:build integration

package crypto_repo

import (
	"context"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/crypto/aml"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// Test-only export lets the outbound adapter exercise the real AML fixture
// without introducing a production import cycle or bypassing financial guards.
type OutboundPublicationFixture struct {
	Ctx            context.Context
	Pool           *pgxpool.Pool
	Parent, Source id.ID
	Auth           crypto.SigningAuthorization
	Unsigned       crypto.UnsignedTxEnvelope
	Bundle         crypto.ApprovalBundle
}

func NewOutboundPublicationFixture(t *testing.T, pending ...bool) OutboundPublicationFixture {
	t.Helper()
	f := riskWithdrawalFixture(t, false)
	intent, err := reserveRiskWithdrawal(f, aml.Low, "20", false)
	require.NoError(t, err)
	if len(pending) == 0 || !pending[0] {
		_, err = f.pool.Exec(f.ctx, "UPDATE doc_withdrawal_requests SET status='approved' WHERE id=$1", intent.BasisID)
		require.NoError(t, err)
	}
	line := intent.SourceLines[0]
	var networkCode, networkIdentifier string
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT code,network_identifier FROM cat_blockchain_networks WHERE id=$1", f.input.Request.NetworkID).Scan(&networkCode, &networkIdentifier))
	bundle := crypto.ApprovalBundle{IntentJWS: "synthetic-intent"}
	digest, err := crypto.DigestJSON(bundle)
	require.NoError(t, err)
	auth := crypto.SigningAuthorization{SchemaVersion: 1, AuthorizationID: id.New().String(), TenantID: intent.TenantID, MerchantID: intent.MerchantID,
		Purpose: crypto.SigningPurposeWithdrawal, BasisType: "CryptoAMLWithdrawalLine", BasisID: line.LineID, Attempt: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Hour),
		NetworkID: f.input.Request.NetworkID, NetworkCode: networkCode, ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: networkIdentifier, SourceAddress: line.SourceAddress,
		FeePayerAddress: line.SourceAddress, VaultKeyID: line.SourceVaultKeyID, DestinationAddress: intent.DestinationAddress, AssetStandard: "trc20", ContractAddress: "test-contract",
		AmountMinorUnits: line.AmountMinorUnits, MaxNetworkFeeMinor: types.MustCryptoAmount("1"), UnsignedFingerprint: "publication-" + line.LineID.String(), ApprovalBundleDigest: digest, PolicyVersion: "tron-v1"}
	unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: auth.UnsignedFingerprint, Payload: []byte(`{"synthetic":true}`)}
	return OutboundPublicationFixture{f.ctx, f.pool, intent.BasisID, line.WalletID, auth, unsigned, bundle}
}

func (f OutboundPublicationFixture) AssertLockOrder(t *testing.T, first, next, writer func(context.Context) error) {
	t.Helper()
	assertAccountingLockOrder(t, f.Ctx, f.Pool, first, next, writer)
}
