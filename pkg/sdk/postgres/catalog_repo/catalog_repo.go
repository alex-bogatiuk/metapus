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
)

// AccountKeyNames reads the key names, never values, of accounts in the given order.
func AccountKeyNames(ctx context.Context, accountIDs []id.ID) ([]integrations.AccountKeys, error) {
	return core.AccountKeyNames(ctx, accountIDs)
}

// AccountKeyNamesOf decrypts the credentials only to list their key names.
func AccountKeyNamesOf(accountID id.ID, name string, encrypted []byte) (integrations.AccountKeys, error) {
	return core.AccountKeyNamesOf(accountID, name, encrypted)
}

func IntegrationEncryptionKey() ([]byte, error) { return core.IntegrationEncryptionKey() }

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

// PoolMemberIDs lists pool accounts in selection order.
func PoolMemberIDs(ctx context.Context, poolID id.ID) ([]id.ID, error) {
	return core.PoolMemberIDs(ctx, poolID)
}
