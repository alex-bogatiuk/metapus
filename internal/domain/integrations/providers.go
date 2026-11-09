package integrations

import (
	"maps"
	"slices"

	"metapus/internal/core/apperror"
)

// Kind describes a supported use, not a separate account or quota owner.
type Kind string

const (
	KindRPC           Kind = "rpc"
	KindExchangeRates Kind = "exchange_rates"
	KindNotifications Kind = "notifications"
	KindResources     Kind = "blockchain_resources"
	KindAML           Kind = "aml_kyt"
)

type KindDefinition struct {
	ID          Kind
	Label       string
	Description string
}

type QuotaPolicy string

const (
	QuotaMonthly  QuotaPolicy = "monthly"
	QuotaDailyUTC QuotaPolicy = "daily_utc"
	QuotaPackage  QuotaPolicy = "package"
	QuotaManual   QuotaPolicy = "manual"
)

type CredentialField struct {
	Name        string
	Label       string
	Description string
}

type CredentialProfile struct {
	Fields        []CredentialField
	ReplaceLabel  string
	Description   string
	InputFormat   string
	KeyFormat     CredentialKeyFormat
	RequireFields bool
	SingleKey     bool
	KeyOnly       bool
	URLImports    []CredentialURLImport
}

// CredentialURLImport describes extraction only; the UI never visits the URL.
type CredentialURLImport struct {
	Host       string
	PathPrefix string
	RolePaths  map[string]string
}

// ProviderDefinition is the shared contract for validation, metadata and UI.
// Existing identifiers remain stable, including legacy profiles whose consumers
// have not yet moved to the integration account mechanism.
type ProviderDefinition struct {
	ID                          Provider
	Label                       string
	Description                 string
	Kinds                       []Kind
	CanCreate                   bool
	Credentials                 CredentialProfile
	QuotaPolicy                 QuotaPolicy
	RPCProfiles                 []RPCProfile
	AccountMinRequestIntervalMS int
	ManagedRPCOnly              bool
}

var kindDefinitions = []KindDefinition{
	{KindAML, "Проверка риска — AML/KYT", "Оценка риска адресов и входящих криптопереводов."},
	{KindRPC, "Доступ к блокчейну — RPC", "Чтение блоков, проверка транзакций и отправка операций в блокчейн."},
	{KindExchangeRates, "Курсы валют", "Получение котировок для расчётов и оценки активов."},
	{KindNotifications, "Уведомления", "Отправка сообщений через внешние сервисы."},
	{KindResources, "Ресурсы блокчейна", "Аренда энергии и других ресурсов блокчейна."},
}

