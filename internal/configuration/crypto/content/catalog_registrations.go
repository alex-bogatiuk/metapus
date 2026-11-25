// Package content is the crypto-processing configuration (Crypto): the
// Registration implementations of its catalogs and documents, its reports
// and the checks of the application assembled with it. The platform's shared
// entities live in internal/platformcontent.
//
// Platform Core interfaces live in internal/platform/ and v1/.
// Client extensions DO NOT modify this package — they register their own
// entities via FactoryRegistry from their own Go module.
package content

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/blockchain_network"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/crypto_provider_account"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/token"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_catalog_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/handlers"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/v1"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/metadata"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

func init() {
	// Register Enum metadata globally for automatic filter UI dropdown resolution.

	// Merchant KYB Status
	metadata.RegisterEnum[merchant.KYBStatus]([]metadata.EnumValue{
		{Value: "pending", Label: "Ожидает верификации"},
		{Value: "approved", Label: "Верифицирован"},
		{Value: "rejected", Label: "Отклонён"},
	})

	// Wallet Tier
	metadata.RegisterEnum[wallet.WalletTier]([]metadata.EnumValue{
		{Value: "pool", Label: "Pool"},
		{Value: "hot", Label: "Hot"},
		{Value: "warm", Label: "Warm"},
		{Value: "cold", Label: "Cold"},
	})

	// Wallet Role
	metadata.RegisterEnum[wallet.WalletRole]([]metadata.EnumValue{
		{Value: "merchant_pool", Label: "Пул мерчанта"},
		{Value: "liquidity_hot", Label: "Ликвидность"},
		{Value: "network_fee", Label: "Комиссия сети"},
		{Value: "treasury", Label: "Казначейство"},
	})

	// Wallet Status
	metadata.RegisterEnum[wallet.WalletStatus]([]metadata.EnumValue{
		{Value: "free", Label: "Свободен"},
		{Value: "leased", Label: "Арендован"},
		{Value: "assigned", Label: "Назначен"},
		{Value: "sweep_pending", Label: "Ожидает sweep"},
		{Value: "sweeping", Label: "В процессе sweep"},
		{Value: "frozen", Label: "Заморожен"},
	})

	// Wallet chain activation status
	metadata.RegisterEnum[wallet.ChainActivationStatus]([]metadata.EnumValue{
		{Value: "unknown", Label: "Не проверен"},
		{Value: "activating", Label: "Активируется"},
		{Value: "active", Label: "Активирован"},
		{Value: "held", Label: "Ожидает повтора"},
		{Value: "failed", Label: "Ошибка"},
	})

	// Wallet Allocation Mode
	metadata.RegisterEnum[wallet.AllocationMode]([]metadata.EnumValue{
		{Value: "transient", Label: "Временный (per-invoice)"},
		{Value: "persistent", Label: "Постоянный (per-customer)"},
	})

	// Blockchain Network Chain Families
	metadata.RegisterEnum[crypto.ChainFamily]([]metadata.EnumValue{
		{Value: string(crypto.ChainFamilyTRON), Label: "TRON"},
		{Value: string(crypto.ChainFamilyEVM), Label: "EVM (Ethereum, Polygon, Arbitrum, BSC)"},
		{Value: string(crypto.ChainFamilyTON), Label: "TON"},
		{Value: string(crypto.ChainFamilySolana), Label: "Solana"},
	})

	// RPC endpoint purposes; the server rejects a purpose the network lacks.
	metadata.RegisterEnum[rpcconfig.Role]([]metadata.EnumValue{
		{Value: string(rpcconfig.RoleRPC), Label: "RPC — чтение и отправка операций (EVM)"},
		{Value: string(rpcconfig.RoleFullNode), Label: "FullNode — операции и свежие данные (TRON)"},
		{Value: string(rpcconfig.RoleSolidity), Label: "Solidity — подтверждённые данные (TRON)"},
		{Value: string(rpcconfig.RoleIndexer), Label: "Indexer — TronGrid API (обработкой не используется)"},
	})

	// Blockchain Network Finality Modes
	metadata.RegisterEnum[crypto.FinalityMode]([]metadata.EnumValue{
		{Value: string(crypto.FinalityByConfirmations), Label: "По подтверждениям (Confirmations)"},
		{Value: string(crypto.FinalityByProtocolFinalized), Label: "Протокольная (Protocol Finalized)"},
		{Value: string(crypto.FinalityByConfirmationsOrHead), Label: "Подтверждения или протокол (Confirmations or Finalized)"},
	})

	// Token Standards
	metadata.RegisterEnum[token.TokenStandard]([]metadata.EnumValue{
		{Value: string(token.TokenStandardNative), Label: "Native"},
		{Value: string(token.TokenStandardTRC20), Label: "TRC-20"},
		{Value: string(token.TokenStandardERC20), Label: "ERC-20"},
		{Value: string(token.TokenStandardBEP20), Label: "BEP-20"},
		{Value: string(token.TokenStandardSPL), Label: "SPL"},
		{Value: string(token.TokenStandardJetton), Label: "Jetton"},
	})
}

