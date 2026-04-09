package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"metapus/internal/core/apperror"
	"metapus/internal/core/tenant"
)

// CustomFieldRecord represents a row in sys_custom_field_schemas.
type CustomFieldRecord struct {
	ID              string         `json:"id"`
	EntityType      string         `json:"entityType"`
	FieldName       string         `json:"fieldName"`
	FieldType       string         `json:"fieldType"`
	DisplayName     string         `json:"displayName"`
	Description     string         `json:"description"`
	IsRequired      bool           `json:"isRequired"`
	IsIndexed       bool           `json:"isIndexed"`
	DefaultValue    any            `json:"defaultValue,omitempty"`
	ValidationRules map[string]any `json:"validationRules,omitempty"`
	ReferenceType   string         `json:"referenceType,omitempty"`
	EnumValues      []string       `json:"enumValues,omitempty"`
	SortOrder       int            `json:"sortOrder"`
	IsActive        bool           `json:"isActive"`
	CreatedAt       string         `json:"createdAt"`
	UpdatedAt       string         `json:"updatedAt"`
}

// CustomFieldUpdate contains optional update fields.
type CustomFieldUpdate struct {
	DisplayName     *string        `json:"displayName"`
	Description     *string        `json:"description"`
	IsRequired      *bool          `json:"isRequired"`
	IsIndexed       *bool          `json:"isIndexed"`
	DefaultValue    any            `json:"defaultValue"`
	ValidationRules map[string]any `json:"validationRules"`
	EnumValues      []string       `json:"enumValues"`
	SortOrder       *int           `json:"sortOrder"`
	IsActive        *bool          `json:"isActive"`
}

// CustomFieldRepo provides CRUD for sys_custom_field_schemas.
type CustomFieldRepo struct{}

// NewCustomFieldRepo creates a new repo instance.
func NewCustomFieldRepo() *CustomFieldRepo {
	return &CustomFieldRepo{}
}

// List returns custom fields, optionally filtered by entity type.
func (r *CustomFieldRepo) List(ctx context.Context, entityType string) ([]CustomFieldRecord, error) {
	pool := tenant.MustGetPool(ctx)

	query := `
		SELECT id, entity_type, field_name, field_type, display_name,
		       description, is_required, is_indexed, default_value,
		       validation_rules, reference_type, enum_values, sort_order,
		       is_active, created_at::text, updated_at::text
		FROM sys_custom_field_schemas
	`
	args := make([]any, 0)

	if entityType != "" {
		query += " WHERE entity_type = $1"
		args = append(args, entityType)
	}
	query += " ORDER BY entity_type, sort_order, field_name"

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query custom fields: %w", err)
	}
	defer rows.Close()

	result := make([]CustomFieldRecord, 0, 16)
	for rows.Next() {
		f, err := scanCustomField(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, nil
}

// GetByID returns a single custom field.
func (r *CustomFieldRepo) GetByID(ctx context.Context, id string) (*CustomFieldRecord, error) {
	pool := tenant.MustGetPool(ctx)

	row := pool.QueryRow(ctx, `
		SELECT id, entity_type, field_name, field_type, display_name,
		       description, is_required, is_indexed, default_value,
		       validation_rules, reference_type, enum_values, sort_order,
		       is_active, created_at::text, updated_at::text
		FROM sys_custom_field_schemas
		WHERE id = $1
	`, id)

	var f CustomFieldRecord
	var defaultVal, validationRules []byte
	var enumVals []string

	err := row.Scan(
		&f.ID, &f.EntityType, &f.FieldName, &f.FieldType, &f.DisplayName,
		&f.Description, &f.IsRequired, &f.IsIndexed, &defaultVal,
		&validationRules, &f.ReferenceType, &enumVals, &f.SortOrder,
		&f.IsActive, &f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		return nil, apperror.NewNotFound("custom_field", id)
	}

	f.EnumValues = enumVals
	unmarshalJSONB(defaultVal, &f.DefaultValue)
	unmarshalJSONBMap(validationRules, &f.ValidationRules)

	return &f, nil
}

// Create inserts a new custom field schema.
func (r *CustomFieldRepo) Create(ctx context.Context, f *CustomFieldRecord) error {
	pool := tenant.MustGetPool(ctx)

	defaultVal, _ := json.Marshal(f.DefaultValue)
	validationRules, _ := json.Marshal(f.ValidationRules)

	err := pool.QueryRow(ctx, `
		INSERT INTO sys_custom_field_schemas
			(entity_type, field_name, field_type, display_name, description,
			 is_required, is_indexed, default_value, validation_rules,
			 reference_type, enum_values, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id
	`,
		f.EntityType, f.FieldName, f.FieldType, f.DisplayName, f.Description,
		f.IsRequired, f.IsIndexed, defaultVal, validationRules,
		f.ReferenceType, f.EnumValues, f.SortOrder,
	).Scan(&f.ID)

	if err != nil {
		if isDuplicateKey(err) {
			return apperror.NewConflict(
				fmt.Sprintf("custom field %s.%s already exists", f.EntityType, f.FieldName))
		}
		return fmt.Errorf("create custom field: %w", err)
	}
	return nil
}

// Update modifies an existing custom field schema.
func (r *CustomFieldRepo) Update(ctx context.Context, id string, upd *CustomFieldUpdate) error {
	pool := tenant.MustGetPool(ctx)

	// Build dynamic SET clause
	setClauses := make([]string, 0, 9)
	args := make([]any, 0, 10)
	argIdx := 1

	addClause := func(col string, val any) {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, argIdx))
		args = append(args, val)
		argIdx++
	}

	if upd.DisplayName != nil {
		addClause("display_name", *upd.DisplayName)
	}
	if upd.Description != nil {
		addClause("description", *upd.Description)
	}
	if upd.IsRequired != nil {
		addClause("is_required", *upd.IsRequired)
	}
	if upd.IsIndexed != nil {
		addClause("is_indexed", *upd.IsIndexed)
	}
	if upd.DefaultValue != nil {
		b, _ := json.Marshal(upd.DefaultValue)
		addClause("default_value", b)
	}
	if upd.ValidationRules != nil {
		b, _ := json.Marshal(upd.ValidationRules)
		addClause("validation_rules", b)
	}
	if upd.EnumValues != nil {
		addClause("enum_values", upd.EnumValues)
	}
	if upd.SortOrder != nil {
		addClause("sort_order", *upd.SortOrder)
	}
	if upd.IsActive != nil {
		addClause("is_active", *upd.IsActive)
	}

	if len(setClauses) == 0 {
		return nil // nothing to update
	}

	query := "UPDATE sys_custom_field_schemas SET "
	for i, clause := range setClauses {
		if i > 0 {
			query += ", "
		}
		query += clause
	}
	query += fmt.Sprintf(" WHERE id = $%d", argIdx)
	args = append(args, id)

	tag, err := pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update custom field: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NewNotFound("custom_field", id)
	}
	return nil
}

