// Package integrations is the integration catalog a configuration adds its
// kinds and providers to.
package integrations

import (
	"github.com/alex-bogatiuk/metapus/internal/core/numerator"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	core "github.com/alex-bogatiuk/metapus/internal/domain/integrations"
)

type (
	Catalog      = core.Catalog
	PoolConsumer = core.PoolConsumer
)

type (
	Account = core.Account
	// AccountKeys lists key names, never values, of one pool account.
	AccountKeys = core.AccountKeys
	// ConnectionTarget is what a pool's connection check reads through. It
	// carries configuration only, never resolved credentials. A pool read through
	// a configuration's endpoint (for example an RPC connection) names it by
	// EndpointID and URL; Attributes carry what that configuration's probe needs
	// and are opaque to the platform.
	ConnectionTarget    = core.ConnectionTarget
	CredentialField     = core.CredentialField
	CredentialKeyFormat = core.CredentialKeyFormat
	CredentialProfile   = core.CredentialProfile
	// CredentialURLImport describes extraction only; the UI never visits the URL.
	CredentialURLImport = core.CredentialURLImport
	// Credentials is write-only at the HTTP boundary. Named keys share the account quota.
	Credentials = core.Credentials
	Failure     = core.Failure
	// Kind describes a supported use, not a separate account or quota owner.
	Kind           = core.Kind
	KindDefinition = core.KindDefinition
	NamedKey       = core.NamedKey
	Pool           = core.Pool
	// PoolUnavailableError describes account availability without disclosing credentials.
	// KeyMissing means accounts were skipped because they lack the requested key
	// name; an empty Reason means no other account state explains the failure.
	PoolUnavailableError = core.PoolUnavailableError
	// Provider identifies a ProviderDefinition of the application's Catalog.
	Provider = core.Provider
	// ProviderDefinition is the shared contract for validation, metadata and UI.
	// Existing identifiers remain stable, including legacy profiles whose consumers
	// have not yet moved to the integration account mechanism.
	ProviderDefinition = core.ProviderDefinition
	Selection          = core.Selection
)

const (
	CoinGeckoDemo     = core.CoinGeckoDemo
	CredentialBearer  = core.CredentialBearer
	CredentialHex32   = core.CredentialHex32
	CredentialURLSafe = core.CredentialURLSafe
	Exhausted         = core.Exhausted
	// KeyMissing means pool accounts lack the key name the endpoint reads.
	KeyMissing        = core.KeyMissing
	KindExchangeRates = core.KindExchangeRates
	KindNotifications = core.KindNotifications
	QuotaDailyUTC     = core.QuotaDailyUTC
	QuotaManual       = core.QuotaManual
	QuotaMonthly      = core.QuotaMonthly
	QuotaPackage      = core.QuotaPackage
	SMTP              = core.SMTP
	Telegram          = core.Telegram
	Throttled         = core.Throttled
	Unauthorized      = core.Unauthorized
)

// MustCatalog is NewCatalog for definitions fixed in code.
func MustCatalog(kinds []core.KindDefinition, providers []core.ProviderDefinition) *core.Catalog {
	return core.MustCatalog(kinds, providers)
}

func NewAccount(code string, name string) *core.Account { return core.NewAccount(code, name) }

// NewAccountService validates accounts against the catalog's provider rules.
func NewAccountService(repo core.AccountRepository, num numerator.Generator, catalog *core.Catalog) *domain.CatalogService[*core.Account] {
	return core.NewAccountService(repo, num, catalog)
}

func NewPool(code string, name string) *core.Pool { return core.NewPool(code, name) }

// PlatformKinds lists the uses every configuration has.
func PlatformKinds() []core.KindDefinition { return core.PlatformKinds() }

// PlatformProviders lists the providers every configuration has.
func PlatformProviders() []core.ProviderDefinition { return core.PlatformProviders() }
