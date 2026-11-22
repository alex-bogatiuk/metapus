// Package cryptointegrations declares the integration providers of the
// crypto-processing configuration: blockchain RPC operators, AML/KYT checks
// and resource rental. Together with the platform providers they form the
// application's integrations.Catalog.
package cryptointegrations

import (
	"sync"
	"time"

	"metapus/pkg/sdk/integrations"
)

// Kinds of integration the crypto configuration adds to the platform's.
const (
	KindRPC       integrations.Kind = "rpc"
	KindAML       integrations.Kind = "aml_kyt"
	KindResources integrations.Kind = "blockchain_resources"
)

// Providers of the crypto configuration. Identifiers are stored in tenant
// databases and must stay stable.
const (
	Alchemy         integrations.Provider = "alchemy"
	Ankr            integrations.Provider = "ankr"
	ValidationCloud integrations.Provider = "validation_cloud"
	ZAN             integrations.Provider = "zan"
	Infura          integrations.Provider = "infura"
	DRPC            integrations.Provider = "drpc"
	TronAPI         integrations.Provider = "tron_api"
	Chainstack      integrations.Provider = "chainstack"
	GetBlock        integrations.Provider = "getblock"
	GetBlockNet     integrations.Provider = "getblock_net"
	TestnetAML      integrations.Provider = "testnet_aml"
	ResourceAPI     integrations.Provider = "resource_api"
)

const amlPoolSelectionDenied = "AML использует отдельный пакет и закреплённый аккаунт"

// Kinds lists the configuration's kinds; the platform's come from integrations.PlatformKinds.
func Kinds() []integrations.KindDefinition {
	return []integrations.KindDefinition{
		{ID: KindAML, Label: "Проверка риска — AML/KYT", Description: "Оценка риска адресов и входящих криптопереводов."},
		{ID: KindRPC, Label: "Доступ к блокчейну — RPC", Description: "Чтение блоков, проверка транзакций и отправка операций в блокчейн."},
		{ID: KindResources, Label: "Ресурсы блокчейна", Description: "Аренда энергии и других ресурсов блокчейна."},
	}
}

