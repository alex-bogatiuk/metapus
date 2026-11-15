// Package content contains concrete Registration implementations for all
// built-in entity types shipped with Metapus ("business content" layer).
//
// Platform Core interfaces live in internal/platform/ and v1/.
// Client extensions DO NOT modify this package — they register their own
// entities via FactoryRegistry from their own Go module.
package content

import (
	"context"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"

	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/blockchain_network"
	"metapus/internal/domain/catalogs/contract"
	"metapus/internal/domain/catalogs/counterparty"
	"metapus/internal/domain/catalogs/crypto_provider_account"
	"metapus/internal/domain/catalogs/currency"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/catalogs/organization"
	"metapus/internal/domain/catalogs/rate_source"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/http/cryptohttp/cryptodto"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/tenantdb"
	"metapus/internal/metadata"
	"metapus/internal/platform"
)

func init() {
	// Register Enum metadata globally for automatic filter UI dropdown resolution.

	// Counterparty Types
	metadata.RegisterEnum[counterparty.CounterpartyType]([]metadata.EnumValue{
		{Value: "customer", Label: "Покупатель"},
		{Value: "supplier", Label: "Поставщик"},
		{Value: "both", Label: "Покупатель и Поставщик"},
		{Value: "other", Label: "Прочие"},
	})

	// Legal Forms
	metadata.RegisterEnum[counterparty.LegalForm]([]metadata.EnumValue{
		{Value: "individual", Label: "Физлицо"},
		{Value: "sole_trader", Label: "ИП"},
		{Value: "company", Label: "Юрлицо"},
		{Value: "government", Label: "Гос. орган"},
	})

	// Contract Types
	metadata.RegisterEnum[contract.ContractType]([]metadata.EnumValue{
		{Value: "supply", Label: "С поставщиком"},
		{Value: "sale", Label: "С покупателем"},
		{Value: "other", Label: "Прочее"},
	})

	// Organization Tax Systems
	metadata.RegisterEnum[organization.TaxSystem]([]metadata.EnumValue{
		{Value: "osno", Label: "ОСНО"},
		{Value: "usn_income", Label: "УСН (доходы)"},
		{Value: "usn_income_expense", Label: "УСН (доходы − расходы)"},
		{Value: "envd", Label: "ЕНВД"},
		{Value: "patent", Label: "Патент"},
	})

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

	// Rate Source Types
	metadata.RegisterEnum[rate_source.SourceType]([]metadata.EnumValue{
		{Value: "coingecko", Label: "CoinGecko"},
		{Value: "binance", Label: "Binance"},
		{Value: "coinmarketcap", Label: "CoinMarketCap"},
		{Value: "manual", Label: "Ручной ввод"},
	})

	// Blockchain Network Chain Families
	metadata.RegisterEnum[crypto.ChainFamily]([]metadata.EnumValue{
		{Value: string(crypto.ChainFamilyTRON), Label: "TRON"},
		{Value: string(crypto.ChainFamilyEVM), Label: "EVM (Ethereum, Polygon, Arbitrum, BSC)"},
		{Value: string(crypto.ChainFamilyTON), Label: "TON"},
		{Value: string(crypto.ChainFamilySolana), Label: "Solana"},
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

// ---------------------------------------------------------------------------
// Counterparty
// ---------------------------------------------------------------------------

type CounterpartyRegistration struct {
	v1.CatalogRegistrationFor[counterparty.Counterparty, dto.CreateCounterpartyRequest, dto.UpdateCounterpartyRequest, dto.CounterpartyResponse]
}

func (r *CounterpartyRegistration) ReferenceTypes() []string { return []string{"supplier", "customer"} }
func (r *CounterpartyRegistration) EntityLabel() string      { return "Контрагенты" }
func (r *CounterpartyRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Контрагент",
		Plural:   "Контрагенты",
		NewLabel: "Новый контрагент",
		Genitive: "контрагента",
	}
}
func (r *CounterpartyRegistration) SearchableFields() platform.SearchFields {
	return platform.SearchFields{
		SearchCols:  []string{"name", "code", "inn", "phone", "email"},
		TitleCol:    "name",
		SubtitleCol: "code",
	}
}

func (r *CounterpartyRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewCounterpartyRepo()
	service := counterparty.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, counterparty.Definition.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*counterparty.Counterparty,
		dto.CreateCounterpartyRequest,
		dto.UpdateCounterpartyRequest,
	]{
		Service:      service.CatalogService,
		EntityName:   counterparty.Definition.Key,
		MapCreateDTO: func(req dto.CreateCounterpartyRequest) *counterparty.Counterparty { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateCounterpartyRequest, existing *counterparty.Counterparty) *counterparty.Counterparty {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *counterparty.Counterparty) any { return dto.FromCounterparty(entity) },
	})
}