var providerDefinitions = []ProviderDefinition{
	{ID: DRPC, Label: "dRPC", Description: "Ethereum Sepolia с ручным сроком ключа.", Kinds: []Kind{KindRPC}, CanCreate: true, ManagedRPCOnly: true, QuotaPolicy: QuotaManual,
		Credentials:                 CredentialProfile{Fields: []CredentialField{{Name: "sepolia", Label: "Ключ dRPC", Description: "Ключ или HTTPS-ссылка Sepolia из dRPC."}}, ReplaceLabel: "Продлить ключ…", Description: "Новый ключ и новая дата сохраняются вместе. После срока доступ не возобновляется автоматически.", KeyFormat: CredentialURLSafe, RequireFields: true, SingleKey: true, KeyOnly: true, URLImports: []CredentialURLImport{{Host: "lb.drpc.live", PathPrefix: "/sepolia"}}},
		AccountMinRequestIntervalMS: 200,
		RPCProfiles:                 []RPCProfile{{Family: "evm", NetworkIdentifier: "11155111", Roles: []string{"rpc"}, Host: "lb.drpc.live", BasePath: "/sepolia", TrustGroup: "drpc"}}},
	{ID: Alchemy, ManagedRPCOnly: true, Label: "Alchemy", Description: "Доступ к EVM и TRON через RPC.", Kinds: []Kind{KindRPC}, CanCreate: true, QuotaPolicy: QuotaMonthly,
		RPCProfiles: []RPCProfile{
			{Family: "evm", Roles: []string{"rpc"}, HostSuffix: ".g.alchemy.com", BasePath: "/v2", TrustGroup: "alchemy"},
			{Family: "tron", NetworkIdentifier: "mainnet", Roles: []string{"fullnode", "solidity"}, Host: "tron-mainnet.g.alchemy.com", BasePath: "/v2", TrustGroup: "alchemy"},
			{Family: "tron", NetworkIdentifier: "nile", Roles: []string{"fullnode", "solidity"}, Host: "tron-testnet.g.alchemy.com", BasePath: "/v2", TrustGroup: "alchemy"},
		}},
	{ID: Infura, ManagedRPCOnly: true, Label: "Infura", Description: "Доступ к Ethereum Sepolia через RPC.", Kinds: []Kind{KindRPC}, CanCreate: true, QuotaPolicy: QuotaDailyUTC, Credentials: CredentialProfile{
		Fields:       []CredentialField{{Name: "sepolia", Label: "Ключ Infura", Description: "Ключ из 32 шестнадцатеричных символов или HTTPS-ссылка Ethereum Sepolia из Infura."}},
		ReplaceLabel: "Заменить ключ…", Description: "Ссылка преобразуется в ключ. Суточная квота аккаунта обновляется в 00:00 UTC.", InputFormat: "infura_key", KeyFormat: CredentialHex32,
		URLImports: []CredentialURLImport{{Host: "sepolia.infura.io", PathPrefix: "/v3"}},
	}, AccountMinRequestIntervalMS: 1000,
		RPCProfiles: []RPCProfile{{Family: "evm", NetworkIdentifier: "11155111", Roles: []string{"rpc"}, Host: "sepolia.infura.io", BasePath: "/v3", TrustGroup: "infura"}}},
	{ID: CoinGeckoDemo, Label: "CoinGecko Demo", Description: "Котировки CoinGecko через Demo API.", Kinds: []Kind{KindExchangeRates}, CanCreate: true, QuotaPolicy: QuotaMonthly},
	{ID: TronAPI, Label: "TRON API / TronGrid", Description: "Доступ к сети TRON через API TronGrid.", Kinds: []Kind{KindRPC}, CanCreate: true, QuotaPolicy: QuotaMonthly,
		RPCProfiles: []RPCProfile{
			{Family: "tron", NetworkIdentifier: "nile", Roles: []string{"fullnode", "solidity", "indexer"}, Host: "nile.trongrid.io", TrustGroup: "trongrid"},
			{Family: "tron", NetworkIdentifier: "mainnet", Roles: []string{"fullnode", "solidity", "indexer"}, Host: "api.trongrid.io", TrustGroup: "trongrid"},
		}},
	{ID: Chainstack, ManagedRPCOnly: true, Label: "Chainstack", Description: "Доступ к TRON Nile и Ethereum Sepolia.", Kinds: []Kind{KindRPC}, CanCreate: true, QuotaPolicy: QuotaMonthly,
		Credentials: CredentialProfile{KeyFormat: CredentialURLSafe, Description: "Введите только токен, без полного URL и суффикса API. Имя nile — для TRON Nile, sepolia — для Ethereum Sepolia."}, AccountMinRequestIntervalMS: 200,
		RPCProfiles: []RPCProfile{
			{Family: "tron", NetworkIdentifier: "nile", Roles: []string{"fullnode", "solidity"}, Host: "tron-nile.core.chainstack.com", TrustGroup: "chainstack"},
			{Family: "evm", NetworkIdentifier: "11155111", Roles: []string{"rpc"}, Host: "ethereum-sepolia.core.chainstack.com", TrustGroup: "chainstack"},
		}},
	{ID: GetBlock, ManagedRPCOnly: true, Label: "Getblock.io", Description: "Доступ к TRON Nile через FullNode и Solidity REST.", Kinds: []Kind{KindRPC}, CanCreate: true, QuotaPolicy: QuotaMonthly, Credentials: CredentialProfile{
		Fields: []CredentialField{
			{Name: "fullnode", Label: "Токен FullNode REST", Description: "Из эндпоинта REST (Fullnode) нужной сети TRON."},
			{Name: "solidity", Label: "Токен Solidity REST", Description: "Из эндпоинта REST (Solidity) той же сети TRON."},
		},
		ReplaceLabel: "Заменить оба токена…", Description: "Оба токена одного аккаунта GetBlock используют общую квоту. Имена ключей назначаются автоматически.", InputFormat: "getblock_token", KeyFormat: CredentialURLSafe, RequireFields: true,
		URLImports: []CredentialURLImport{{Host: "shared.eu-central-1.getblock.io", RolePaths: map[string]string{"fullnode": "wallet", "solidity": "walletsolidity"}}},
	}, AccountMinRequestIntervalMS: 200,
		RPCProfiles: []RPCProfile{{Family: "tron", NetworkIdentifier: "nile", Roles: []string{"fullnode", "solidity"}, Host: "shared.eu-central-1.getblock.io", TrustGroup: "getblock", SlotByRole: true}}},
	{ID: GetBlockNet, Label: "Getblock.net", Description: "AML/KYT-проверки криптоактивов. Отдельный аккаунт и пакет проверок.", Kinds: []Kind{KindAML}, CanCreate: true, QuotaPolicy: QuotaPackage, Credentials: CredentialProfile{
		Fields:       []CredentialField{{Name: "default", Label: "Bearer token", Description: "Токен API из аккаунта Getblock.net."}},
		ReplaceLabel: "Заменить токен…", Description: "Проверки расходуются из пакета. Календарного сброса нет.", InputFormat: "bearer_token", KeyFormat: CredentialBearer, RequireFields: true, SingleKey: true, KeyOnly: true,
	}},
	{ID: TestnetAML, Label: "AML эмулятор (testnet)", Description: "Синтетические AML-ответы только для TRON Nile и Ethereum Sepolia. Настраивается через control plane.", Kinds: []Kind{KindAML}, QuotaPolicy: QuotaPackage, Credentials: CredentialProfile{Description: "Ключи провайдера не используются."}},
	{ID: Telegram, Label: "Telegram", Description: "Уведомления настраиваются в отдельной подсистеме автоматизации.", Kinds: []Kind{KindNotifications}, QuotaPolicy: QuotaMonthly},
	{ID: SMTP, Label: "SMTP", Description: "Отправка почты настраивается в отдельной подсистеме автоматизации.", Kinds: []Kind{KindNotifications}, QuotaPolicy: QuotaMonthly},
	{ID: ResourceAPI, Label: "API ресурсов", Description: "Поставщики ресурсов настраиваются в справочнике аккаунтов криптопровайдеров.", Kinds: []Kind{KindResources}, QuotaPolicy: QuotaMonthly},
}

