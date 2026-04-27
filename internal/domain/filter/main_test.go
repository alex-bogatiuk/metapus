package filter_test

import (
	"fmt"
	"testing"

	"metapus/internal/domain/filter"

	"github.com/Masterminds/squirrel"
)

func TestSquirrelDate(t *testing.T) {
	sql, args, _ := squirrel.Eq{"DATE(foo)": "2026-01-01"}.ToSql()
	fmt.Println(sql, args)
	sql, args, _ = squirrel.Lt{"DATE(foo)": "2026-01-01"}.ToSql()
	fmt.Println(sql, args)
}

// --- M5: BuildSearchConditions tests ---

func TestBuildSearchConditions_Empty(t *testing.T) {
	if cond := filter.BuildSearchConditions("", []string{"name"}); cond != nil {
		t.Error("expected nil for empty query")
	}
	if cond := filter.BuildSearchConditions("foo", nil); cond != nil {
		t.Error("expected nil for empty searchCols")
	}
	if cond := filter.BuildSearchConditions("   ", []string{"name"}); cond != nil {
		t.Error("expected nil for whitespace-only query")
	}
}

func TestBuildSearchConditions_SingleToken(t *testing.T) {
	cond := filter.BuildSearchConditions("красн", []string{"name", "code"})
	if cond == nil {
		t.Fatal("expected non-nil condition")
	}
	sql, args, err := cond.ToSql()
	if err != nil {
		t.Fatalf("ToSql error: %v", err)
	}
	// Should produce: (name ILIKE ? OR code ILIKE ?)
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d: %v", len(args), args)
	}
	for _, arg := range args {
		if arg != "%красн%" {
			t.Errorf("unexpected arg: %v", arg)
		}
	}
	t.Logf("SQL: %s, Args: %v", sql, args)
}

func TestBuildSearchConditions_MultiToken(t *testing.T) {
	cond := filter.BuildSearchConditions("красн авто", []string{"name"})
	if cond == nil {
		t.Fatal("expected non-nil condition")
	}
	sql, args, err := cond.ToSql()
	if err != nil {
		t.Fatalf("ToSql error: %v", err)
	}
	// Should produce: (name ILIKE ?) AND (name ILIKE ?)
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d: %v", len(args), args)
	}
	if args[0] != "%красн%" || args[1] != "%авто%" {
		t.Errorf("unexpected args: %v", args)
	}
	t.Logf("SQL: %s, Args: %v", sql, args)
}

func TestBuildSearchConditions_EscapeWildcards(t *testing.T) {
	// User searching for "100%" should not inject SQL wildcards
	cond := filter.BuildSearchConditions("100% сок", []string{"name"})
	if cond == nil {
		t.Fatal("expected non-nil condition")
	}
	_, args, err := cond.ToSql()
	if err != nil {
		t.Fatalf("ToSql error: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}
	// The "100%" token should have its % escaped
	if args[0] != `%100\%%` {
		t.Errorf("expected escaped %% in first arg, got: %v", args[0])
	}
	if args[1] != "%сок%" {
		t.Errorf("unexpected second arg: %v", args[1])
	}
}

func TestBuildSearchConditions_EscapeUnderscore(t *testing.T) {
	cond := filter.BuildSearchConditions("item_name", []string{"name"})
	if cond == nil {
		t.Fatal("expected non-nil condition")
	}
	_, args, _ := cond.ToSql()
	if args[0] != `%item\_name%` {
		t.Errorf("expected escaped underscore, got: %v", args[0])
	}
}

// --- M5: ExtractSearchQuery tests ---

func TestExtractSearchQuery_NoSearch(t *testing.T) {
	items := []filter.Item{
		{Field: "name", Operator: filter.Equal, Value: "foo"},
	}
	query, remaining := filter.ExtractSearchQuery(items)
	if query != "" {
		t.Errorf("expected empty query, got: %q", query)
	}
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining item, got %d", len(remaining))
	}
}

func TestExtractSearchQuery_WithSearch(t *testing.T) {
	items := []filter.Item{
		{Field: "name", Operator: filter.Equal, Value: "foo"},
		{Field: "__search", Operator: filter.Contains, Value: "красн авто"},
		{Field: "code", Operator: filter.Equal, Value: "bar"},
	}
	query, remaining := filter.ExtractSearchQuery(items)
	if query != "красн авто" {
		t.Errorf("expected 'красн авто', got: %q", query)
	}
	if len(remaining) != 2 {
		t.Errorf("expected 2 remaining items, got %d", len(remaining))
	}
	for _, item := range remaining {
		if item.Field == "__search" {
			t.Error("__search should have been removed from remaining")
		}
	}
}

func TestExtractSearchQuery_NonStringValue(t *testing.T) {
	items := []filter.Item{
		{Field: "__search", Operator: filter.Contains, Value: 42},
	}
	query, remaining := filter.ExtractSearchQuery(items)
	if query != "" {
		t.Errorf("expected empty query for non-string Value, got: %q", query)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining, got %d", len(remaining))
	}
}
