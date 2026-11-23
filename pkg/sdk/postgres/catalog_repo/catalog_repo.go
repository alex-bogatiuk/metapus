// Package catalog_repo stores catalogs and integration accounts.
package catalog_repo

import (
	"context"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
	core "metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

type (
	// BaseCatalogRepo provides common CRUD operations for catalog entities.
	// Embed this in specific catalog repositories.
	//
	// In Database-per-Tenant architecture:
	// - TxManager is obtained from context per-request
	// - No tenant filtering in queries (isolation is physical)
	BaseCatalogRepo[T any] = core.BaseCatalogRepo[T]
	IntegrationAccountRepo = core.IntegrationAccountRepo
	IntegrationPoolRepo    = core.IntegrationPoolRepo
	// RateSourceKey is a rate source that still keeps its provider key on
	// the source itself instead of an integration pool.
	RateSourceKey = core.RateSourceKey
)

// AccountKeyNames reads the key names, never values, of accounts in the given order.
func AccountKeyNames(ctx context.Context, accountIDs []id.ID) ([]integrations.AccountKeys, error) {
	return core.AccountKeyNames(ctx, accountIDs)
}

// AccountKeysOf lists the key names of an account's credentials as they are
// about to be written.
func AccountKeysOf(account *integrations.Account) (integrations.AccountKeys, error) {
	return core.AccountKeysOf(account)
}

// AccountCredentials opens the stored credentials of an integration account;
// the platform key stays in the platform.
func AccountCredentials(ctx context.Context, accountID id.ID) (integrations.Credentials, error) {
	return core.AccountCredentials(ctx, accountID)
}

// Catalog mutations share a short tenant-local lock to validate cross-catalog profiles.
// Runtime selection uses its own pool-local row locks and never holds this lock over HTTP.
func LockIntegrationConfiguration(ctx context.Context) error {
	return core.LockIntegrationConfiguration(ctx)
}

// NewCatalogRepoForDefinition uses the same hierarchy and logical key as the
// service and HTTP registration, without a second independent boolean.
func NewCatalogRepoForDefinition[T any](def entity.Definition, selectCols []string, newFn func() T) *core.BaseCatalogRepo[T] {
	return core.NewCatalogRepoForDefinition[T](def, selectCols, newFn)
}

// NewIntegrationAccountRepo checks credential changes against the consumers
// of the account's pools.
func NewIntegrationAccountRepo(consumers ...integrations.PoolConsumer) *core.IntegrationAccountRepo {
	return core.NewIntegrationAccountRepo(consumers...)
}

// NewIntegrationPoolRepo keeps pools usable by their consumers: a used pool
// cannot be deleted or change provider, and its members must satisfy them.
func NewIntegrationPoolRepo(consumers ...integrations.PoolConsumer) *core.IntegrationPoolRepo {
	return core.NewIntegrationPoolRepo(consumers...)
}

// PoolAccountKeys reads the key names of the pool's accounts in selection order.
func PoolAccountKeys(ctx context.Context, poolID id.ID) ([]integrations.AccountKeys, error) {
	return core.PoolAccountKeys(ctx, poolID)
}

// PoolProvider returns the provider of a pool that is not marked for deletion.
func PoolProvider(ctx context.Context, poolID id.ID) (integrations.Provider, error) {
	return core.PoolProvider(ctx, poolID)
}

// AccountPoolIDs lists the pools that contain the account.
func AccountPoolIDs(ctx context.Context, accountID id.ID) ([]id.ID, error) {
	return core.AccountPoolIDs(ctx, accountID)
}

// PoolsSharingAccounts lists the pools that hold any account of the pool.
func PoolsSharingAccounts(ctx context.Context, poolID id.ID) ([]id.ID, error) {
	return core.PoolsSharingAccounts(ctx, poolID)
}

// AccountProvider returns the provider of an account that is not deleted.
func AccountProvider(ctx context.Context, accountID id.ID) (integrations.Provider, error) {
	return core.AccountProvider(ctx, accountID)
}

// ProviderAccountIDs lists the provider's accounts that are not deleted.
func ProviderAccountIDs(ctx context.Context, provider integrations.Provider) ([]id.ID, error) {
	return core.ProviderAccountIDs(ctx, provider)
}

// UsableAccountIDs lists the provider's active accounts that are not marked
// for deletion.
func UsableAccountIDs(ctx context.Context, provider integrations.Provider) ([]id.ID, error) {
	return core.UsableAccountIDs(ctx, provider)
}

// RequireUsableAccount fails with NotFound unless the account is usable and
// belongs to the provider.
func RequireUsableAccount(ctx context.Context, accountID id.ID, provider integrations.Provider) error {
	return core.RequireUsableAccount(ctx, accountID, provider)
}

// LockUsableAccount checks the account like RequireUsableAccount and locks it
// until the transaction ends.
func LockUsableAccount(ctx context.Context, accountID id.ID, provider integrations.Provider) error {
	return core.LockUsableAccount(ctx, accountID, provider)
}

// CreateKeylessAccount stores an active account without credentials for a
// provider the configuration serves itself, such as an emulator.
func CreateKeylessAccount(ctx context.Context, accountID id.ID, code, name string, provider integrations.Provider) error {
	return core.CreateKeylessAccount(ctx, accountID, code, name, provider)
}

// RateSourcesWithOwnKey lists, in code order, the rate sources of the type
// that are not marked for deletion and keep a key of their own.
func RateSourcesWithOwnKey(ctx context.Context, sourceType string) ([]RateSourceKey, error) {
	return core.RateSourcesWithOwnKey(ctx, sourceType)
}

// MoveRateSourceKeyToPool makes a CoinGecko rate source take its key from a
// CoinGecko Demo pool and drops the key it kept itself.
func MoveRateSourceKeyToPool(ctx context.Context, sourceID, poolID id.ID) error {
	return core.MoveRateSourceKeyToPool(ctx, sourceID, poolID)
}
