package crypto

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
)

type contractDriver struct{ network NetworkRef }

func (d contractDriver) Network() NetworkRef { return d.network }
func (d contractDriver) Capabilities() ChainFamilyCapabilities {
	return contractCapabilities(d.network.Family)
}
func (d contractDriver) Watchers() WatcherFactory             { return nil }
func (d contractDriver) Transactions() TransactionBuilder     { return nil }
func (d contractDriver) Broadcaster() Broadcaster             { return nil }
func (d contractDriver) Finality() FinalityReader             { return nil }
func (d contractDriver) Balances() BalanceReader              { return nil }
func (d contractDriver) Wallets() WalletDeriver               { return nil }
func (d contractDriver) Accounts() AccountLifecycle           { return nil }
func (d contractDriver) TokenAccounts() TokenAccountLifecycle { return nil }
func (d contractDriver) Addresses() AddressCodec              { return nil }

type contractFactory struct{ family ChainFamily }

func (f contractFactory) Family() ChainFamily { return f.family }
func (f contractFactory) Capabilities() ChainFamilyCapabilities {
	return contractCapabilities(f.family)
}
func (f contractFactory) Create(_ context.Context, network NetworkRef) (NetworkDriver, error) {
	return contractDriver{network: network}, nil
}

func contractCapabilities(family ChainFamily) ChainFamilyCapabilities {
	return ChainFamilyCapabilities{
		Family:               family,
		WalletProvisioning:   WalletProvisioningUnsupported,
		RequiredRPCRoles:     []string{"rpc"},
		ExternalResourceMode: ExternalResourceNone,
		SequenceModel:        SequenceNone,
		CheckpointTimeUnit:   CheckpointUnixSeconds,
		TokenAccountModel:    TokenAccountSameAddress,
	}
}

type capabilityContractDriver struct {
	contractDriver
	capabilities ChainFamilyCapabilities
	deriver      WalletDeriver
}

func (d capabilityContractDriver) Capabilities() ChainFamilyCapabilities { return d.capabilities }
func (d capabilityContractDriver) Wallets() WalletDeriver                { return d.deriver }

type contractWalletDeriver struct{}

func (contractWalletDeriver) DeriveAddress(context.Context, NetworkRef, string) (WalletDerivation, error) {
	return WalletDerivation{}, nil
}

func TestBuildChainDriverRegistryRoutesOnlyByNetworkID(t *testing.T) {
	tronNetwork := NetworkRef{ID: id.New(), Code: "TRON-NILE", Family: ChainFamilyTRON, NetworkIdentifier: "nile", NativeTokenID: id.New(), NativeDecimals: 6, RequiredConfirmations: 20, FinalityMode: FinalityByConfirmations}
	testSecondFamily := NetworkRef{ID: id.New(), Code: "EVM-TEST", Family: ChainFamilyEVM, NetworkIdentifier: "31337", NativeTokenID: id.New(), NativeDecimals: 18, RequiredConfirmations: 12, FinalityMode: FinalityByConfirmations}

	registry, err := BuildChainDriverRegistry(context.Background(), []NetworkRef{tronNetwork, testSecondFamily}, []DriverFactory{
		contractFactory{family: ChainFamilyTRON},
		contractFactory{family: ChainFamilyEVM},
	})
	require.NoError(t, err)

	driver, err := registry.Resolve(testSecondFamily.ID)
	require.NoError(t, err)
	require.Equal(t, ChainFamilyEVM, driver.Network().Family)
	_, err = registry.Resolve(id.New())
	require.ErrorIs(t, err, ErrNetworkDriverNotFound)
}

