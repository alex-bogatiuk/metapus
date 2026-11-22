package crypto

import (
	"context"
	"strings"
	"time"

	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
)

type MerchantWalletRequest struct {
	ID, MerchantID, NetworkID id.ID
	Role, Actor               string
}

func (r MerchantWalletRequest) Validate() error {
	if id.IsNil(r.ID) || id.IsNil(r.MerchantID) || id.IsNil(r.NetworkID) || strings.TrimSpace(r.Actor) == "" || (r.Role != "merchant_pool" && r.Role != "liquidity_hot" && r.Role != "network_fee") {
		return apperror.NewValidation("merchant wallet provisioning requires immutable owner, role, request ID and actor")
	}
	return nil
}

type MerchantWalletProvision struct {
	VaultNamespace id.ID
	Request        MerchantWalletRequest
	Job            WalletProvisioningJob
	Network        NetworkRef
}

type MerchantWalletEnrollment struct {
	SchemaVersion string `json:"schema_version"`
	Network       string `json:"network"`
	Address       string `json:"address"`
	MerchantID    string `json:"merchant_id"`
	Role          string `json:"role"`
}

func MerchantCustodyNetwork(network NetworkRef) (string, error) {
	if network.Family == ChainFamilyEVM && network.NetworkIdentifier != "" {
		return network.NetworkIdentifier, nil
	}
	if network.Family == ChainFamilyTRON {
		switch strings.ToLower(strings.TrimSpace(network.NetworkIdentifier)) {
		case "mainnet":
			return "tron_mainnet", nil
		case "nile":
			return "tron_nile", nil
		}
	}
	return "", apperror.NewValidation("unsupported merchant custody network")
}

// MerchantWalletProvisioner is deliberately control-plane only. The ordinary
// signer/worker ACL never gets permission to assign wallet owners.
type MerchantWalletProvisioner interface {
	DeriveAndEnroll(context.Context, id.ID, NetworkRef, id.ID, string, string) (WalletDerivation, MerchantWalletEnrollment, error)
}

type MerchantWalletProvisionRepository interface {
	ClaimMerchantWalletProvision(context.Context, MerchantWalletRequest, string, time.Duration) (MerchantWalletProvision, error)
	CompleteMerchantWalletProvision(context.Context, MerchantWalletProvision, WalletDerivation, MerchantWalletEnrollment) (id.ID, error)
	RetryMerchantWalletProvision(context.Context, MerchantWalletProvision) error
}
