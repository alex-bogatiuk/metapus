// Package merchant_provisioning runs through a dedicated control-plane Vault
// proxy. Never install its enrollment capability in the ordinary worker ACL.
package merchant_provisioning

import (
	"context"
	"fmt"
	"net/http"

	"metapus/internal/configuration/crypto/domain/crypto"
	evm "metapus/internal/configuration/crypto/infrastructure/crypto/evm_vault_signer"
	tron "metapus/internal/configuration/crypto/infrastructure/crypto/vault_signer"
	"metapus/internal/configuration/crypto/infrastructure/vaultclient"
	"metapus/internal/core/id"
)

type Vault struct {
	client   *vaultclient.Client
	tenantID string
}

func New(client *vaultclient.Client, tenantID id.ID) (*Vault, error) {
	if client == nil || id.IsNil(tenantID) {
		return nil, fmt.Errorf("control-plane Vault client and canonical tenant required")
	}
	return &Vault{client: client, tenantID: tenantID.String()}, nil
}

func (v *Vault) DeriveAndEnroll(ctx context.Context, namespace id.ID, network crypto.NetworkRef, merchantID id.ID, role, path string) (crypto.WalletDerivation, crypto.MerchantWalletEnrollment, error) {
	var derived crypto.WalletDerivation
	var binding crypto.MerchantWalletEnrollment
	if namespace.String() != v.tenantID {
		return derived, binding, fmt.Errorf("provisioning database and Vault namespace mismatch")
	}
	pluginNetwork, err := crypto.MerchantCustodyNetwork(network)
	if err != nil {
		return derived, binding, err
	}
	var deriver crypto.WalletDeriver
	var readiness crypto.MerchantCustodySigner
	switch network.Family {
	case crypto.ChainFamilyTRON:
		signer, e := tron.New(tron.Config{TenantID: v.tenantID, Client: v.client})
		if e != nil {
			return derived, binding, e
		}
		deriver, readiness = signer, signer
	case crypto.ChainFamilyEVM:
		signer, e := evm.New(evm.Config{TenantID: v.tenantID, Client: v.client})
		if e != nil {
			return derived, binding, e
		}
		deriver, readiness = signer, signer
	default:
		return derived, binding, fmt.Errorf("unsupported merchant provisioning family")
	}
	if err = readiness.CheckMerchantCustodyReady(ctx, network); err != nil {
		return derived, binding, err
	}
	derived, err = deriver.DeriveAddress(ctx, network, path)
	if err != nil {
		return derived, binding, err
	}
	if derived.DerivationPath != path || derived.Address == "" || derived.VaultKeyID == "" {
		return derived, binding, fmt.Errorf("Vault merchant derivation identity mismatch")
	}
	request := map[string]string{"network": pluginNetwork, "address": derived.Address, "merchant_id": merchantID.String(), "role": role}
	var response struct {
		Data crypto.MerchantWalletEnrollment `json:"data"`
	}
	err = v.client.DoJSON(ctx, http.MethodPost, "/v1/"+string(network.Family)+"/tenants/"+v.tenantID+"/wallets/custody", request, &response)
	return derived, response.Data, err
}
