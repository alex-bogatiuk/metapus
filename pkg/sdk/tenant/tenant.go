// Package tenant is the tenant of a request and the transaction manager
// of its database.
package tenant

import (
	"context"
	core "github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/core/tx"
)

type (
	// ContextTxManager resolves the tenant transaction manager for each command.
	// It stores no tenant or connection between requests.
	ContextTxManager = core.ContextTxManager
	// ProvisioningStage identifies the last provisioning step attempted for a tenant.
	ProvisioningStage = core.ProvisioningStage
	// Registry provides access to tenant metadata stored in meta-database.
	Registry = core.Registry
	// Schema is the migration state of a tenant database: the configuration whose
	// migrations it holds and the highest applied version of the platform's
	// migrations and of the configuration's. Each has a goose history of its own.
	Schema = core.Schema
	// Status represents tenant lifecycle state.
	Status = core.Status
	// Tenant represents a tenant record from meta-database.
	Tenant = core.Tenant
)

var (
	// ErrTenantNotActive is returned when tenant exists but is not active.
	ErrTenantNotActive = core.ErrTenantNotActive
	// ErrTenantNotFound is returned when tenant does not exist in meta-database.
	ErrTenantNotFound = core.ErrTenantNotFound
)

// GetTenant retrieves tenant from context.
func GetTenant(ctx context.Context) *core.Tenant { return core.GetTenant(ctx) }

// GetTenantID returns tenant ID or empty string.
// Alias for backwards compatibility with existing code.
func GetTenantID(ctx context.Context) string { return core.GetTenantID(ctx) }

// GetTxManager retrieves TxManager from context.
func GetTxManager(ctx context.Context) (tx.Manager, error) { return core.GetTxManager(ctx) }

// MustGetTxManager retrieves TxManager or panics.
// Use in places where missing TxManager is a programming error.
func MustGetTxManager(ctx context.Context) tx.Manager { return core.MustGetTxManager(ctx) }

// WithTenant stores tenant info in context.
func WithTenant(ctx context.Context, t *core.Tenant) context.Context { return core.WithTenant(ctx, t) }

// WithTxManager stores TxManager in context.
func WithTxManager(ctx context.Context, txm tx.Manager) context.Context {
	return core.WithTxManager(ctx, txm)
}
