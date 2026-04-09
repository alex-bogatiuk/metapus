// Package domain provides core business logic interfaces and types.
package domain

import (
	"context"
	"metapus/internal/domain/cursor"
	"metapus/internal/domain/filter"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
)

// --- Filter & Pagination ---

// ListFilter contains common filtering options for list operations.
// Uses cursor-based (keyset) pagination — no Offset.
type ListFilter struct {
	// Search performs full-text search on searchable fields
	Search string

	// IDs filters by specific IDs
	IDs []id.ID

	// IncludeDeleted includes soft-deleted records
	IncludeDeleted bool

	// ParentID filters by parent (for hierarchical catalogs)
	ParentID *id.ID

	// IsFolder filters folders only or items only
	IsFolder *bool

	// AdvancedFilters - list of arbitrary filters (analogous to 1C filtering)
	AdvancedFilters []filter.Item

	// OrderBy specifies sorting (e.g., "name", "-created_at")
	OrderBy string

	// Limit is the max number of items to return per page
	Limit int

	// CursorReq contains cursor-based pagination parameters (after/before/around)
	CursorReq *cursor.Request

	// DataScope provides row-level security constraints.
	// When set, repositories use it to add WHERE conditions limiting
	// visibility by organization_id, counterparty_id, etc.
	DataScope *security.DataScope
}

// DefaultListFilter returns sensible defaults.
func DefaultListFilter() ListFilter {
	return ListFilter{
		Limit:   50,
		OrderBy: "name",
	}
}

// CursorListResult contains cursor-paginated results.
type CursorListResult[T any] struct {
	Items       []T    `json:"items"`
	NextCursor  string `json:"nextCursor,omitempty"`
	PrevCursor  string `json:"prevCursor,omitempty"`
	HasMore     bool   `json:"hasMore"`
	HasPrev     bool   `json:"hasPrev"`
	TargetIndex *int   `json:"targetIndex,omitempty"`
	TotalCount  int64  `json:"totalCount"`
}

// --- Repository Interfaces ---

// CatalogRepository defines CRUD operations for catalog entities.
type CatalogRepository[T entity.Validatable] interface {
	// Create inserts a new entity
	Create(ctx context.Context, entity T) error

	// GetByID retrieves entity by ID
	GetByID(ctx context.Context, id id.ID) (T, error)

	// GetByCode retrieves entity by code (unique within tenant)
	GetByCode(ctx context.Context, code string) (T, error)

	// Update modifies existing entity (with optimistic locking)
	Update(ctx context.Context, entity T) error

	// Delete performs physical removal.
	Delete(ctx context.Context, id id.ID) error

	// SetDeletionMark sets or clears the deletion mark
	SetDeletionMark(ctx context.Context, id id.ID, marked bool) error

	// List retrieves entities with cursor-based pagination
	List(ctx context.Context, filter ListFilter) (CursorListResult[T], error)

	// Exists checks if entity with given ID exists
	Exists(ctx context.Context, id id.ID) (bool, error)

	// ExistsByCode checks if entity with given code exists
	ExistsByCode(ctx context.Context, code string) (bool, error)

	// GetTree retrieves hierarchical structure (for hierarchical catalogs)
	GetTree(ctx context.Context, rootID *id.ID) ([]T, error)

	// GetPath retrieves path from root to entity
	GetPath(ctx context.Context, id id.ID) ([]T, error)
}

// --- Hooks ---

// HookEvent represents lifecycle event type.
type HookEvent string

const (
	BeforeCreate HookEvent = "before_create"
	AfterCreate  HookEvent = "after_create"
	BeforeUpdate HookEvent = "before_update"
	AfterUpdate  HookEvent = "after_update"
	BeforeDelete HookEvent = "before_delete"
	AfterDelete  HookEvent = "after_delete"
)

// Hook is a function that runs at specific lifecycle points.
type Hook[T any] func(ctx context.Context, entity T) error

// hookEntry stores a hook with its priority and optional name.
// Lower Priority values run first (default = 0).
type hookEntry[T any] struct {
	Priority int
	Name     string
	Fn       Hook[T]
}

// HookRegistry stores lifecycle hooks for an entity type.
// Hooks are executed in priority order (lower = first).
// When multiple extensions register hooks for the same event,
// priority ensures deterministic execution order.
type HookRegistry[T any] struct {
	hooks  map[HookEvent][]hookEntry[T]
	sorted map[HookEvent]bool // tracks whether event's hooks are already sorted
}