func TestBuildChainDriverRegistryKeepsHealthyNetworksWhenOneFamilyIsUnavailable(t *testing.T) {
	tronNetwork := NetworkRef{ID: id.New(), Code: "TRON-NILE", Family: ChainFamilyTRON, NetworkIdentifier: "nile", NativeTokenID: id.New(), NativeDecimals: 6, RequiredConfirmations: 20, FinalityMode: FinalityByConfirmations}
	evmNetwork := NetworkRef{ID: id.New(), Code: "ETHEREUM-MAINNET", Family: ChainFamilyEVM, NetworkIdentifier: "1", NativeTokenID: id.New(), NativeDecimals: 18, RequiredConfirmations: 12, FinalityMode: FinalityByConfirmations}

	registry, err := BuildChainDriverRegistry(context.Background(), []NetworkRef{tronNetwork, evmNetwork}, []DriverFactory{
		contractFactory{family: ChainFamilyTRON},
	})
	require.ErrorIs(t, err, ErrNetworkDriverNotFound)
	_, resolveErr := registry.Resolve(tronNetwork.ID)
	require.NoError(t, resolveErr)
	_, resolveErr = registry.Resolve(evmNetwork.ID)
	require.ErrorIs(t, resolveErr, ErrNetworkDriverNotFound)
}

func TestChainDriverRegistryEnforcesProvisioningCapabilityDependencies(t *testing.T) {
	network := NetworkRef{ID: id.New(), Code: "CUSTOM-EVM", Family: ChainFamilyEVM, NetworkIdentifier: "31337", NativeTokenID: id.New(), NativeDecimals: 18, RequiredConfirmations: 1, FinalityMode: FinalityByConfirmations}
	derived := ChainFamilyCapabilities{Family: ChainFamilyEVM, WalletProvisioning: WalletProvisioningDerived, RequiredRPCRoles: []string{"rpc"}, ExternalResourceMode: ExternalResourceNone, SequenceModel: SequenceNone, CheckpointTimeUnit: CheckpointUnixSeconds, TokenAccountModel: TokenAccountSameAddress}
	registry := NewChainDriverRegistry()
	if err := registry.Register(capabilityContractDriver{
		network:      network,
		capabilities: derived,
	}); err == nil {
		t.Fatal("derived provisioning was registered without a wallet deriver")
	}
	if err := registry.Register(capabilityContractDriver{
		network:      network,
		capabilities: derived,
		deriver:      contractWalletDeriver{},
	}); err != nil {
		t.Fatalf("derived provisioning with a wallet deriver was rejected: %v", err)
	}

	activationNetwork := network
	activationNetwork.ID = id.New()
	activation := ChainFamilyCapabilities{Family: ChainFamilyEVM, WalletProvisioning: WalletProvisioningDerivedWithActivation, RequiredRPCRoles: []string{"rpc"}, ExternalResourceMode: ExternalResourceNone, SequenceModel: SequenceNone, CheckpointTimeUnit: CheckpointUnixSeconds, TokenAccountModel: TokenAccountSameAddress}
	if err := registry.Register(capabilityContractDriver{
		network:      activationNetwork,
		capabilities: activation,
		deriver:      contractWalletDeriver{},
	}); err == nil {
		t.Fatal("activation provisioning was registered without an account lifecycle")
	}
}

func TestChainDriverRegistryEnforcesReplacementCapability(t *testing.T) {
	network := NetworkRef{ID: id.New(), Code: "CUSTOM-EVM", Family: ChainFamilyEVM, NetworkIdentifier: "31337", NativeTokenID: id.New(), NativeDecimals: 18, RequiredConfirmations: 1, FinalityMode: FinalityByConfirmations}
	caps := evmWalletPoolCapabilities()
	registry := NewChainDriverRegistry()
	if err := registry.Register(capabilityContractDriver{
		network:      network,
		capabilities: caps,
		deriver:      contractWalletDeriver{},
	}); err == nil {
		t.Fatal("replacement capability was registered without ReplacementTransactionBuilder")
	}
	if err := registry.Register(replacementContractDriver{
		network:      network,
		capabilities: caps,
		deriver:      contractWalletDeriver{}}); err != nil {
		t.Fatalf("replacement-capable driver was rejected: %v", err)
	}
}

type replacementContractDriver struct {
	capabilityContractDriver
}

func (replacementContractDriver) BuildReplacement(context.Context, TransferIntent, UnsignedTxEnvelope, types.RawAmount) (UnsignedTxEnvelope, FeeQuote, error) {
	return UnsignedTxEnvelope{}, FeeQuote{}, nil
}

