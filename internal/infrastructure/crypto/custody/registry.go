package custody

import (
	"context"
	"fmt"

	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/crypto/evm_vault_signer"
	"metapus/internal/infrastructure/crypto/vault_signer"
	"metapus/internal/infrastructure/vaultclient"
)

type Config struct {
	Client   *vaultclient.Client
	TenantID string
}

func Build(cfg Config) (*crypto.FamilySignerRegistry, *crypto.FamilyWalletDeriverRegistry, error) {
	signers := crypto.NewFamilySignerRegistry()
	derivers := crypto.NewFamilyWalletDeriverRegistry()
	if cfg.Client == nil {
		unavailable := unavailableDeriver{}
		_ = derivers.Register(crypto.ChainFamilyTRON, unavailable)
		_ = derivers.Register(crypto.ChainFamilyEVM, unavailable)
		return signers, derivers, nil
	}
	tronSigner, err := vault_signer.New(vault_signer.Config{Client: cfg.Client, TenantID: cfg.TenantID})
	if err != nil {
		return nil, nil, fmt.Errorf("build TRON custody: %w", err)
	}
	evmSigner, err := evm_vault_signer.New(evm_vault_signer.Config{Client: cfg.Client, TenantID: cfg.TenantID})
	if err != nil {
		return nil, nil, fmt.Errorf("build EVM custody: %w", err)
	}
	for _, entry := range []struct {
		family  crypto.ChainFamily
		signer  crypto.PolicyAwareTransactionSigner
		deriver crypto.WalletDeriver
	}{
		{crypto.ChainFamilyTRON, tronSigner, tronSigner}, {crypto.ChainFamilyEVM, evmSigner, evmSigner},
	} {
		if err := signers.Register(entry.signer); err != nil {
			return nil, nil, err
		}
		if err := derivers.Register(entry.family, entry.deriver); err != nil {
			return nil, nil, err
		}
	}
	return signers, derivers, nil
}

type unavailableDeriver struct{}

func (unavailableDeriver) DeriveAddress(context.Context, crypto.NetworkRef, string) (crypto.WalletDerivation, error) {
	return crypto.WalletDerivation{}, fmt.Errorf("vault wallet derivation is not configured")
}
