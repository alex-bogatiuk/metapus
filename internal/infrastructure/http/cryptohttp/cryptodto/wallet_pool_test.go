package cryptodto

import (
	"strings"
	"testing"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
)

func TestUpdateWalletPoolPolicyRequestMapsNestedActivation(t *testing.T) {
	maxConcurrent := 2
	reserve := types.NewCryptoAmountFromInt64(3)
	request := UpdateWalletPoolPolicyRequest{
		Activation: &UpdateWalletPoolActivationPolicyRequest{
			MaxConcurrent: &maxConcurrent,
			ReserveMinor:  &reserve,
		},
		Version: 7,
	}
	patch := request.ToDomain()
	if patch.Activation == nil || patch.Activation.MaxConcurrent != &maxConcurrent || patch.Activation.ReserveMinor != &reserve {
		t.Fatalf("activation patch = %#v", patch.Activation)
	}
}

func TestWalletPoolStateResponsePublishesCapabilitiesAndNullableActivation(t *testing.T) {
	response := NewWalletPoolStateResponse(crypto.WalletPoolState{
		Policy: crypto.WalletPoolPolicy{
			NetworkID:   id.New(),
			ChainFamily: crypto.ChainFamilyEVM,
		},
		Capabilities: crypto.ChainFamilyCapabilities{
			Family:               crypto.ChainFamilyEVM,
			WalletProvisioning:   crypto.WalletProvisioningDerived,
			RequiredRPCRoles:     []string{"rpc"},
			ExternalResourceMode: crypto.ExternalResourceNone,
			SequenceModel:        crypto.SequenceAccount,
			CheckpointTimeUnit:   crypto.CheckpointUnixSeconds,
			TokenAccountModel:    crypto.TokenAccountSameAddress,
			SupportsReplacement:  true,
		},
	})
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	encoded := string(payload)
	for _, expected := range []string{`"activation":null`, `"family":"evm"`, `"walletProvisioning":"derived"`} {
		if !strings.Contains(encoded, expected) {
			t.Fatalf("response JSON %s does not contain %s", encoded, expected)
		}
	}
}
