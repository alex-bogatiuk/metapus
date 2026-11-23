package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
)

const duplicateDatabaseSQLState = "42P04"

// TenantDatabaseAdmin creates tenant databases through a PostgreSQL admin connection.
type TenantDatabaseAdmin struct {
	pool *pgxpool.Pool
}

func NewTenantDatabaseAdmin(pool *pgxpool.Pool) *TenantDatabaseAdmin {
	return &TenantDatabaseAdmin{pool: pool}
}

// EnsureDatabase creates a database once. Identifiers are always PostgreSQL-quoted.
func (a *TenantDatabaseAdmin) EnsureDatabase(ctx context.Context, databaseName string) (bool, error) {
	if a == nil || a.pool == nil {
		return false, fmt.Errorf("postgres admin pool is required")
	}
	if err := validateDatabaseIdentifier(databaseName); err != nil {
		return false, err
	}

	exists, err := a.DatabaseExists(ctx, databaseName)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}

	statement := "CREATE DATABASE " + pgx.Identifier{databaseName}.Sanitize()
	if _, err := a.pool.Exec(ctx, statement); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == duplicateDatabaseSQLState {
			return false, nil
		}
		return false, fmt.Errorf("create database: %w", err)
	}
	return true, nil
}

func (a *TenantDatabaseAdmin) DatabaseExists(ctx context.Context, databaseName string) (bool, error) {
	if a == nil || a.pool == nil {
		return false, fmt.Errorf("postgres admin pool is required")
	}
	if err := validateDatabaseIdentifier(databaseName); err != nil {
		return false, err
	}

	var exists bool
	if err := a.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`, databaseName).Scan(&exists); err != nil {
		return false, fmt.Errorf("check database existence: %w", err)
	}
	return exists, nil
}

func validateDatabaseIdentifier(name string) error {
	if name == "" {
		return fmt.Errorf("database name is required")
	}
	if len(name) > 63 {
		return fmt.Errorf("database name must be 63 bytes or less")
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("database name contains a null byte")
	}
	return nil
}

var _ tenant.DatabaseAdmin = (*TenantDatabaseAdmin)(nil)
