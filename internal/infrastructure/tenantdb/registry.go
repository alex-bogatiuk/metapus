package tenantdb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	tenantcore "github.com/alex-bogatiuk/metapus/internal/core/tenant"
)

// tenantColumns is the shared SELECT column list for all tenant queries.
// Update this constant when adding new columns to the tenants table.
const tenantColumns = `id, slug, display_name, db_name, db_host, db_port,
	       status, plan, schema_version, configuration,
	       configuration_schema_version, addon, addon_schema_version, provisioning_stage,
	       provisioning_error, provisioned_at, created_at, updated_at, settings`

// PostgresRegistry implements tenantcore.Registry using meta-database PostgreSQL.
type PostgresRegistry struct {
	pool *pgxpool.Pool
}

func NewPostgresRegistry(pool *pgxpool.Pool) *PostgresRegistry {
	return &PostgresRegistry{pool: pool}
}

func (r *PostgresRegistry) GetByID(ctx context.Context, tenantID string) (*tenantcore.Tenant, error) {
	var t tenantcore.Tenant
	err := pgxscan.Get(ctx, r.pool, &t, `
		SELECT `+tenantColumns+`
		FROM tenants
		WHERE id = $1
	`, tenantID)
	if err != nil {
		if pgxscan.NotFound(err) {
			return nil, tenantcore.ErrTenantNotFound
		}
		return nil, fmt.Errorf("get tenant by id: %w", err)
	}
	return &t, nil
}

func (r *PostgresRegistry) GetBySlug(ctx context.Context, slug string) (*tenantcore.Tenant, error) {
	var t tenantcore.Tenant
	err := pgxscan.Get(ctx, r.pool, &t, `
		SELECT `+tenantColumns+`
		FROM tenants
		WHERE lower(slug) = lower($1)
	`, slug)
	if err != nil {
		if pgxscan.NotFound(err) {
			return nil, tenantcore.ErrTenantNotFound
		}
		return nil, fmt.Errorf("get tenant by slug: %w", err)
	}
	return &t, nil
}

func (r *PostgresRegistry) ListActive(ctx context.Context) ([]*tenantcore.Tenant, error) {
	var tenants []*tenantcore.Tenant
	err := pgxscan.Select(ctx, r.pool, &tenants, `
		SELECT `+tenantColumns+`
		FROM tenants
		WHERE status = $1
		ORDER BY slug
	`, tenantcore.StatusActive)
	if err != nil {
		return nil, fmt.Errorf("list active tenants: %w", err)
	}
	return tenants, nil
}

