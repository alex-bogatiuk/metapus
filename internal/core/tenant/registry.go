package tenant

import (
	"context"
	"fmt"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
)

// tenantColumns is the shared SELECT column list for all tenant queries.
// Update this constant when adding new columns to the tenants table.
const tenantColumns = `id, slug, display_name, db_name, db_host, db_port,
	       status, plan, schema_version, version_group, created_at, updated_at, settings`

// Registry provides access to tenant metadata stored in meta-database.
type Registry interface {
	// GetByID retrieves tenant by UUID string.
	GetByID(ctx context.Context, tenantID string) (*Tenant, error)

	// ListActive returns all active tenants.
	ListActive(ctx context.Context) ([]*Tenant, error)

	// ListAll returns all tenants.
	ListAll(ctx context.Context) ([]*Tenant, error)

	// ListByVersionGroup returns active tenants belonging to a specific version group.
	// Used by cloud-mode servers and workers to filter "their" tenants.
	ListByVersionGroup(ctx context.Context, group string) ([]*Tenant, error)

	// Create inserts a new tenant row and populates t.ID.
	Create(ctx context.Context, t *Tenant) error

	// UpdateStatusByID updates tenant status by UUID string.
	UpdateStatusByID(ctx context.Context, tenantID string, status Status) error

	// UpdateSchemaVersion sets the schema_version field after a successful migration.
	UpdateSchemaVersion(ctx context.Context, tenantID string, version int) error

	// UpdateVersionGroup assigns a tenant to a version group (cloud mode).
	UpdateVersionGroup(ctx context.Context, tenantID string, group string) error
}

// PostgresRegistry implements Registry using meta-database PostgreSQL.
type PostgresRegistry struct {
	pool *pgxpool.Pool
}

func NewPostgresRegistry(pool *pgxpool.Pool) *PostgresRegistry {
	return &PostgresRegistry{pool: pool}
}

func (r *PostgresRegistry) GetByID(ctx context.Context, tenantID string) (*Tenant, error) {
	var t Tenant
	err := pgxscan.Get(ctx, r.pool, &t, `
		SELECT `+tenantColumns+`
		FROM tenants
		WHERE id = $1
	`, tenantID)
	if err != nil {
		if pgxscan.NotFound(err) {
			return nil, ErrTenantNotFound
		}
		return nil, fmt.Errorf("get tenant by id: %w", err)
	}
	return &t, nil
}

func (r *PostgresRegistry) ListActive(ctx context.Context) ([]*Tenant, error) {
	var tenants []*Tenant
	err := pgxscan.Select(ctx, r.pool, &tenants, `
		SELECT `+tenantColumns+`
		FROM tenants
		WHERE status = $1
		ORDER BY slug
	`, StatusActive)
	if err != nil {
		return nil, fmt.Errorf("list active tenants: %w", err)
	}
	return tenants, nil
}

func (r *PostgresRegistry) ListAll(ctx context.Context) ([]*Tenant, error) {
	var tenants []*Tenant
	err := pgxscan.Select(ctx, r.pool, &tenants, `
		SELECT `+tenantColumns+`
		FROM tenants
		ORDER BY slug
	`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	return tenants, nil
}

func (r *PostgresRegistry) ListByVersionGroup(ctx context.Context, group string) ([]*Tenant, error) {
	var tenants []*Tenant
	err := pgxscan.Select(ctx, r.pool, &tenants, `
		SELECT `+tenantColumns+`
		FROM tenants
		WHERE status = $1 AND version_group = $2
		ORDER BY slug
	`, StatusActive, group)
	if err != nil {
		return nil, fmt.Errorf("list tenants by version group: %w", err)
	}
	return tenants, nil
}

func (r *PostgresRegistry) Create(ctx context.Context, t *Tenant) error {
	if t == nil {
		return fmt.Errorf("tenant is nil")
	}
	if t.Status == "" {
		t.Status = StatusActive
	}
	if t.Plan == "" {
		t.Plan = PlanStandard
	}

	// settings is JSONB with default '{}', but we still pass it explicitly for clarity.
	if t.Settings == nil {
		t.Settings = map[string]any{}
	}

	// Return generated UUID.
	err := r.pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, display_name, db_name, db_host, db_port, status, plan, settings)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, t.Slug, t.DisplayName, t.DBName, t.DBHost, t.DBPort, t.Status, t.Plan, t.Settings).Scan(&t.ID)
	if err != nil {
		return fmt.Errorf("create tenant: %w", err)
	}
	return nil
}

func (r *PostgresRegistry) UpdateStatusByID(ctx context.Context, tenantID string, status Status) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE tenants
		SET status = $2
		WHERE id = $1
	`, tenantID, status)
	if err != nil {
		return fmt.Errorf("update tenant status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTenantNotFound
	}
	return nil
}

func (r *PostgresRegistry) UpdateSchemaVersion(ctx context.Context, tenantID string, version int) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE tenants
		SET schema_version = $2
		WHERE id = $1
	`, tenantID, version)
	if err != nil {
		return fmt.Errorf("update schema version: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTenantNotFound
	}
	return nil
}

func (r *PostgresRegistry) UpdateVersionGroup(ctx context.Context, tenantID string, group string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE tenants
		SET version_group = $2
		WHERE id = $1
	`, tenantID, group)
	if err != nil {
		return fmt.Errorf("update version group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTenantNotFound
	}
	return nil
}

var _ Registry = (*PostgresRegistry)(nil)