// ── ResolveRefs callbacks for catalogs with FK references ───────────────

func resolveCatalogRefs[T any](ctx context.Context, collect func(*postgres.ReferenceResolver, T), entities ...T) (any, error) {
	resolver := postgres.NewReferenceResolver()
	for _, e := range entities {
		collect(resolver, e)
	}
	return resolver.Resolve(ctx)
}

// ---------------------------------------------------------------------------
// BlockchainNetwork
// ---------------------------------------------------------------------------

type BlockchainNetworkRegistration struct {
	v1.CatalogRegistrationFor[blockchain_network.BlockchainNetwork, cryptodto.CreateBlockchainNetworkRequest, cryptodto.UpdateBlockchainNetworkRequest, cryptodto.BlockchainNetworkResponse]
}

func (r *BlockchainNetworkRegistration) ReferenceTypes() []string {
	return []string{"blockchain_network"}
}
func (r *BlockchainNetworkRegistration) EntityLabel() string { return "Блокчейн-сети" }
func (r *BlockchainNetworkRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Блокчейн-сеть",
		Plural:   "Блокчейн-сети",
		NewLabel: "Новая блокчейн-сеть",
		Genitive: "блокчейн-сети",
	}
}
func (r *BlockchainNetworkRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := crypto_catalog_repo.NewBlockchainNetworkRepo()
	service := blockchain_network.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, blockchain_network.Definition.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*blockchain_network.BlockchainNetwork,
		cryptodto.CreateBlockchainNetworkRequest,
		cryptodto.UpdateBlockchainNetworkRequest,
	]{
		Service:    service.CatalogService,
		EntityName: blockchain_network.Definition.Key,
		MapCreateDTO: func(req cryptodto.CreateBlockchainNetworkRequest) *blockchain_network.BlockchainNetwork {
			return req.ToEntity()
		},
		MapUpdateDTO: func(req cryptodto.UpdateBlockchainNetworkRequest, existing *blockchain_network.BlockchainNetwork) *blockchain_network.BlockchainNetwork {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *blockchain_network.BlockchainNetwork) any {
			return cryptodto.FromBlockchainNetwork(entity)
		},
	})
}

// ---------------------------------------------------------------------------
// Token
// ---------------------------------------------------------------------------

func tokenRegistration() v1.CatalogRegistration {
	return v1.Catalog[token.Token](metadata.Presentation{
		Singular: "Токен", Plural: "Токены", NewLabel: "Новый токен", Genitive: "токена",
	}).
		// TokenResponse is the platform's auth token contract.
		ResponseName("CryptoTokenResponse").
		Service(func(deps v1.CatalogDeps) *domain.CatalogService[*token.Token] {
			return token.NewService(crypto_catalog_repo.NewTokenRepo(), deps.Numerator).CatalogService
		})
}

// ---------------------------------------------------------------------------
// Merchant
// ---------------------------------------------------------------------------

type MerchantRegistration struct {
	v1.CatalogRegistrationFor[merchant.Merchant, cryptodto.CreateMerchantRequest, cryptodto.UpdateMerchantRequest, cryptodto.MerchantResponse]
}

func (r *MerchantRegistration) ReferenceTypes() []string { return []string{"merchant"} }
func (r *MerchantRegistration) EntityLabel() string      { return "Мерчанты" }
func (r *MerchantRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Мерчант",
		Plural:   "Мерчанты",
		NewLabel: "Новый мерчант",
		Genitive: "мерчанта",
	}
}
func (r *MerchantRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := crypto_catalog_repo.NewMerchantRepo()
	service := merchant.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, merchant.Definition.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*merchant.Merchant,
		cryptodto.CreateMerchantRequest,
		cryptodto.UpdateMerchantRequest,
	]{
		Service:      service.CatalogService,
		EntityName:   merchant.Definition.Key,
		MapCreateDTO: func(req cryptodto.CreateMerchantRequest) *merchant.Merchant { return req.ToEntity() },
		MapUpdateDTO: func(req cryptodto.UpdateMerchantRequest, existing *merchant.Merchant) *merchant.Merchant {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *merchant.Merchant) any { return cryptodto.FromMerchant(entity) },
	})
}

