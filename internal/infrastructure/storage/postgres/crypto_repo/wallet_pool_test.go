package crypto_repo

import (
	"strings"
	"testing"

	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto"
)

func TestWalletPoolRefillCandidatesAreSelectedByPolicyStateOnly(t *testing.T) {
	query := strings.ToUpper(walletPoolRefillCandidatesQuery)
	for _, forbidden := range []string{"TRON-NILE", "TRON-MAINNET", "N.CODE LIKE", "CHAIN_FAMILY"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("refill candidate query contains legacy network gate %q", forbidden)
		}
	}
	for _, required := range []string{"P.ENABLED = TRUE", "P.NEEDS_REVIEW = FALSE", "N.IS_ACTIVE = TRUE", "N.DELETION_MARK = FALSE"} {
		if !strings.Contains(query, required) {
			t.Fatalf("refill candidate query is missing lifecycle gate %q", required)
		}
	}
}

func TestInitialChainActivationStatusFollowsProvisioningMode(t *testing.T) {
	tests := []struct {
		mode crypto.WalletProvisioningMode
		want wallet.ChainActivationStatus
	}{
		{mode: crypto.WalletProvisioningDerived, want: wallet.ChainActivationActive},
		{mode: crypto.WalletProvisioningDerivedWithActivation, want: wallet.ChainActivationUnknown},
	}
	for _, test := range tests {
		got, err := initialChainActivationStatus(test.mode)
		if err != nil {
			t.Fatalf("initialChainActivationStatus(%q) error = %v", test.mode, err)
		}
		if got != test.want {
			t.Fatalf("initialChainActivationStatus(%q) = %q, want %q", test.mode, got, test.want)
		}
	}
	if _, err := initialChainActivationStatus(crypto.WalletProvisioningUnsupported); err == nil {
		t.Fatal("unsupported provisioning mode was accepted")
	}
}