func (r *PostgresRegistry) ListAll(ctx context.Context) ([]*tenantcore.Tenant, error) {
	var tenants []*tenantcore.Tenant
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

func (r *PostgresRegistry) Create(ctx context.Context, t *tenantcore.Tenant) error {
	if t == nil {
		return fmt.Errorf("tenant is nil")
	}
	if t.Status == "" {
		t.Status = tenantcore.StatusProvisioning
	}
	if t.Plan == "" {
		t.Plan = tenantcore.PlanStandard
	}
	input := tenantcore.CreateTenantInput{
		Configuration: t.Configuration,
		AddOn:         t.AddOn,
		Slug:          t.Slug,
		DisplayName:   t.DisplayName,
		Plan:          t.Plan,
		DBHost:        t.DBHost,
		DBPort:        t.DBPort,
	}
	if err := input.Validate(); err != nil {
		return fmt.Errorf("validate tenant: %w", err)
	}
	t.Slug = input.Slug
	t.DisplayName = input.DisplayName
	t.Plan = input.Plan
	t.DBHost = input.DBHost
	t.DBPort = input.DBPort
	if t.DBName == "" || len(t.DBName) > 63 || strings.ContainsRune(t.DBName, 0) {
		return fmt.Errorf("validate tenant: database name must be 1-63 bytes without null characters")
	}

	// settings is JSONB with default '{}', but we still pass it explicitly for clarity.
	if t.Settings == nil {
		t.Settings = map[string]any{}
	}
	if t.ID == "" {
		t.ID = id.New().String()
	}
	if t.Status == tenantcore.StatusProvisioning && t.ProvisioningStage == "" {
		t.ProvisioningStage = tenantcore.ProvisioningStageReserved
	}
	if t.Status == tenantcore.StatusActive && t.ProvisionedAt == nil {
		now := time.Now().UTC()
		t.ProvisionedAt = &now
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create tenant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO tenants (
			id, slug, display_name, db_name, db_host, db_port, status, plan, settings,
			provisioning_stage, provisioning_error, provisioned_at, configuration, addon
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, t.ID, t.Slug, t.DisplayName, t.DBName, t.DBHost, t.DBPort, t.Status, t.Plan,
		t.Settings, t.ProvisioningStage, t.ProvisioningError, t.ProvisionedAt, t.Configuration, t.AddOn)
	if err != nil {
		return fmt.Errorf("create tenant: %w", err)
	}

	auditAction := "tenant_registered"
	if t.Status == tenantcore.StatusProvisioning {
		auditAction = "provisioning_started"
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO tenant_audit (tenant_id, action, details)
		VALUES ($1, $2, jsonb_build_object('stage', $3::text))
	`, t.ID, auditAction, t.ProvisioningStage)
	if err != nil {
		return fmt.Errorf("audit tenant creation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tenant creation: %w", err)
	}
	return nil
}

func (r *PostgresRegistry) WithProvisioningLock(
	ctx context.Context,
	slug string,
	fn func(context.Context) error,
) (err error) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire provisioning lock connection: %w", err)
	}
	defer conn.Release()

	lockKey := "metapus:tenant-provision:" + slug
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return fmt.Errorf("lock tenant provisioning: %w", err)
	}
	defer func() {
		_, unlockErr := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, lockKey)
		if unlockErr != nil {
			err = errors.Join(err, fmt.Errorf("unlock tenant provisioning: %w", unlockErr))
		}
	}()

	return fn(ctx)
}

func (r *PostgresRegistry) SetProvisioningStage(
	ctx context.Context,
	tenantID string,
	stage tenantcore.ProvisioningStage,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE tenants
		SET status = $2, provisioning_stage = $3, provisioning_error = ''
		WHERE id = $1 AND status IN ($2, $4, $5)
	`, tenantID, tenantcore.StatusProvisioning, stage, tenantcore.StatusProvisionFailed, tenantcore.StatusActive)
	if err != nil {
		return fmt.Errorf("set tenant provisioning stage: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return tenantcore.ErrTenantNotFound
	}
	return nil
}

func (r *PostgresRegistry) MarkProvisioningFailed(
	ctx context.Context,
	tenantID string,
	stage tenantcore.ProvisioningStage,
	message string,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tenant provisioning failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE tenants
		SET status = $2, provisioning_stage = $3, provisioning_error = $4
		WHERE id = $1 AND status IN ($5, $2)
	`, tenantID, tenantcore.StatusProvisionFailed, stage, message, tenantcore.StatusProvisioning)
	if err != nil {
		return fmt.Errorf("mark tenant provisioning failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return tenantcore.ErrTenantNotFound
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO tenant_audit (tenant_id, action, details)
		VALUES ($1, 'provisioning_failed', jsonb_build_object('stage', $2::text, 'error', $3::text))
	`, tenantID, stage, message)
	if err != nil {
		return fmt.Errorf("audit tenant provisioning failure: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tenant provisioning failure: %w", err)
	}
	return nil
}

func (r *PostgresRegistry) MarkProvisioned(ctx context.Context, tenantID string, schema tenantcore.Schema) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tenant provisioning completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE tenants
		SET status = $2,
		    schema_version = $3,
		    provisioning_stage = $4,
		    provisioning_error = '',
		    provisioned_at = NOW(),
		    configuration = $6,
		    configuration_schema_version = $7,
		    addon = $8,
		    addon_schema_version = $9
		WHERE id = $1 AND status = $5 AND configuration IN ('', $6) AND addon IN ('', $8)
	`, tenantID, tenantcore.StatusActive, schema.Version, tenantcore.ProvisioningStageComplete, tenantcore.StatusProvisioning,
		schema.Configuration, schema.ConfigurationVersion, schema.AddOn, schema.AddOnVersion)
	if err != nil {
		return fmt.Errorf("mark tenant provisioned: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return tenantcore.ErrTenantNotFound
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO tenant_audit (tenant_id, action, details)
		VALUES ($1, 'provisioned', jsonb_build_object(
			'schema_version', $2::int,
			'configuration', $3::text,
			'configuration_schema_version', $4::int,
			'addon', $5::text,
			'addon_schema_version', $6::int))
	`, tenantID, schema.Version, schema.Configuration, schema.ConfigurationVersion, schema.AddOn, schema.AddOnVersion)
	if err != nil {
		return fmt.Errorf("audit tenant provisioning completion: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tenant provisioning completion: %w", err)
	}
	return nil
}

func (r *PostgresRegistry) UpdateStatusByID(ctx context.Context, tenantID string, status tenantcore.Status) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE tenants
		SET status = $2
		WHERE id = $1
	`, tenantID, status)
	if err != nil {
		return fmt.Errorf("update tenant status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return tenantcore.ErrTenantNotFound
	}
	return nil
}

func (r *PostgresRegistry) UpdateSchema(ctx context.Context, tenantID string, schema tenantcore.Schema) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE tenants
		SET schema_version = $2,
		    configuration = $3,
		    configuration_schema_version = $4,
		    addon = $5,
		    addon_schema_version = $6
		WHERE id = $1 AND configuration IN ('', $3) AND addon IN ('', $5)
	`, tenantID, schema.Version, schema.Configuration, schema.ConfigurationVersion, schema.AddOn, schema.AddOnVersion)
	if err != nil {
		return fmt.Errorf("update schema version: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	t, err := r.GetByID(ctx, tenantID)
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: tenant %s is %q, not %q", tenantcore.ErrConfigurationMismatch, tenantID, stackName(t.Configuration, t.AddOn), stackName(schema.Configuration, schema.AddOn))
}

// stackName names a tenant's application: its configuration and the add-on
// built on it.
func stackName(configuration, addOn string) string {
	if addOn == "" {
		return configuration
	}
	return configuration + "+" + addOn
}

var _ tenantcore.Registry = (*PostgresRegistry)(nil)