// ---------------------------------------------------------------------------
// Currency
// ---------------------------------------------------------------------------

type CurrencyRegistration struct {
	v1.CatalogRegistrationFor[currency.Currency, dto.CreateCurrencyRequest, dto.UpdateCurrencyRequest, dto.CurrencyResponse]
}

func (r *CurrencyRegistration) ReferenceTypes() []string { return []string{"currency"} }
func (r *CurrencyRegistration) EntityLabel() string      { return "Валюты" }
func (r *CurrencyRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Валюта",
		Plural:   "Валюты",
		NewLabel: "Новая валюта",
		Genitive: "валюты",
	}
}
func (r *CurrencyRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewCurrencyRepo()
	service := currency.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, currency.Definition.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*currency.Currency,
		dto.CreateCurrencyRequest,
		dto.UpdateCurrencyRequest,
	]{
		Service:      service.CatalogService,
		EntityName:   currency.Definition.Key,
		MapCreateDTO: func(req dto.CreateCurrencyRequest) *currency.Currency { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateCurrencyRequest, existing *currency.Currency) *currency.Currency {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *currency.Currency) any { return dto.FromCurrency(entity) },
	})
}

// ---------------------------------------------------------------------------
// Organization
// ---------------------------------------------------------------------------

type OrganizationRegistration struct {
	v1.CatalogRegistrationFor[organization.Organization, dto.CreateOrganizationRequest, dto.UpdateOrganizationRequest, dto.OrganizationResponse]
}

func (r *OrganizationRegistration) ReferenceTypes() []string { return []string{"organization"} }
func (r *OrganizationRegistration) EntityLabel() string      { return "Организации" }
func (r *OrganizationRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Организация",
		Plural:   "Организации",
		NewLabel: "Новая организация",
		Genitive: "организации",
	}
}
func (r *OrganizationRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewOrganizationRepo()
	service := organization.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, organization.Definition.Key, deps.EventWriter)

	// Invalidate CurrencyResolver cache when org's base currency changes
	if deps.CurrencyCacheInvalidator != nil {
		inv := deps.CurrencyCacheInvalidator
		service.Hooks().OnAfterUpdate(func(_ context.Context, org *organization.Organization) error {
			inv.InvalidateOrgCurrency(org.ID)
			return nil
		})
	}

	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*organization.Organization,
		dto.CreateOrganizationRequest,
		dto.UpdateOrganizationRequest,
	]{
		Service:      service.CatalogService,
		EntityName:   organization.Definition.Key,
		MapCreateDTO: func(req dto.CreateOrganizationRequest) *organization.Organization { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateOrganizationRequest, existing *organization.Organization) *organization.Organization {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO:    func(entity *organization.Organization) any { return dto.FromOrganization(entity) },
		ResolveRefs: resolveOrganizationRefs,
		MapToDTOWithRefs: func(entity *organization.Organization, refs any) any {
			return dto.FromOrganization(entity, refs.(postgres.ResolvedRefs))
		},
	})
}

// ---------------------------------------------------------------------------
// Contract
// ---------------------------------------------------------------------------

type ContractRegistration struct {
	v1.CatalogRegistrationFor[contract.Contract, dto.CreateContractRequest, dto.UpdateContractRequest, dto.ContractResponse]
}

func (r *ContractRegistration) ReferenceTypes() []string { return []string{"contract"} }
func (r *ContractRegistration) EntityLabel() string      { return "Договоры" }
func (r *ContractRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Договор",
		Plural:   "Договоры",
		NewLabel: "Новый договор",
		Genitive: "договора",
	}
}
func (r *ContractRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewContractRepo()
	service := contract.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, contract.Definition.Key, deps.EventWriter)

	// Invalidate CurrencyResolver cache when contract's currency changes
	if deps.CurrencyCacheInvalidator != nil {
		inv := deps.CurrencyCacheInvalidator
		service.Hooks().OnAfterUpdate(func(_ context.Context, c *contract.Contract) error {
			inv.InvalidateContractCurrency(c.ID)
			return nil
		})
	}

	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*contract.Contract,
		dto.CreateContractRequest,
		dto.UpdateContractRequest,
	]{
		Service:      service.CatalogService,
		EntityName:   contract.Definition.Key,
		MapCreateDTO: func(req dto.CreateContractRequest) *contract.Contract { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateContractRequest, existing *contract.Contract) *contract.Contract {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO:    func(entity *contract.Contract) any { return dto.FromContract(entity) },
		ResolveRefs: resolveContractRefs,
		MapToDTOWithRefs: func(entity *contract.Contract, refs any) any {
			return dto.FromContract(entity, refs.(postgres.ResolvedRefs))
		},
	})
}

