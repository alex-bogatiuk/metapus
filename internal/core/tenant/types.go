// Package tenant provides multi-tenant database management for Database-per-Tenant architecture.
// Each tenant has their own isolated PostgreSQL database.
package tenant

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Status represents tenant lifecycle state.
type Status string

const (
	// StatusProvisioning means the physical database is being created and migrated.
	StatusProvisioning Status = "provisioning"

	// StatusProvisionFailed means provisioning can be resumed after its cause is fixed.
	StatusProvisionFailed Status = "provision_failed"

	// StatusActive - tenant can accept requests
	StatusActive Status = "active"

	// StatusSuspended - tenant is temporarily disabled (e.g., payment issues)
	StatusSuspended Status = "suspended"

	// StatusDeleted - tenant is marked for deletion
	StatusDeleted Status = "deleted"
)

// ProvisioningStage identifies the last provisioning step attempted for a tenant.
type ProvisioningStage string

const (
	ProvisioningStageReserved  ProvisioningStage = "reserved"
	ProvisioningStageDatabase  ProvisioningStage = "database"
	ProvisioningStageMigration ProvisioningStage = "migration"
	ProvisioningStageComplete  ProvisioningStage = "complete"
)

// Plan represents tenant subscription plan.
type Plan string

const (
	PlanStandard   Plan = "standard"
	PlanPremium    Plan = "premium"
	PlanEnterprise Plan = "enterprise"
)

// Tenant represents a tenant record from meta-database.
type Tenant struct {
	ID            string `db:"id"`
	Slug          string `db:"slug"`         // URL-safe identifier
	DisplayName   string `db:"display_name"` // Human-readable name
	DBName        string `db:"db_name"`      // Database name
	DBHost        string `db:"db_host"`      // Database host
	DBPort        int    `db:"db_port"`      // Database port
	Status        Status `db:"status"`
	Plan          Plan   `db:"plan"`
	SchemaVersion int    `db:"schema_version"` // Highest applied platform migration
	// Configuration names the configuration whose migrations the tenant
	// database holds; empty until a binary that records it migrates the
	// tenant.
	Configuration              string            `db:"configuration"`
	ConfigurationSchemaVersion int               `db:"configuration_schema_version"` // Highest applied configuration migration
	ProvisioningStage          ProvisioningStage `db:"provisioning_stage"`
	ProvisioningError          string            `db:"provisioning_error"`
	ProvisionedAt              *time.Time        `db:"provisioned_at"`
	CreatedAt                  time.Time         `db:"created_at"`
	UpdatedAt                  time.Time         `db:"updated_at"`
	Settings                   map[string]any    `db:"settings"` // Additional settings (JSONB)
}

// Schema returns the tenant database's recorded migration state.
func (t *Tenant) Schema() Schema {
	return Schema{Configuration: t.Configuration, Version: t.SchemaVersion, ConfigurationVersion: t.ConfigurationSchemaVersion}
}

// Schema is the migration state of a tenant database: the configuration whose
// migrations it holds and the highest applied version of the platform's
// migrations and of the configuration's. Each has a goose history of its own.
type Schema struct {
	Configuration        string
	Version              int
	ConfigurationVersion int
}

// Matches reports whether a tenant database in state s is at expected.
func (s Schema) Matches(expected Schema) bool { return s == expected }

// IsActive returns true if tenant can accept requests.
func (t *Tenant) IsActive() bool {
	return t.Status == StatusActive
}

// CanCreatePool returns true if a connection pool can be created for this tenant.
// Business traffic is served only for active tenants.
func (t *Tenant) CanCreatePool() bool {
	return t.Status == StatusActive
}

// CreateTenantInput contains data for creating a new tenant.
type CreateTenantInput struct {
	// Configuration names the configuration whose migrations the tenant
	// database gets; a tenant keeps it for life.
	Configuration string
	Slug          string
	DisplayName   string
	Plan          Plan
	DBHost        string // Optional, defaults to localhost
	DBPort        int    // Optional, defaults to 5432
}

// configurationName matches configuration.ValidName.
var configurationName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

var tenantSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// Validate checks if input is valid.
func (i *CreateTenantInput) Validate() error {
	if !configurationName.MatchString(i.Configuration) {
		return fmt.Errorf("configuration must be a lowercase identifier of at most 40 characters")
	}
	i.Slug = strings.ToLower(strings.TrimSpace(i.Slug))
	if i.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if !tenantSlugPattern.MatchString(i.Slug) {
		return fmt.Errorf("slug must be 1-63 lowercase ASCII letters, digits, or hyphens and cannot start or end with a hyphen")
	}
	i.DisplayName = strings.TrimSpace(i.DisplayName)
	if i.DisplayName == "" {
		return fmt.Errorf("display_name is required")
	}
	if utf8.RuneCountInString(i.DisplayName) > 255 {
		return fmt.Errorf("display_name must be 255 characters or less")
	}
	if i.Plan == "" {
		i.Plan = PlanStandard
	}
	switch i.Plan {
	case PlanStandard, PlanPremium, PlanEnterprise:
	default:
		return fmt.Errorf("plan must be standard, premium, or enterprise")
	}
	if i.DBHost == "" {
		i.DBHost = "localhost"
	}
	if i.DBPort == 0 {
		i.DBPort = 5432
	}
	if i.DBPort < 1 || i.DBPort > 65535 {
		return fmt.Errorf("db_port must be between 1 and 65535")
	}
	return nil
}

// GenerateDatabaseName creates a stable physical name independent from the public slug.
func GenerateDatabaseName(tenantID string) (string, error) {
	id, err := uuid.Parse(tenantID)
	if err != nil {
		return "", fmt.Errorf("parse tenant id: %w", err)
	}
	return "mt_" + strings.ReplaceAll(id.String(), "-", ""), nil
}