// ---------------------------------------------------------------------------
// Wallet
// ---------------------------------------------------------------------------

type WalletRegistration struct {
	v1.CatalogRegistrationFor[wallet.Wallet, cryptodto.CreateWalletRequest, cryptodto.UpdateWalletRequest, cryptodto.WalletResponse]
}

func (r *WalletRegistration) ReferenceTypes() []string { return []string{"wallet"} }
func (r *WalletRegistration) EntityLabel() string      { return "Кошельки" }
func (r *WalletRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Кошелёк",
		Plural:   "Кошельки",
		NewLabel: "Новый кошелёк",
		Genitive: "кошелька",
	}
}
func (r *WalletRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := crypto_catalog_repo.NewWalletRepo()
	service := wallet.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, wallet.Definition.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*wallet.Wallet,
		cryptodto.CreateWalletRequest,
		cryptodto.UpdateWalletRequest,
	]{
		Service:      service.CatalogService,
		EntityName:   wallet.Definition.Key,
		MapCreateDTO: func(req cryptodto.CreateWalletRequest) *wallet.Wallet { return req.ToEntity() },
		MapUpdateDTO: func(req cryptodto.UpdateWalletRequest, existing *wallet.Wallet) *wallet.Wallet {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO:    func(entity *wallet.Wallet) any { return cryptodto.FromWallet(entity) },
		ResolveRefs: resolveWalletRefs,
		MapToDTOWithRefs: func(entity *wallet.Wallet, refs any) any {
			return cryptodto.FromWallet(entity, refs.(postgres.ResolvedRefs))
		},
	})
}

func resolveWalletRefs(ctx context.Context, entities ...*wallet.Wallet) (any, error) {
	ids := make([]id.ID, 0, len(entities))
	for _, item := range entities {
		ids = append(ids, item.ID)
	}
	views, err := crypto_repo.NewAMLRepository().ReadWalletRiskDisplays(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, item := range entities {
		item.RiskDisplay = views[item.ID]
	}

	return resolveCatalogRefs(ctx, cryptodto.CollectWalletRefs, entities...)
}

// ---------------------------------------------------------------------------
// CryptoProviderAccount
// ---------------------------------------------------------------------------

type CryptoProviderAccountRegistration struct {
	v1.CatalogRegistrationFor[crypto_provider_account.CryptoProviderAccount, cryptodto.CreateCryptoProviderAccountRequest, cryptodto.UpdateCryptoProviderAccountRequest, cryptodto.CryptoProviderAccountResponse]
}

func (r *CryptoProviderAccountRegistration) ReferenceTypes() []string {
	return []string{"crypto_provider_account"}
}
func (r *CryptoProviderAccountRegistration) EntityLabel() string {
	return "Аккаунты крипто-провайдеров"
}
func (r *CryptoProviderAccountRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{Singular: "Аккаунт крипто-провайдера", Plural: "Аккаунты крипто-провайдеров", NewLabel: "Новый аккаунт", Genitive: "аккаунта крипто-провайдера"}
}
func (r *CryptoProviderAccountRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := crypto_catalog_repo.NewCryptoProviderAccountRepo()
	service := crypto_provider_account.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, crypto_provider_account.Definition.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*crypto_provider_account.CryptoProviderAccount,
		cryptodto.CreateCryptoProviderAccountRequest,
		cryptodto.UpdateCryptoProviderAccountRequest,
	]{
		Service: service.CatalogService, EntityName: crypto_provider_account.Definition.Key,
		MapCreateDTO: func(req cryptodto.CreateCryptoProviderAccountRequest) *crypto_provider_account.CryptoProviderAccount {
			return req.ToEntity()
		},
		MapUpdateDTO: func(req cryptodto.UpdateCryptoProviderAccountRequest, existing *crypto_provider_account.CryptoProviderAccount) *crypto_provider_account.CryptoProviderAccount {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(account *crypto_provider_account.CryptoProviderAccount) any {
			return cryptodto.FromCryptoProviderAccount(account)
		},
	})
}
