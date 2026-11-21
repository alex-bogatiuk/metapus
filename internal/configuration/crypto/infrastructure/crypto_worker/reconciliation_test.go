package crypto_worker

import (
	"testing"

	"metapus/internal/configuration/crypto/domain/catalogs/token"
)

func TestReconciliationKind(t *testing.T) {
	if got := reconciliationKind(token.TokenStandardNative); got != "native_fee_balance_mismatch" {
		t.Fatalf("native kind = %q", got)
	}
	if got := reconciliationKind(token.TokenStandardTRC20); got != "token_balance_mismatch" {
		t.Fatalf("TRC20 kind = %q", got)
	}
}

func TestRunNetworkBalanceReconciliationRejectsEmptyNetwork(t *testing.T) {
	t.Parallel()

	processor := &CryptoProcessor{}
	_, err := processor.RunNetworkBalanceReconciliation(t.Context(), "  ")
	if err == nil || err.Error() != "reconciliation network code is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}