// NewHookRegistry creates an empty hook registry.
func NewHookRegistry[T any]() *HookRegistry[T] {
	return &HookRegistry[T]{
		hooks:  make(map[HookEvent][]hookEntry[T]),
		sorted: make(map[HookEvent]bool),
	}
}

// On registers a hook for the specified event with default priority (0).
func (r *HookRegistry[T]) On(event HookEvent, hook Hook[T]) {
	r.hooks[event] = append(r.hooks[event], hookEntry[T]{Fn: hook})
	r.sorted[event] = false
}

// OnWithPriority registers a named hook with explicit priority.
// Lower priority values run first. Use this when hook ordering matters
// (e.g., validation before audit logging).
func (r *HookRegistry[T]) OnWithPriority(event HookEvent, priority int, name string, hook Hook[T]) {
	r.hooks[event] = append(r.hooks[event], hookEntry[T]{
		Priority: priority,
		Name:     name,
		Fn:       hook,
	})
	r.sorted[event] = false
}

// Run executes all hooks for the specified event in priority order.
func (r *HookRegistry[T]) Run(ctx context.Context, event HookEvent, entity T) error {
	entries := r.hooks[event]
	if len(entries) == 0 {
		return nil
	}

	// Sort by priority on first Run after registration (lazy, amortized O(1)).
	if !r.sorted[event] {
		sortHookEntries(entries)
		r.sorted[event] = true
	}

	for _, entry := range entries {
		if err := entry.Fn(ctx, entity); err != nil {
			return err
		}
	}
	return nil
}

// sortHookEntries performs insertion sort (stable, optimal for small slices).
func sortHookEntries[T any](entries []hookEntry[T]) {
	for i := 1; i < len(entries); i++ {
		key := entries[i]
		j := i - 1
		for j >= 0 && entries[j].Priority > key.Priority {
			entries[j+1] = entries[j]
			j--
		}
		entries[j+1] = key
	}
}

// Convenience methods for backward compatibility (default priority = 0)

// OnBeforeCreate registers a hook to run before create.
func (r *HookRegistry[T]) OnBeforeCreate(hook Hook[T]) {
	r.On(BeforeCreate, hook)
}

// OnAfterCreate registers a hook to run after create.
func (r *HookRegistry[T]) OnAfterCreate(hook Hook[T]) {
	r.On(AfterCreate, hook)
}

// OnBeforeUpdate registers a hook to run before update.
func (r *HookRegistry[T]) OnBeforeUpdate(hook Hook[T]) {
	r.On(BeforeUpdate, hook)
}

// OnAfterUpdate registers a hook to run after update.
func (r *HookRegistry[T]) OnAfterUpdate(hook Hook[T]) {
	r.On(AfterUpdate, hook)
}

// OnBeforeDelete registers a hook to run before delete.
func (r *HookRegistry[T]) OnBeforeDelete(hook Hook[T]) {
	r.On(BeforeDelete, hook)
}

// OnAfterDelete registers a hook to run after delete.
func (r *HookRegistry[T]) OnAfterDelete(hook Hook[T]) {
	r.On(AfterDelete, hook)
}

// RunBeforeCreate executes all before-create hooks.
func (r *HookRegistry[T]) RunBeforeCreate(ctx context.Context, entity T) error {
	return r.Run(ctx, BeforeCreate, entity)
}

// RunAfterCreate executes all after-create hooks.
func (r *HookRegistry[T]) RunAfterCreate(ctx context.Context, entity T) error {
	return r.Run(ctx, AfterCreate, entity)
}

// RunBeforeUpdate executes all before-update hooks.
func (r *HookRegistry[T]) RunBeforeUpdate(ctx context.Context, entity T) error {
	return r.Run(ctx, BeforeUpdate, entity)
}

// RunAfterUpdate executes all after-update hooks.
func (r *HookRegistry[T]) RunAfterUpdate(ctx context.Context, entity T) error {
	return r.Run(ctx, AfterUpdate, entity)
}

// RunBeforeDelete executes all before-delete hooks.
func (r *HookRegistry[T]) RunBeforeDelete(ctx context.Context, entity T) error {
	return r.Run(ctx, BeforeDelete, entity)
}

// RunAfterDelete executes all after-delete hooks.
func (r *HookRegistry[T]) RunAfterDelete(ctx context.Context, entity T) error {
	return r.Run(ctx, AfterDelete, entity)
}