// ── ResolveRefs callbacks for catalogs with FK references ───────────────

func resolveCatalogRefs[T any](ctx context.Context, collect func(*postgres.ReferenceResolver, T), entities ...T) (any, error) {
	resolver := postgres.NewReferenceResolver()
	for _, e := range entities {
		collect(resolver, e)
	}
	pool := tenantdb.MustGetPool(ctx)
	return resolver.Resolve(ctx, pool)
}

func resolveOrganizationRefs(ctx context.Context, entities ...*organization.Organization) (any, error) {
	return resolveCatalogRefs(ctx, dto.CollectOrganizationRefs, entities...)
}

func resolveContractRefs(ctx context.Context, entities ...*contract.Contract) (any, error) {
	return resolveCatalogRefs(ctx, dto.CollectContractRefs, entities...)
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
	repo := catalog_repo.NewBlockchainNetworkRepo()
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

type TokenRegistration struct {
	v1.CatalogRegistrationFor[token.Token, cryptodto.CreateTokenRequest, cryptodto.UpdateTokenRequest, cryptodto.CryptoTokenResponse]
}

func (r *TokenRegistration) ReferenceTypes() []string { return []string{"token"} }
func (r *TokenRegistration) EntityLabel() string      { return "Токены" }
func (r *TokenRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Токен",
		Plural:   "Токены",
		NewLabel: "Новый токен",
		Genitive: "токена",
	}
}
func (r *TokenRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewTokenRepo()
	service := token.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, token.Definition.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*token.Token,
		cryptodto.CreateTokenRequest,
		cryptodto.UpdateTokenRequest,
	]{
		Service:      service.CatalogService,
		EntityName:   token.Definition.Key,
		MapCreateDTO: func(req cryptodto.CreateTokenRequest) *token.Token { return req.ToEntity() },
		MapUpdateDTO: func(req cryptodto.UpdateTokenRequest, existing *token.Token) *token.Token {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO:    func(entity *token.Token) any { return cryptodto.FromToken(entity) },
		ResolveRefs: resolveTokenRefs,
		MapToDTOWithRefs: func(entity *token.Token, refs any) any {
			return cryptodto.FromToken(entity, refs.(postgres.ResolvedRefs))
		},
	})
}

func resolveTokenRefs(ctx context.Context, entities ...*token.Token) (any, error) {
	return resolveCatalogRefs(ctx, cryptodto.CollectTokenRefs, entities...)
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
	repo := catalog_repo.NewMerchantRepo()
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
	repo := catalog_repo.NewWalletRepo()
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
	repo := catalog_repo.NewCryptoProviderAccountRepo()
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

// ---------------------------------------------------------------------------
// RateSource
// ---------------------------------------------------------------------------

type RateSourceRegistration struct {
	v1.CatalogRegistrationFor[rate_source.RateSource, dto.CreateRateSourceRequest, dto.UpdateRateSourceRequest, dto.RateSourceResponse]
}

func (r *RateSourceRegistration) ReferenceTypes() []string { return []string{"rate_source"} }
func (r *RateSourceRegistration) EntityLabel() string      { return "Источники курсов" }
func (r *RateSourceRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Источник курсов",
		Plural:   "Источники курсов",
		NewLabel: "Новый источник курсов",
		Genitive: "источника курсов",
	}
}
func (r *RateSourceRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := catalog_repo.NewRateSourceRepo()
	service := rate_source.NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, rate_source.Definition.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[
		*rate_source.RateSource,
		dto.CreateRateSourceRequest,
		dto.UpdateRateSourceRequest,
	]{
		Service:      service.CatalogService,
		EntityName:   rate_source.Definition.Key,
		MapCreateDTO: func(req dto.CreateRateSourceRequest) *rate_source.RateSource { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateRateSourceRequest, existing *rate_source.RateSource) *rate_source.RateSource {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *rate_source.RateSource) any { return dto.FromRateSource(entity) },
	})
}
