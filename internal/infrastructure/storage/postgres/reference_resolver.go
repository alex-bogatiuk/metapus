// Package postgres provides the generic ReferenceResolver for batch-resolving
// catalog IDs to their display names. This eliminates N+1 queries when
// building response DTOs that contain reference fields.
//
// Analogous to 1С's "Представление()" / ERPNext's link title_field resolution.
package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/Masterminds/squirrel"

	"metapus/internal/core/id"
)

// RefDisplay is a lightweight representation of a referenced catalog entity.
// Sent alongside the raw ID so the frontend can display human-readable names.
type RefDisplay struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CurrencyRefDisplay extends RefDisplay with currency-specific fields.
// Used in CurrencyAware document responses so the frontend knows how to
// format monetary values (decimal places, symbol) without extra API calls.
type CurrencyRefDisplay struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DecimalPlaces int    `json:"decimalPlaces"`
	Symbol        string `json:"symbol"`
}

// ReferenceResolver batch-resolves catalog IDs to display names.
// It collects IDs per table, executes one SELECT per table, and returns a map.
//
// Usage:
//
//	resolver := NewReferenceResolver()
//	resolver.Add("cat_counterparties", doc.SupplierID)
//	resolver.Add("cat_warehouses", doc.WarehouseID)
//	resolved, err := resolver.Resolve(ctx, querier)
//	supplierName := resolved["cat_counterparties"][doc.SupplierID.String()]
type ReferenceResolver struct {
	// table → set of IDs to resolve
	pending map[string]map[id.ID]struct{}
}

// NewReferenceResolver creates a new resolver.
func NewReferenceResolver() *ReferenceResolver {
	return &ReferenceResolver{
		pending: make(map[string]map[id.ID]struct{}),
	}
}

// Add registers an ID to be resolved from the given table.
// Nil/zero IDs are silently ignored.
func (r *ReferenceResolver) Add(table string, entityID id.ID) {
	if id.IsNil(entityID) {
		return
	}
	if r.pending[table] == nil {
		r.pending[table] = make(map[id.ID]struct{})
	}
	r.pending[table][entityID] = struct{}{}
}

// AddPtr registers a pointer ID (for optional references).
func (r *ReferenceResolver) AddPtr(table string, entityID *id.ID) {
	if entityID == nil || id.IsNil(*entityID) {
		return
	}
	r.Add(table, *entityID)
}

// ResolvedRefs maps table → id_string → RefDisplay.
type ResolvedRefs map[string]map[string]RefDisplay

// Get returns the display for a given table and ID. Returns empty RefDisplay if not found.
func (rr ResolvedRefs) Get(table string, entityID id.ID) RefDisplay {
	if id.IsNil(entityID) {
		return RefDisplay{}
	}
	if m, ok := rr[table]; ok {
		if d, ok := m[entityID.String()]; ok {
			return d
		}
	}
	return RefDisplay{ID: entityID.String()}
}

// GetPtr returns a *RefDisplay for an optional reference. Returns nil if ID is nil.
func (rr ResolvedRefs) GetPtr(table string, entityID *id.ID) *RefDisplay {
	if entityID == nil || id.IsNil(*entityID) {
		return nil
	}
	d := rr.Get(table, *entityID)
	return &d
}

// ResolvedCurrencyRefs maps id_string → CurrencyRefDisplay.
type ResolvedCurrencyRefs map[string]CurrencyRefDisplay

// Get returns the CurrencyRefDisplay for a given ID. Returns empty struct if not found.
func (cr ResolvedCurrencyRefs) Get(entityID id.ID) CurrencyRefDisplay {
	if id.IsNil(entityID) {
		return CurrencyRefDisplay{}
	}
	if d, ok := cr[entityID.String()]; ok {
		return d
	}
	return CurrencyRefDisplay{ID: entityID.String()}
}

// GetPtr returns a *CurrencyRefDisplay for an optional reference. Returns nil if ID is nil.
func (cr ResolvedCurrencyRefs) GetPtr(entityID *id.ID) *CurrencyRefDisplay {
	if entityID == nil || id.IsNil(*entityID) {
		return nil
	}
	d := cr.Get(*entityID)
	return &d
}