// Deactivate soft-deletes a custom field (is_active = FALSE).
func (r *CustomFieldRepo) Deactivate(ctx context.Context, id string) error {
	pool := tenant.MustGetPool(ctx)

	tag, err := pool.Exec(ctx,
		"UPDATE sys_custom_field_schemas SET is_active = FALSE WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("deactivate custom field: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.NewNotFound("custom_field", id)
	}
	return nil
}

// --- internal helpers ---

type scannable interface {
	Scan(dest ...any) error
}

func scanCustomField(row scannable) (CustomFieldRecord, error) {
	var f CustomFieldRecord
	var defaultVal, validationRules []byte
	var enumVals []string

	err := row.Scan(
		&f.ID, &f.EntityType, &f.FieldName, &f.FieldType, &f.DisplayName,
		&f.Description, &f.IsRequired, &f.IsIndexed, &defaultVal,
		&validationRules, &f.ReferenceType, &enumVals, &f.SortOrder,
		&f.IsActive, &f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		return f, fmt.Errorf("scan custom field: %w", err)
	}

	f.EnumValues = enumVals
	unmarshalJSONB(defaultVal, &f.DefaultValue)
	unmarshalJSONBMap(validationRules, &f.ValidationRules)

	return f, nil
}

func unmarshalJSONB(data []byte, dest *any) {
	if len(data) > 0 {
		var v any
		if json.Unmarshal(data, &v) == nil {
			*dest = v
		}
	}
}

func unmarshalJSONBMap(data []byte, dest *map[string]any) {
	if len(data) > 0 {
		var m map[string]any
		if json.Unmarshal(data, &m) == nil {
			*dest = m
		}
	}
}

func isDuplicateKey(err error) bool {
	return err != nil && (
	// pgx wraps PG error codes; check for unique_violation (23505)
	fmt.Sprintf("%v", err) != "" &&
		(contains(err.Error(), "23505") || contains(err.Error(), "duplicate key")))
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
