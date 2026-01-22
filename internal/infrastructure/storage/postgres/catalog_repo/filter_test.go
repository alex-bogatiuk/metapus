package catalog_repo

import (
	"context"
	"metapus/internal/domain/filter"
	"testing"
)

func TestApplyAdvancedFilters_Operators(t *testing.T) {
	repo := NewBaseCatalogRepo[any]("test_table", []string{"id", "col1"}, func() any { return nil })
	ctx := context.Background()

	tests := []struct {
		name     string
		item     filter.Item
		wantSQL  string
		wantArgs []any
	}{
		{
			name:     "Greater",
			item:     filter.Item{Field: "col1", Operator: filter.Greater, Value: 10},
			wantSQL:  "SELECT id, col1 FROM test_table WHERE col1 > $1",
			wantArgs: []any{10},
		},
		{
			name:     "Less",
			item:     filter.Item{Field: "col1", Operator: filter.Less, Value: 5},
			wantSQL:  "SELECT id, col1 FROM test_table WHERE col1 < $1",
			wantArgs: []any{5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseQ := repo.baseSelect(ctx)
			q, err := repo.applyAdvancedFilters(ctx, baseQ, []filter.Item{tt.item})
			if err != nil {
				t.Fatalf("applyAdvancedFilters failed: %v", err)
			}

			sql, args, err := q.ToSql()
			if err != nil {
				t.Fatalf("ToSql failed: %v", err)
			}

			if sql != tt.wantSQL {
				t.Errorf("SQL mismatch\nwant: %s\ngot:  %s", tt.wantSQL, sql)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("Args count mismatch\nwant: %d\ngot:  %d", len(tt.wantArgs), len(args))
			}
			if args[0] != tt.wantArgs[0] {
				t.Errorf("Args mismatch\nwant: %v\ngot:  %v", tt.wantArgs[0], args[0])
			}
		})
	}
}
