package aml

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestMerchantRiskBalanceConservesNetClaimsAndDebitFirstCommitments(t *testing.T) {
	now := time.Now()
	expires := now.Add(time.Minute)
	e := MerchantBalanceEvidence{Scope: BalanceScope{id.New(), id.New(), id.New()}, Ledger: types.MustCryptoAmount("900000000000000000000000"),
		RoutingEnabled: true, ScopeVersion: 1, LedgerMatches: true, PendingChain: types.MustCryptoAmount("100"),
		Sources: []PrincipalPosition{{WalletID: id.New(), Amount: types.MustCryptoAmount("900000000000000000000000"), Class: Low, Revision: 1, ScopeVersion: 1, Covered: true, ExpiresAt: &expires}}}
	r, err := BuildMerchantRiskBalance(e, now)
	require.NoError(t, err)
	require.True(t, r.AllocationComplete)
	require.Equal(t, e.Ledger.String(), r.Free.String())
	require.Equal(t, e.Ledger.String(), r.Total.String(), "pending chain gross is not merchant principal")
	assertRiskBalanceConservation(t, r)
	e.Ledger = e.Ledger.Sub(types.MustCryptoAmount("20"))
	e.Commitments = []DebitCommitment{{RequestID: id.New(), Amount: types.MustCryptoAmount("20"), Executable: true}}
	r, err = BuildMerchantRiskBalance(e, now)
	require.NoError(t, err)
	require.False(t, r.AllocationComplete)
	require.Equal(t, e.Sources[0].Amount.String(), r.Total.String())
	require.Equal(t, "20", r.Reserved.String())
	require.True(t, r.Free.IsZero(), "unallocated request must not leave an invented free low source")
	require.Equal(t, e.Ledger.String(), r.TechnicalHold.String())
	assertRiskBalanceConservation(t, r)
	e.Commitments[0].Executable = false
	r, err = BuildMerchantRiskBalance(e, now)
	require.NoError(t, err)
	require.Equal(t, "20", r.PendingRestoration.String())
	require.True(t, r.Reserved.IsZero())
	require.Equal(t, r.Total.String(), r.TechnicalHold.String())
	assertRiskBalanceConservation(t, r)
	e.Commitments[0].Amount = types.ZeroCryptoAmount()
	e.Commitments[0].Executable = true
	r, err = BuildMerchantRiskBalance(e, now)
	require.NoError(t, err)
	require.Equal(t, "withdrawal_reconciliation_required", r.Reason)
	require.True(t, r.Free.IsZero(), "potential execution without a matching debit requires reconciliation")
	assertRiskBalanceConservation(t, r)
}

func TestMerchantRiskBalanceExpiryHoldsAndUnallocatedClaims(t *testing.T) {
	now := time.Now()
	expires := now.Add(-time.Second)
	e := MerchantBalanceEvidence{Scope: BalanceScope{id.New(), id.New(), id.New()}, Ledger: types.MustCryptoAmount("100"),
		RoutingEnabled: true, ScopeVersion: 2, LedgerMatches: true,
		Sources: []PrincipalPosition{{WalletID: id.New(), Amount: types.MustCryptoAmount("90"), Class: Low, Revision: 1, ScopeVersion: 2, Covered: true, ExpiresAt: &expires}}}
	r, err := BuildMerchantRiskBalance(e, now)
	require.NoError(t, err)
	require.False(t, r.AllocationComplete)
	require.Equal(t, "90", r.Buckets[3].Free.String(), "expired evidence is unknown, never stale low")
	require.Equal(t, "10", r.Buckets[3].TechnicalHold.String())
	assertRiskBalanceConservation(t, r)
	e.Sources[0].Held = true
	r, err = BuildMerchantRiskBalance(e, now)
	require.NoError(t, err)
	require.True(t, r.Free.IsZero())
	require.Equal(t, "100", r.TechnicalHold.String())
	e.Sources[0].Class = High
	e.LedgerMatches = false
	r, err = BuildMerchantRiskBalance(e, now)
	require.NoError(t, err)
	require.True(t, r.HasHighExposure, "an attribution hold cannot hide known high exposure")
	require.Equal(t, "ledger_reconciliation_required", r.Reason)
	require.Equal(t, "100", r.TechnicalHold.String())
	assertRiskBalanceConservation(t, r)
}

func TestMerchantRiskBalanceRejectsDuplicatesAndOverAttribution(t *testing.T) {
	e := MerchantBalanceEvidence{Scope: BalanceScope{id.New(), id.New(), id.New()}, Ledger: types.MustCryptoAmount("10"), RoutingEnabled: true, ScopeVersion: 1, LedgerMatches: true,
		Sources: []PrincipalPosition{{WalletID: id.New(), Amount: types.MustCryptoAmount("11"), Class: High, Revision: 1, ScopeVersion: 1, Covered: true}}}
	r, err := BuildMerchantRiskBalance(e, time.Now())
	require.NoError(t, err)
	require.Equal(t, "source_reconciliation_required", r.Reason)
	require.Equal(t, "10", r.Total.String())
	require.True(t, r.Free.IsZero())
	e.Sources = append(e.Sources, e.Sources[0])
	_, err = BuildMerchantRiskBalance(e, time.Now())
	require.ErrorContains(t, err, "duplicate principal")
}

func assertRiskBalanceConservation(t *testing.T, r MerchantRiskBalance) {
	t.Helper()
	require.Equal(t, r.Total.String(), r.Free.Add(r.Reserved).Add(r.TechnicalHold).String())
	total := types.ZeroCryptoAmount()
	for _, b := range r.Buckets {
		require.Equal(t, b.Total.String(), b.Free.Add(b.Reserved).Add(b.TechnicalHold).String())
		total = total.Add(b.Total)
	}
	require.Equal(t, r.Total.String(), total.String())
}
