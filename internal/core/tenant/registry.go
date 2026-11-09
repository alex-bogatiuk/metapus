package tenant

import "context"

// Registry provides access to tenant metadata stored in meta-database.
type Registry interface {
	// GetByID retrieves tenant by UUID string.
	GetByID(ctx context.Context, tenantID string) (*Tenant, error)

	// GetBySlug retrieves tenant by its case-insensitive public slug.
	GetBySlug(ctx context.Context, slug string) (*Tenant, error)

	// ListActive returns all active tenants.
	ListActive(ctx context.Context) ([]*Tenant, error)

	// ListAll returns all tenants.
	ListAll(ctx context.Context) ([]*Tenant, error)

	// Create inserts a new tenant row and populates t.ID.
	Create(ctx context.Context, t *Tenant) error

	// WithProvisioningLock serializes provisioning attempts for the same slug.
	WithProvisioningLock(ctx context.Context, slug string, fn func(context.Context) error) error

	// SetProvisioningStage records progress while keeping the tenant unavailable.
	SetProvisioningStage(ctx context.Context, tenantID string, stage ProvisioningStage) error

	// MarkProvisioningFailed records a resumable provisioning failure.
	MarkProvisioningFailed(ctx context.Context, tenantID string, stage ProvisioningStage, message string) error

	// MarkProvisioned atomically publishes the tenant after migrations succeed.
	MarkProvisioned(ctx context.Context, tenantID string, schemaVersion int) error

	// UpdateStatusByID updates tenant status by UUID string.
	UpdateStatusByID(ctx context.Context, tenantID string, status Status) error

	// UpdateSchemaVersion sets the schema_version field after a successful migration.
	UpdateSchemaVersion(ctx context.Context, tenantID string, version int) error
}