func KindDefinitions() []KindDefinition { return slices.Clone(kindDefinitions) }

func cloneProvider(d ProviderDefinition) ProviderDefinition {
	d.Kinds = slices.Clone(d.Kinds)
	d.Credentials.Fields = slices.Clone(d.Credentials.Fields)
	d.Credentials.URLImports = slices.Clone(d.Credentials.URLImports)
	for i := range d.Credentials.URLImports {
		d.Credentials.URLImports[i].RolePaths = maps.Clone(d.Credentials.URLImports[i].RolePaths)
	}
	d.RPCProfiles = slices.Clone(d.RPCProfiles)
	for i := range d.RPCProfiles {
		d.RPCProfiles[i].Roles = slices.Clone(d.RPCProfiles[i].Roles)
	}
	return d
}

func ProviderDefinitions() []ProviderDefinition {
	result := make([]ProviderDefinition, len(providerDefinitions))
	for i, definition := range providerDefinitions {
		result[i] = cloneProvider(definition)
	}
	return result
}

func (p Provider) Definition() (ProviderDefinition, bool) {
	for _, definition := range providerDefinitions {
		if definition.ID == p {
			return cloneProvider(definition), true
		}
	}
	return ProviderDefinition{}, false
}

func (p Provider) Supports(kind Kind) bool {
	definition, ok := p.Definition()
	return ok && slices.Contains(definition.Kinds, kind)
}

func (p Provider) ValidateCreation() error {
	definition, ok := p.Definition()
	if !ok || !definition.CanCreate {
		return apperror.NewValidation("Выберите провайдера, поддерживаемого аккаунтами интеграций").WithDetail("field", "provider")
	}
	return nil
}
