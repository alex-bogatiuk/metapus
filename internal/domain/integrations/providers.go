package integrations

import (
	"slices"
	"time"
)

// Kind describes a supported use, not a separate account or quota owner.
type Kind string

const (
	KindExchangeRates Kind = "exchange_rates"
	KindNotifications Kind = "notifications"
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
	AccountMinRequestIntervalMS int
	// ExhaustionRecheck bounds the wait before a recovery probe when the gateway's
	// credit renewal does not reliably align with the configured billing calendar.
	ExhaustionRecheck time.Duration
	// PoolSelectionDenied, when set, is the reason the provider's accounts are
	// never chosen from a pool: its own subsystem pins them.
	PoolSelectionDenied string
}

// ManualPeriod reports a key period set by the operator instead of a calendar.
func (d ProviderDefinition) ManualPeriod() bool { return d.QuotaPolicy == QuotaManual }

// Platform providers: the exchange rate feed and automation channels. A
// configuration adds its own through NewCatalog.
const (
	CoinGeckoDemo Provider = "coingecko_demo"
	Telegram      Provider = "telegram"
	SMTP          Provider = "smtp"
)

// PlatformKinds lists the uses every configuration has.
func PlatformKinds() []KindDefinition {
	return []KindDefinition{
		{KindExchangeRates, "Курсы валют", "Получение котировок для расчётов и оценки активов."},
		{KindNotifications, "Уведомления", "Отправка сообщений через внешние сервисы."},
	}
}

// PlatformProviders lists the providers every configuration has.
func PlatformProviders() []ProviderDefinition {
	return []ProviderDefinition{
		{ID: CoinGeckoDemo, Label: "CoinGecko Demo", Description: "Котировки CoinGecko через Demo API.", Kinds: []Kind{KindExchangeRates}, CanCreate: true, QuotaPolicy: QuotaMonthly},
		{ID: Telegram, Label: "Telegram", Description: "Уведомления настраиваются в отдельной подсистеме автоматизации.", Kinds: []Kind{KindNotifications}, QuotaPolicy: QuotaMonthly},
		{ID: SMTP, Label: "SMTP", Description: "Отправка почты настраивается в отдельной подсистеме автоматизации.", Kinds: []Kind{KindNotifications}, QuotaPolicy: QuotaMonthly},
	}
}

func cloneProvider(d ProviderDefinition) ProviderDefinition {
	d.Kinds = slices.Clone(d.Kinds)
	d.Credentials.Fields = slices.Clone(d.Credentials.Fields)
	d.Credentials.URLImports = slices.Clone(d.Credentials.URLImports)
	for i := range d.Credentials.URLImports {
		rolePaths := d.Credentials.URLImports[i].RolePaths
		if rolePaths != nil {
			copied := make(map[string]string, len(rolePaths))
			for k, v := range rolePaths {
				copied[k] = v
			}
			d.Credentials.URLImports[i].RolePaths = copied
		}
	}
	return d
}
