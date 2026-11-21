package tenant

import (
	"context"
	"errors"
	"fmt"

	"metapus/internal/core/id"
)

// DatabaseAdmin owns cluster-level tenant database creation.
type DatabaseAdmin interface {
	DatabaseExists(ctx context.Context, databaseName string) (bool, error)
	EnsureDatabase(ctx context.Context, databaseName string) (created bool, err error)
}

// SchemaMigrator applies the platform's and t.Configuration's migrations and
// returns the resulting schema.
type SchemaMigrator interface {
	Migrate(ctx context.Context, t *Tenant) (Schema, error)
}

// ProvisionResult describes an idempotent tenant provisioning attempt.
type ProvisionResult struct {
	Tenant             *Tenant
	DatabaseCreated    bool
	AlreadyProvisioned bool
}

// Provisioner coordinates the non-transactional CREATE DATABASE workflow as a resumable saga.
type Provisioner struct {
	registry Registry
	dbAdmin  DatabaseAdmin
	migrator SchemaMigrator
}

func NewProvisioner(registry Registry, dbAdmin DatabaseAdmin, migrator SchemaMigrator) *Provisioner {
	return &Provisioner{registry: registry, dbAdmin: dbAdmin, migrator: migrator}
}

// Provision reserves metadata, creates the physical database, migrates it, and only then publishes it.
func (p *Provisioner) Provision(ctx context.Context, input CreateTenantInput) (*ProvisionResult, error) {
	if p == nil || p.registry == nil || p.dbAdmin == nil || p.migrator == nil {
		return nil, fmt.Errorf("tenant provisioner dependencies are required")
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}

	result := &ProvisionResult{}
	err := p.registry.WithProvisioningLock(ctx, input.Slug, func(ctx context.Context) error {
		t, err := p.registry.GetBySlug(ctx, input.Slug)
		switch {
		case err == nil:
			result.Tenant = t
			if t.Configuration != "" && t.Configuration != input.Configuration {
				return fmt.Errorf("%w: tenant %s is %q, not %q", ErrConfigurationMismatch, t.ID, t.Configuration, input.Configuration)
			}
			if t.Status == StatusActive {
				exists, existsErr := p.dbAdmin.DatabaseExists(ctx, t.DBName)
				if existsErr != nil {
					return fmt.Errorf("verify active tenant database: %w", existsErr)
				}
				if exists {
					result.AlreadyProvisioned = true
					return nil
				}
			}
			if t.Status != StatusProvisioning && t.Status != StatusProvisionFailed && t.Status != StatusActive {
				return fmt.Errorf("%w: tenant %s has status %s", ErrTenantProvisioningState, t.ID, t.Status)
			}
			t.Configuration = input.Configuration
		case errors.Is(err, ErrTenantNotFound):
			tenantID := id.New().String()
			dbName, nameErr := GenerateDatabaseName(tenantID)
			if nameErr != nil {
				return nameErr
			}
			t = &Tenant{
				ID:                tenantID,
				Slug:              input.Slug,
				DisplayName:       input.DisplayName,
				DBName:            dbName,
				DBHost:            input.DBHost,
				DBPort:            input.DBPort,
				Status:            StatusProvisioning,
				Plan:              input.Plan,
				Configuration:     input.Configuration,
				ProvisioningStage: ProvisioningStageReserved,
				Settings:          map[string]any{},
			}
			if err := p.registry.Create(ctx, t); err != nil {
				return fmt.Errorf("reserve tenant: %w", err)
			}
			result.Tenant = t
		default:
			return err
		}

		if err := p.registry.SetProvisioningStage(ctx, t.ID, ProvisioningStageDatabase); err != nil {
			return fmt.Errorf("start database provisioning: %w", err)
		}
		created, err := p.dbAdmin.EnsureDatabase(ctx, t.DBName)
		if err != nil {
			return p.fail(ctx, t.ID, ProvisioningStageDatabase, fmt.Errorf("create tenant database: %w", err))
		}
		result.DatabaseCreated = created

		if err := p.registry.SetProvisioningStage(ctx, t.ID, ProvisioningStageMigration); err != nil {
			return fmt.Errorf("start tenant migrations: %w", err)
		}
		schema, err := p.migrator.Migrate(ctx, t)
		if err != nil {
			return p.fail(ctx, t.ID, ProvisioningStageMigration, fmt.Errorf("migrate tenant database: %w", err))
		}
		if schema.Version < 0 || schema.ConfigurationVersion < 0 || schema.Configuration != input.Configuration {
			return p.fail(ctx, t.ID, ProvisioningStageMigration, fmt.Errorf("migrator returned invalid schema %+v", schema))
		}
		if err := p.registry.MarkProvisioned(ctx, t.ID, schema); err != nil {
			return fmt.Errorf("publish provisioned tenant: %w", err)
		}

		t.Status = StatusActive
		t.SchemaVersion = schema.Version
		t.ConfigurationSchemaVersion = schema.ConfigurationVersion
		t.ProvisioningStage = ProvisioningStageComplete
		t.ProvisioningError = ""
		return nil
	})
	if err != nil {
		return result, err
	}
	return result, nil
}

func (p *Provisioner) fail(
	ctx context.Context,
	tenantID string,
	stage ProvisioningStage,
	cause error,
) error {
	markErr := p.registry.MarkProvisioningFailed(context.WithoutCancel(ctx), tenantID, stage, cause.Error())
	if markErr != nil {
		return errors.Join(cause, fmt.Errorf("record provisioning failure: %w", markErr))
	}
	return cause
}
