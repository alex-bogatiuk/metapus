package compiler

import (
	"context"
	"errors"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/reports/schema"
)

func reportPathError(message, field string, err error) error {
	var app *apperror.AppError
	if errors.As(err, &app) {
		return err
	}
	return apperror.NewValidation(message).WithDetail("field", field).WithCause(err)
}

func (c *Compiler) validateParameters(ctx context.Context, ds *schema.Dataset, params map[string]any) error {
	definitions := make(map[string]schema.FilterDef, len(ds.Filters))
	for _, definition := range ds.Filters {
		definitions[definition.Key] = definition
		if definition.Required {
			if _, ok := params[definition.Key]; !ok {
				return apperror.NewValidation("required report parameter is missing").WithDetail("field", definition.Key)
			}
		}
	}
	for key, value := range params {
		if definition, ok := definitions[key]; ok {
			if len(definition.Access) == 0 {
				return apperror.NewForbidden("report parameter policy is not registered")
			}
			if err := c.reads.RequireReportFields(ctx, definition.Access); err != nil {
				return err
			}
			valid := false
			switch definition.Type {
			case schema.FilterDate:
				if text, ok := value.(string); ok {
					_, full := time.Parse(time.RFC3339, text)
					_, date := time.Parse("2006-01-02", text)
					valid = full == nil || date == nil
				}
			case schema.FilterBoolean:
				_, valid = value.(bool)
			case schema.FilterString, schema.FilterEnum:
				_, valid = value.(string)
			case schema.FilterRef:
				valid = validReferenceParameter(value)
			}
			if !valid {
				return apperror.NewValidation("invalid report parameter").WithDetail("field", key)
			}
			continue
		}
		field := ds.FindField(key)
		if field == nil {
			return apperror.NewValidation("unknown report parameter").WithDetail("field", key)
		}
		if len(field.Access) == 0 {
			return apperror.NewForbidden("report field policy is not registered")
		}
		if err := c.reads.RequireReportFields(ctx, field.Access); err != nil {
			return err
		}
		// Executors declare their parameter contract through Filters. Their only
		// direct field parameters are lists of reference IDs (scope dimensions).
		if ds.ExecutorKey != "" && (field.Type != schema.TypeRef || !validReferenceParameter(value)) {
			return apperror.NewValidation("unsupported report parameter").WithDetail("field", key)
		}
	}
	return nil
}

func validReferenceParameter(value any) bool {
	var values []string
	switch list := value.(type) {
	case []string:
		values = list
	case []any:
		for _, item := range list {
			text, ok := item.(string)
			if !ok {
				return false
			}
			values = append(values, text)
		}
	default:
		return false
	}
	for _, value := range values {
		if _, err := id.Parse(value); err != nil {
			return false
		}
	}
	return true
}
