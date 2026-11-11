// Package postgres provides the generic ReferenceResolver for batch-resolving
// catalog IDs to their display names. This eliminates N+1 queries when
// building response DTOs that contain reference fields.
//
// Analogous to 1C's "Presentation()" / ERPNext's link title_field resolution.
package postgres

import (
	"context"
	"fmt"
	"metapus/internal/application/readaccess"
	"metapus/internal/core/apperror"
	"net/http"
	"strings"

	"metapus/internal/core/id"
)

// RefDisplay is a lightweight representation of a referenced catalog entity.
// Sent alongside the raw ID so the frontend can display human-readable names.
type RefDisplay struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Code string `json:"code,omitempty"`
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
//	resolver.Add("cat_counterparties", doc.CounterpartyID)
//	resolver.Add("cat_merchants", doc.MerchantID)
//	resolved, err := resolver.Resolve(ctx, querier)
//	supplierName := resolved["cat_counterparties"][doc.CounterpartyID.String()]
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
	return RefDisplay{}
}

// GetPtr returns a *RefDisplay for an optional reference. Returns nil if ID is nil.
func (rr ResolvedRefs) GetPtr(table string, entityID *id.ID) *RefDisplay {
	if entityID == nil || id.IsNil(*entityID) {
		return nil
	}
	d := rr.Get(table, *entityID)
	if d.ID == "" {
		return nil
	}
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

// referenceRows reads reference projections; the supplied
// querier is intentionally not a way to bypass application authorization.
func referenceRows(ctx context.Context, table string, ids []id.ID) ([]readaccess.Projection, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	reads, ok := readaccess.FromContext(ctx)
	if !ok {
		return nil, apperror.NewForbidden("authorized reference reader is unavailable")
	}
	if _, err := reads.Require(ctx, table); err != nil {
		if apperror.GetHTTPStatus(err) == http.StatusForbidden {
			return nil, nil
		}
		return nil, err
	}
	return reads.ReadIDs(ctx, table, ids)
}
func batchResolveName(ctx context.Context, _ Querier, table string, ids []id.ID) (map[string]RefDisplay, error) {
	rows, err := referenceRows(ctx, table, ids)
	if err != nil {
		return nil, err
	}
	result := make(map[string]RefDisplay, len(rows))
	for _, row := range rows {
		name, _ := row.Fields["name"].(string)
		code, _ := row.Fields["code"].(string)
		if table == "users" {
			first, _ := row.Fields["firstName"].(string)
			last, _ := row.Fields["lastName"].(string)
			name = strings.TrimSpace(last + " " + first)
			if name == "" {
				name, _ = row.Fields["email"].(string)
			}
		}
		result[row.ID.String()] = RefDisplay{ID: row.ID.String(), Name: name, Code: code}
	}
	return result, nil
}