// Providers lists the configuration's providers. RPC providers' supported
// networks, roles and addresses live in rpcconfig.
func Providers() []integrations.ProviderDefinition {
	zan := monthlyRPC(ZAN, "ZAN", "Ethereum Sepolia и TRON Nile.", "default",
		"Ключ проекта или HTTPS-ссылка Sepolia или Nile из ZAN.",
		integrations.CredentialURLImport{Host: "api.zan.top", PathPrefix: "/node/v1/eth/sepolia"},
		integrations.CredentialURLImport{Host: "api.zan.top", PathPrefix: "/node/v1/tron/nile", RolePaths: map[string]string{"default": "jsonrpc"}})
	// Free credits renew every 30 days from activation, independently of calendar months.
	zan.ExhaustionRecheck = time.Hour
	return []integrations.ProviderDefinition{
		{ID: DRPC, Label: "dRPC", Description: "Ethereum Sepolia с ручным сроком ключа.", Kinds: []integrations.Kind{KindRPC}, CanCreate: true, QuotaPolicy: integrations.QuotaManual,
			Credentials:                 integrations.CredentialProfile{Fields: []integrations.CredentialField{{Name: "sepolia", Label: "Ключ dRPC", Description: "Ключ или HTTPS-ссылка Sepolia из dRPC."}}, ReplaceLabel: "Продлить ключ…", Description: "Новый ключ и новая дата сохраняются вместе. После срока доступ не возобновляется автоматически.", KeyFormat: integrations.CredentialURLSafe, RequireFields: true, SingleKey: true, KeyOnly: true, URLImports: []integrations.CredentialURLImport{{Host: "lb.drpc.live", PathPrefix: "/sepolia"}}},
			AccountMinRequestIntervalMS: 200},
		{ID: Alchemy, Label: "Alchemy", Description: "Доступ к EVM и TRON через RPC.", Kinds: []integrations.Kind{KindRPC}, CanCreate: true, QuotaPolicy: integrations.QuotaMonthly},
		monthlyRPC(ValidationCloud, "Validation Cloud", "Ethereum Sepolia.", "sepolia",
			"Ключ или HTTPS-ссылка Ethereum Sepolia из Validation Cloud.",
			integrations.CredentialURLImport{Host: "sepolia.ethereum.validationcloud.io", PathPrefix: "/v1"}),
		zan,
		{
			ID: Ankr, Label: "Ankr", Description: "Ethereum Sepolia по тарифу Freemium.",
			Kinds: []integrations.Kind{KindRPC}, CanCreate: true, QuotaPolicy: integrations.QuotaMonthly,
			Credentials: integrations.CredentialProfile{
				Fields:       []integrations.CredentialField{{Name: "sepolia", Label: "Ключ Ankr", Description: "Ключ проекта или HTTPS-ссылка Ethereum Sepolia из Ankr."}},
				ReplaceLabel: "Заменить ключ…", Description: "Укажите день и часовой пояс обновления месячной квоты аккаунта Ankr.",
				KeyFormat: integrations.CredentialURLSafe, RequireFields: true, SingleKey: true, KeyOnly: true,
				URLImports: []integrations.CredentialURLImport{{Host: "rpc.ankr.com", PathPrefix: "/eth_sepolia"}},
			},
			// Freemium Node API allows about 1800 requests/minute across endpoints.
			AccountMinRequestIntervalMS: 34,
		},
		{ID: Infura, Label: "Infura", Description: "Доступ к Ethereum Sepolia через RPC.", Kinds: []integrations.Kind{KindRPC}, CanCreate: true, QuotaPolicy: integrations.QuotaDailyUTC, Credentials: integrations.CredentialProfile{
			Fields:       []integrations.CredentialField{{Name: "sepolia", Label: "Ключ Infura", Description: "Ключ из 32 шестнадцатеричных символов или HTTPS-ссылка Ethereum Sepolia из Infura."}},
			ReplaceLabel: "Заменить ключ…", Description: "Ссылка преобразуется в ключ. Суточная квота аккаунта обновляется в 00:00 UTC.", InputFormat: "infura_key", KeyFormat: integrations.CredentialHex32,
			URLImports: []integrations.CredentialURLImport{{Host: "sepolia.infura.io", PathPrefix: "/v3"}},
		}, AccountMinRequestIntervalMS: 1000},
		{ID: TronAPI, Label: "TRON API / TronGrid", Description: "Доступ к сети TRON через API TronGrid.", Kinds: []integrations.Kind{KindRPC}, CanCreate: true, QuotaPolicy: integrations.QuotaMonthly},
		{ID: Chainstack, Label: "Chainstack", Description: "Доступ к TRON Nile и Ethereum Sepolia.", Kinds: []integrations.Kind{KindRPC}, CanCreate: true, QuotaPolicy: integrations.QuotaMonthly,
			Credentials: integrations.CredentialProfile{KeyFormat: integrations.CredentialURLSafe, Description: "Введите только токен, без полного URL и суффикса API. Имя nile — для TRON Nile, sepolia — для Ethereum Sepolia."}, AccountMinRequestIntervalMS: 200},
		{ID: GetBlock, Label: "Getblock.io", Description: "Доступ к TRON Nile через FullNode и Solidity REST.", Kinds: []integrations.Kind{KindRPC}, CanCreate: true, QuotaPolicy: integrations.QuotaMonthly, Credentials: integrations.CredentialProfile{
			Fields: []integrations.CredentialField{
				{Name: "fullnode", Label: "Токен FullNode REST", Description: "Из эндпоинта REST (Fullnode) нужной сети TRON."},
				{Name: "solidity", Label: "Токен Solidity REST", Description: "Из эндпоинта REST (Solidity) той же сети TRON."},
			},
			ReplaceLabel: "Заменить оба токена…", Description: "Оба токена одного аккаунта GetBlock используют общую квоту. Имена ключей назначаются автоматически.", InputFormat: "getblock_token", KeyFormat: integrations.CredentialURLSafe, RequireFields: true,
			URLImports: []integrations.CredentialURLImport{{Host: "shared.eu-central-1.getblock.io", RolePaths: map[string]string{"fullnode": "wallet", "solidity": "walletsolidity"}}},
		}, AccountMinRequestIntervalMS: 200},
		{ID: GetBlockNet, Label: "Getblock.net", Description: "AML/KYT-проверки криптоактивов. Отдельный аккаунт и пакет проверок.", Kinds: []integrations.Kind{KindAML}, CanCreate: true, QuotaPolicy: integrations.QuotaPackage, PoolSelectionDenied: amlPoolSelectionDenied, Credentials: integrations.CredentialProfile{
			Fields:       []integrations.CredentialField{{Name: "default", Label: "Bearer token", Description: "Токен API из аккаунта Getblock.net."}},
			ReplaceLabel: "Заменить токен…", Description: "Проверки расходуются из пакета. Календарного сброса нет.", InputFormat: "bearer_token", KeyFormat: integrations.CredentialBearer, RequireFields: true, SingleKey: true, KeyOnly: true,
		}},
		{ID: TestnetAML, Label: "AML эмулятор (testnet)", Description: "Синтетические AML-ответы только для TRON Nile и Ethereum Sepolia. Настраивается через control plane.", Kinds: []integrations.Kind{KindAML}, QuotaPolicy: integrations.QuotaPackage, PoolSelectionDenied: amlPoolSelectionDenied, Credentials: integrations.CredentialProfile{Description: "Ключи провайдера не используются."}},
		{ID: ResourceAPI, Label: "API ресурсов", Description: "Поставщики ресурсов настраиваются в справочнике аккаунтов криптопровайдеров.", Kinds: []integrations.Kind{KindResources}, QuotaPolicy: integrations.QuotaMonthly},
	}
}