func TestChainDriverRegistryEnforcesTokenAccountCapability(t *testing.T) {
	network := NetworkRef{ID: id.New(), Code: "CUSTOM-EVM", Family: ChainFamilyEVM, NetworkIdentifier: "31337", NativeTokenID: id.New(), NativeDecimals: 18, RequiredConfirmations: 1, FinalityMode: FinalityByConfirmations}
	caps := evmWalletPoolCapabilities()
	caps.SupportsReplacement = false
	caps.TokenAccountModel = TokenAccountDerivedPerAsset
	registry := NewChainDriverRegistry()
	if err := registry.Register(capabilityContractDriver{
		network:      network,
		capabilities: caps,
		deriver:      contractWalletDeriver{},
	}); err == nil {
		t.Fatal("derived-per-asset token model was registered without TokenAccountLifecycle")
	}

	sameAddress := evmWalletPoolCapabilities()
	sameAddress.SupportsReplacement = false
	if err := registry.Register(tokenAccountContractDriver{
		network:      network,
		capabilities: sameAddress,
		deriver:      contractWalletDeriver{},
	}); err == nil {
		t.Fatal("same-address token model was registered with TokenAccountLifecycle")
	}

	derivedNetwork := network
	derivedNetwork.ID = id.New()
	if err := registry.Register(tokenAccountContractDriver{
		network:      derivedNetwork,
		capabilities: caps,
		deriver:      contractWalletDeriver{},
	}); err != nil {
		t.Fatalf("derived-per-asset driver was rejected: %v", err)
	}
}

type stubTokenAccounts struct{}

func (stubTokenAccounts) IsReady(context.Context, AssetRef, string) (bool, error) {
	return true, nil
}
func (stubTokenAccounts) BuildMaterialization(context.Context, TransferIntent, FeeQuote) (UnsignedTxEnvelope, error) {
	return UnsignedTxEnvelope{}, nil
}

type tokenAccountContractDriver struct {
	capabilityContractDriver
}

func (tokenAccountContractDriver) TokenAccounts() TokenAccountLifecycle { return stubTokenAccounts{} }

func TestCanonicalUnsignedFingerprintIgnoresJSONFormattingAndKeyOrder(t *testing.T) {
	a, err := CanonicalUnsignedFingerprint(ChainFamilyTRON, 1, json.RawMessage(`{"amount":1,"to":"T"}`))
	require.NoError(t, err)
	b, err := CanonicalUnsignedFingerprint(ChainFamilyTRON, 1, json.RawMessage("{\n  \"to\": \"T\", \"amount\": 1\n}"))
	require.NoError(t, err)
	require.Equal(t, a, b)

	c, err := CanonicalUnsignedFingerprint(ChainFamilyEVM, 1, json.RawMessage(`{"amount":1,"to":"T"}`))
	require.NoError(t, err)
	require.NotEqual(t, a, c, "family must bind the fingerprint and prevent cross-family replay")
}

func TestCanonicalUnsignedFingerprintKeepsSchemaV1BytesFrozen(t *testing.T) {
	t.Parallel()

	fingerprint, err := CanonicalUnsignedFingerprint(
		ChainFamilyTRON,
		1,
		json.RawMessage(`{"text":"<>&\u2028\u2029","n":9007199254740993}`),
	)
	require.NoError(t, err)
	require.Equal(t, "8ce61cebcca58a4d62169ad0ddb311760beb83d63e53b78e5e42bf4ee7173c28", fingerprint)

	_, err = CanonicalUnsignedFingerprint(ChainFamilyTRON, 2, json.RawMessage(`{}`))
	require.ErrorContains(t, err, "unsupported unsigned fingerprint schema version")
}

func FuzzCanonicalUnsignedFingerprint(f *testing.F) {
	f.Add([]byte(`{"a":1}`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, payload []byte) {
		fingerprint, err := CanonicalUnsignedFingerprint(ChainFamilyTRON, 1, payload)
		if err != nil {
			return
		}
		require.Len(t, fingerprint, 64)
		repeat, err := CanonicalUnsignedFingerprint(ChainFamilyTRON, 1, payload)
		require.NoError(t, err)
		require.Equal(t, fingerprint, repeat)
	})
}
