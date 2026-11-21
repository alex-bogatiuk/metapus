package cryptoapp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/internal/core/id"
)

type amlBalanceEvidenceFixture struct {
	now      time.Time
	evidence []aml.MerchantBalanceEvidence
}

func (f amlBalanceEvidenceFixture) ReadMerchantRiskBalance(context.Context, id.ID) (time.Time, []aml.MerchantBalanceEvidence, error) {
	return f.now, f.evidence, nil
}

func TestAMLBalanceSnapshotExpiresWithEvidenceAndRejectsForeignScope(t *testing.T) {
	now := time.Now()
	expiry := now.Add(time.Second)
	merchantID := id.New()
	f := amlBalanceEvidenceFixture{now: now, evidence: []aml.MerchantBalanceEvidence{{Scope: aml.BalanceScope{MerchantID: merchantID, NetworkID: id.New(), TokenID: id.New()},
		Sources: []aml.PrincipalPosition{{WalletID: id.New(), Class: aml.Low, ExpiresAt: &expiry}}}}}
	service := NewAMLBalanceService(f)
	snapshot, err := service.Get(t.Context(), merchantID)
	require.NoError(t, err)
	require.Equal(t, expiry, snapshot.FreshUntil)
	_, err = service.Get(t.Context(), id.New())
	require.ErrorContains(t, err, "scope mismatch")
}