// Resolve executes batch queries to resolve all pending IDs.
// Uses one SELECT per table: SELECT id, name FROM <table> WHERE id IN (...)
func (r *ReferenceResolver) Resolve(ctx context.Context, querier Querier) (ResolvedRefs, error) {
	result := make(ResolvedRefs, len(r.pending))

	for table, idSet := range r.pending {
		if len(idSet) == 0 {
			continue
		}

		ids := make([]id.ID, 0, len(idSet))
		for eid := range idSet {
			ids = append(ids, eid)
		}

		refs, err := batchResolveName(ctx, querier, table, ids)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", table, err)
		}
		result[table] = refs
	}

	return result, nil
}

// ResolveCurrencies batch-resolves currency IDs with extra fields (decimal_places, symbol).
// This is the systematic way for any CurrencyAware document to get formatting metadata.
func (r *ReferenceResolver) ResolveCurrencies(ctx context.Context, querier Querier) (ResolvedCurrencyRefs, error) {
	const table = "cat_currencies"
	idSet := r.pending[table]
	if len(idSet) == 0 {
		return ResolvedCurrencyRefs{}, nil
	}

	ids := make([]id.ID, 0, len(idSet))
	for eid := range idSet {
		ids = append(ids, eid)
	}

	return batchResolveCurrency(ctx, querier, ids)
}

// batchResolveName fetches id + name for a batch of IDs from a single table.
// Uses the package-level Querier interface from tx_manager.go.
func batchResolveName(ctx context.Context, q Querier, table string, ids []id.ID) (map[string]RefDisplay, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Determine the display expression per table.
	// Most catalogs expose "name" (or "code"), while users require full name composition.
	displayCol := displayExprForTable(table)

	// Build query: SELECT id, <displayCol> FROM <table> WHERE id = ANY($1)
	// We use raw SQL here for simplicity — table names are controlled by code, not user input.
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, eid := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = eid
	}

	query := fmt.Sprintf(
		"SELECT id, %s AS display_name FROM %s WHERE id IN (%s)",
		displayCol, table, strings.Join(placeholders, ","),
	)

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", table, err)
	}
	defer rows.Close()

	result := make(map[string]RefDisplay, len(ids))
	for rows.Next() {
		var eid id.ID
		var name string
		if err := rows.Scan(&eid, &name); err != nil {
			return nil, fmt.Errorf("scan %s: %w", table, err)
		}
		result[eid.String()] = RefDisplay{
			ID:   eid.String(),
			Name: name,
		}
	}

	return result, rows.Err()
}

func displayExprForTable(table string) string {
	switch table {
	case "users":
		// Required format for audit labels: "Фамилия Имя".
		// Fallback to email, then raw ID for robustness.
		return "COALESCE(NULLIF(TRIM(CONCAT_WS(' ', NULLIF(last_name, ''), NULLIF(first_name, ''))), ''), email, id::text)"
	default:
		return "COALESCE(name, code, id::text)"
	}
}

// batchResolveCurrency fetches id, name, decimal_places, symbol for a batch of currency IDs.
func batchResolveCurrency(ctx context.Context, q Querier, ids []id.ID) (ResolvedCurrencyRefs, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, eid := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = eid
	}

	query := fmt.Sprintf(
		"SELECT id, COALESCE(name, code, id::text), decimal_places, COALESCE(symbol, '') FROM cat_currencies WHERE id IN (%s)",
		strings.Join(placeholders, ","),
	)

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query cat_currencies: %w", err)
	}
	defer rows.Close()

	result := make(ResolvedCurrencyRefs, len(ids))
	for rows.Next() {
		var eid id.ID
		var name string
		var decimalPlaces int
		var symbol string
		if err := rows.Scan(&eid, &name, &decimalPlaces, &symbol); err != nil {
			return nil, fmt.Errorf("scan cat_currencies: %w", err)
		}
		result[eid.String()] = CurrencyRefDisplay{
			ID:            eid.String(),
			Name:          name,
			DecimalPlaces: decimalPlaces,
			Symbol:        symbol,
		}
	}

	return result, rows.Err()
}

// Helper to build using squirrel (alternative, not used currently but available)
func buildResolveQuery(table string, ids []id.ID) (string, []any, error) {
	q := squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar).
		Select("id", "COALESCE(name, code, id::text) AS display_name").
		From(table).
		Where(squirrel.Eq{"id": ids})

	return q.ToSql()
}
