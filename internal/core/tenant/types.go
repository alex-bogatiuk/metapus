// Package tenant provides multi-tenant database management for Database-per-Tenant architecture.
// Each tenant has their own isolated PostgreSQL database.
package tenant

import (
	"fmt"
	"strings"
	"time"
)

// Status represents tenant lifecycle state.
type Status string

const (
	// StatusActive - tenant can accept requests
	StatusActive Status = "active"

	// StatusSuspended - tenant is temporarily disabled (e.g., payment issues)
	StatusSuspended Status = "suspended"

	// StatusDeleted - tenant is marked for deletion
	StatusDeleted Status = "deleted"
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
	ID          string         `db:"id"`
	Slug        string         `db:"slug"`         // URL-safe identifier
	DisplayName string         `db:"display_name"` // Human-readable name
	DBName      string         `db:"db_name"`      // Database name
	DBHost      string         `db:"db_host"`      // Database host
	DBPort      int            `db:"db_port"`      // Database port
	Status      Status         `db:"status"`
	Plan        Plan           `db:"plan"`
	CreatedAt   time.Time      `db:"created_at"`
	UpdatedAt   time.Time      `db:"updated_at"`
	Settings    map[string]any `db:"settings"` // Additional settings (JSONB)
}

// IsActive returns true if tenant can accept requests.
func (t *Tenant) IsActive() bool {
	return t.Status == StatusActive
}

// DSN builds PostgreSQL connection string for this tenant's database.
func (t *Tenant) DSN(user, password string) string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		user, password, t.DBHost, t.DBPort, t.DBName,
	)
}

// DSNWithSSL builds PostgreSQL connection string with SSL enabled.
func (t *Tenant) DSNWithSSL(user, password, sslMode string) string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		user, password, t.DBHost, t.DBPort, t.DBName, sslMode,
	)
}

// CreateTenantInput contains data for creating a new tenant.
type CreateTenantInput struct {
	Slug        string
	DisplayName string
	Plan        Plan
	DBHost      string // Optional, defaults to localhost
	DBPort      int    // Optional, defaults to 5432
}

// Validate checks if input is valid.
func (i *CreateTenantInput) Validate() error {
	if i.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	i.Slug = strings.ToLower(i.Slug)
	if len(i.Slug) > 63 {
		return fmt.Errorf("slug must be 63 characters or less")
	}
	if i.DisplayName == "" {
		return fmt.Errorf("display_name is required")
	}
	return nil
}

// GenerateDBName creates database name from slug.
// Format: mt_<slug> (mt = multi-tenant)
func (i *CreateTenantInput) GenerateDBName() string {
	return "mt_" + i.Slug
}
