//go:build integration

package crypto_worker

import "github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"

type fakeSweepNetworkDriver struct{ accounts crypto.AccountLifecycle }

func (d fakeSweepNetworkDriver) Network() crypto.NetworkRef {
	return crypto.NetworkRef{Code: "TRON-NILE", Family: crypto.ChainFamilyTRON}
}
func (d fakeSweepNetworkDriver) Capabilities() crypto.ChainFamilyCapabilities {
	return crypto.ChainFamilyCapabilities{
		Family:               crypto.ChainFamilyTRON,
		WalletProvisioning:   crypto.WalletProvisioningDerivedWithActivation,
		RequiredRPCRoles:     []string{"indexer", "fullnode", "solidity"},
		ExternalResourceMode: crypto.ExternalResourceOptional,
		SequenceModel:        crypto.SequenceNone,
		CheckpointTimeUnit:   crypto.CheckpointUnixMillis,
		TokenAccountModel:    crypto.TokenAccountSameAddress,
		MinWatcherLagSeconds: 180,
	}
}
func (d fakeSweepNetworkDriver) Watchers() crypto.WatcherFactory             { return nil }
func (d fakeSweepNetworkDriver) Transactions() crypto.TransactionBuilder     { return nil }
func (d fakeSweepNetworkDriver) Broadcaster() crypto.Broadcaster             { return nil }
func (d fakeSweepNetworkDriver) Finality() crypto.FinalityReader             { return nil }
func (d fakeSweepNetworkDriver) Balances() crypto.BalanceReader              { return nil }
func (d fakeSweepNetworkDriver) Wallets() crypto.WalletDeriver               { return nil }
func (d fakeSweepNetworkDriver) Accounts() crypto.AccountLifecycle           { return d.accounts }
func (d fakeSweepNetworkDriver) TokenAccounts() crypto.TokenAccountLifecycle { return nil }
func (d fakeSweepNetworkDriver) Addresses() crypto.AddressCodec              { return nil }