// monthlyRPC describes an RPC account with one project key and a monthly quota.
func monthlyRPC(provider integrations.Provider, label, description, slot, keyHelp string, imports ...integrations.CredentialURLImport) integrations.ProviderDefinition {
	return integrations.ProviderDefinition{
		ID: provider, Label: label, Description: description, Kinds: []integrations.Kind{KindRPC},
		CanCreate: true, QuotaPolicy: integrations.QuotaMonthly,
		Credentials: integrations.CredentialProfile{
			Fields:       []integrations.CredentialField{{Name: slot, Label: "Ключ " + label, Description: keyHelp}},
			ReplaceLabel: "Заменить ключ…", KeyFormat: integrations.CredentialURLSafe,
			RequireFields: true, SingleKey: true, KeyOnly: true, URLImports: imports,
		},
	}
}

var catalog = sync.OnceValue(func() *integrations.Catalog {
	kinds := map[integrations.Kind]integrations.KindDefinition{}
	for _, kind := range append(integrations.PlatformKinds(), Kinds()...) {
		kinds[kind.ID] = kind
	}
	providers := map[integrations.Provider]integrations.ProviderDefinition{}
	for _, provider := range append(integrations.PlatformProviders(), Providers()...) {
		providers[provider.ID] = provider
	}
	orderedKinds := make([]integrations.KindDefinition, 0, len(kinds))
	for _, id := range kindOrder {
		orderedKinds = append(orderedKinds, kinds[id])
	}
	orderedProviders := make([]integrations.ProviderDefinition, 0, len(providers))
	for _, id := range providerOrder {
		orderedProviders = append(orderedProviders, providers[id])
	}
	if len(orderedKinds) != len(kinds) || len(orderedProviders) != len(providers) {
		panic("cryptointegrations: listing order misses a kind or provider")
	}
	return integrations.MustCatalog(orderedKinds, orderedProviders)
})

// kindOrder and providerOrder fix the listing order of the settings UI.
var (
	kindOrder     = []integrations.Kind{KindAML, KindRPC, integrations.KindExchangeRates, integrations.KindNotifications, KindResources}
	providerOrder = []integrations.Provider{DRPC, Alchemy, Ankr, Infura, ValidationCloud, ZAN, integrations.CoinGeckoDemo, TronAPI, Chainstack, GetBlock, GetBlockNet, TestnetAML, integrations.Telegram, integrations.SMTP, ResourceAPI}
)

// Catalog is the application's catalog: platform and crypto kinds and
// providers, in the order the settings UI lists them. It is immutable.
func Catalog() *integrations.Catalog { return catalog() }

// ValidGetBlockNetToken accepts an opaque bearer token, never an RPC URL or
// header prefix. Format-specific IDs are intentionally not guessed.
func ValidGetBlockNetToken(value string) bool {
	return integrations.CredentialBearer.Valid(value)
}
