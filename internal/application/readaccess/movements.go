package readaccess

import (
	"context"
	"strings"
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain"
)

type ReadSnapshot func(context.Context, func(context.Context) error) error

// ReadMovements authorizes the complete source document before any provider is
// called. Source, movement data and related presentations share one snapshot.
func (s *Service) ReadMovements(ctx context.Context, key string, uid id.ID, snapshot ReadSnapshot, authorize func(context.Context, id.ID) error, providers []entity.MovementProvider, resolver domain.RefResolver) ([]entity.DocumentMovement, error) {
	definition, err := s.Require(ctx, key)
	if err != nil {
		return nil, err
	}
	result := make([]entity.DocumentMovement, 0)
	err = snapshot(ctx, func(ctx context.Context) error {
		if err := authorize(ctx, uid); err != nil {
			return err
		}
		for _, provider := range providers {
			rows, err := provider.GetDocumentMovements(ctx, uid)
			if err != nil {
				return err
			}
			for _, row := range rows {
				projected, ok := projectMovement(ctx, definition, row)
				if ok {
					result = append(result, projected)
				}
			}
		}
		if resolver != nil {
			return EnrichMovementRefs(ctx, result, resolver)
		}
		for _, row := range result {
			for _, column := range row.Columns {
				if column.Type == "ref" || column.Type == "amount" {
					delete(row.Data, column.Key)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func sourceFieldAllowed(policy *security.FieldPolicy, path string) bool {
	if part, field, ok := strings.Cut(path, "."); ok {
		return policy.IsFieldAllowed(path) && policy.IsFieldAllowed(part) && policy.IsTablePartFieldAllowed(part, field)
	}
	return policy.IsFieldAllowed(path)
}

// projectMovement hides movement columns the reader may not see. A source
// field policy admits only columns the document declares in MovementSources
// with every listed field readable; a register policy admits only readable
// PolicyFields.
func projectMovement(ctx context.Context, source Definition, row entity.DocumentMovement) (entity.DocumentMovement, bool) {
	sourcePolicy := security.GetFieldPolicy(ctx, source.Key, "read")
	registerPolicy := security.GetFieldPolicy(ctx, row.PolicyEntity, "read")
	if sourcePolicy != nil && !sourcePolicy.IsFieldAllowed("record_type") {
		row.RecordType = ""
	}
	if registerPolicy != nil {
		if !registerPolicy.IsFieldAllowed("period") {
			row.Period = time.Time{}
		}
		if !registerPolicy.IsFieldAllowed("record_type") {
			row.RecordType = ""
		}
	}
	dependencies := source.MovementSources
	// Presence of a movement would disclose the posting state itself.
	if sourcePolicy != nil && !sourcePolicy.IsFieldAllowed("posted") {
		return entity.DocumentMovement{}, false
	}
	if sourcePolicy != nil && !sourcePolicy.IsFieldAllowed("date") {
		row.Period = time.Time{}
	}
	columns := make([]entity.MovementColumnDef, 0, len(row.Columns))
	values := make(map[string]any)
	for _, column := range row.Columns {
		paths, mapped := dependencies[column.Key]
		allowed := true
		if sourcePolicy != nil {
			allowed = mapped
			for _, path := range paths {
				allowed = allowed && sourceFieldAllowed(sourcePolicy, path)
			}
		}
		if registerPolicy != nil {
			allowed = allowed && column.PolicyField != "" && registerPolicy.IsFieldAllowed(column.PolicyField)
		}
		if allowed {
			columns = append(columns, column)
			if value, ok := row.Data[column.Key]; ok {
				values[column.Key] = value
			}
		}
	}
	row.Columns, row.Data = columns, values
	return row, len(columns) > 0
}
