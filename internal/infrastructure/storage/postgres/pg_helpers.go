package postgres

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsUniqueViolation checks whether the error is a Postgres unique constraint violation (23505).
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key value")
}

// IsForeignKeyViolation checks whether the error is a Postgres FK constraint violation (23503).
func IsForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "23503") || strings.Contains(err.Error(), "violates foreign key constraint")
}

// ExtractForeignKeyField attempts to extract the column name from a PostgreSQL 
// foreign key violation error and convert it to camelCase for the frontend.
func ExtractForeignKeyField(err error, tableName string) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		constraint := pgErr.ConstraintName
		
		// Typically constraints are named: {tableName}_{columnName}_fkey
		// E.g., doc_goods_receipt_counterparty_id_fkey
		
		field := strings.TrimPrefix(constraint, tableName+"_")
		field = strings.TrimSuffix(field, "_fkey")
		
		// If the constraint naming doesn't follow the convention, we might get back the original string.
		// Let's convert snake_case to camelCase
		return snakeToCamel(field)
	}
	return ""
}

func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}
