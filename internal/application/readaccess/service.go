// Package readaccess applies entity permissions, row policies and field
// projections before supplemental reads leave the application boundary.
package readaccess

import (
	"context"
	"fmt"
	"slices"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
)

type Field struct {
	Name, Column string
	WriteOnly    bool
}
type Definition struct {
	Key, Permission, Table string
	RequiredRoles          []string
	Dimensions             map[string]string
	Fields                 []Field
	SoftDelete             bool
	// MovementSources maps movement column keys to the source document
	// fields that disclose them (see platform.MovementSourceProvider).
	MovementSources map[string][]string
}
type Registry interface {
	Definition(string) (Definition, bool)
}
type Query struct {
	IDs           []id.ID
	After         id.ID
	Limit         uint64
	Search        string
	SearchColumns []string
	Scope         *security.DataScope
}
type Record struct {
	ID         id.ID
	Entity     any
	Fields     map[string]any
	Dimensions map[string]string
}
type Repository interface {
	Read(context.Context, Definition, Query) ([]Record, error)
}
type Projection struct {
	ID     id.ID
	Fields map[string]any
}
type Service struct {
	registry Registry
	repo     Repository
	policies *security.PolicyEngine
}

func New(reg Registry, repo Repository, policies *security.PolicyEngine) *Service {
	return &Service{reg, repo, policies}
}

func (s *Service) Definition(key string) (Definition, bool) { return s.registry.Definition(key) }
func (s *Service) Require(ctx context.Context, key string) (Definition, error) {
	d, ok := s.registry.Definition(key)
	if !ok || (d.Permission == "" && len(d.RequiredRoles) == 0) {
		return Definition{}, apperror.NewForbidden("entity read permission is not registered")
	}
	user := appctx.GetUser(ctx)
	if user == nil {
		return Definition{}, apperror.NewUnauthorized("authentication required")
	}
	for _, role := range d.RequiredRoles {
		if !slices.Contains(user.Roles, role) {
			return Definition{}, apperror.NewForbidden("insufficient role")
		}
	}
	if d.Permission != "" && !user.IsAdmin && !slices.Contains(user.Permissions, d.Permission) {
		return Definition{}, apperror.NewForbidden("insufficient permissions").WithDetail("required_permission", d.Permission)
	}
	return d, nil
}

func FieldAllowed(ctx context.Context, d Definition, field Field) bool {
	if field.WriteOnly {
		return false
	}
	policy := security.GetFieldPolicy(ctx, d.Key, "read")
	if policy == nil {
		return true
	}
	name := field.Column
	if name == "" {
		name = field.Name
	}
	return policy.IsFieldAllowed(name)
}

func (s *Service) ReadIDs(ctx context.Context, key string, ids []id.ID) ([]Projection, error) {
	d, err := s.Require(ctx, key)
	if err != nil {
		return nil, err
	}
	unique := make([]id.ID, 0, len(ids))
	seen := make(map[id.ID]bool, len(ids))
	for _, v := range ids {
		if !id.IsNil(v) && !seen[v] {
			seen[v] = true
			unique = append(unique, v)
		}
	}
	if len(unique) == 0 {
		return []Projection{}, nil
	}
	rows, err := s.read(ctx, d, Query{IDs: unique})
	if err != nil {
		return nil, err
	}
	return s.project(ctx, d, rows), nil
}

func (s *Service) ReadOne(ctx context.Context, key string, uid id.ID) (Projection, error) {
	rows, err := s.ReadIDs(ctx, key, []id.ID{uid})
	if err != nil {
		return Projection{}, err
	}
	if len(rows) == 0 {
		return Projection{}, apperror.NewNotFound(key, uid.String())
	}
	return rows[0], nil
}

// Search checks fields before building predicates. Candidate batches are
// exhausted until enough allowed rows are found, without exposing raw totals.
func (s *Service) Search(ctx context.Context, key, query string, columns []string, limit int) ([]Projection, error) {
	d, err := s.Require(ctx, key)
	if err != nil {
		return nil, err
	}
	allowed := make([]string, 0, len(columns))
	for _, column := range columns {
		for _, field := range d.Fields {
			if field.Column == column && FieldAllowed(ctx, d, field) {
				allowed = append(allowed, column)
				break
			}
		}
	}
	if len(allowed) == 0 || limit <= 0 {
		return []Projection{}, nil
	}
	result := make([]Projection, 0, limit)
	after := id.Nil()
	for len(result) < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows, err := s.read(ctx, d, Query{Search: query, SearchColumns: allowed, Limit: 100, After: after})
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		result = append(result, s.project(ctx, d, rows)...)
		after = rows[len(rows)-1].ID
		if len(rows) < 100 {
			break
		}
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *Service) read(ctx context.Context, d Definition, q Query) ([]Record, error) {
	// All constraints originate here. Adapters do not reconstruct policies.
	q.Scope = security.GetDataScope(ctx)
	rules := security.GetApplicableRules(ctx, d.Key, "read")
	if len(rules) > 0 {
		if s.policies == nil {
			return nil, apperror.NewForbidden("policy evaluator is unavailable")
		}
		available := make(map[string]bool, len(d.Fields)*2)
		for _, f := range d.Fields {
			if f.Column != "" {
				available[f.Name] = true
				available[f.Column] = true
			}
		}
		if err := s.policies.RequireAvailableFields(rules, available); err != nil {
			return []Record{}, nil
		}
	}
	rows, err := s.repo.Read(ctx, d, q)
	if err != nil {
		return nil, fmt.Errorf("authorized read %s: %w", d.Key, err)
	}
	return rows, nil
}

func (s *Service) project(ctx context.Context, d Definition, rows []Record) []Projection {
	rules := security.GetApplicableRules(ctx, d.Key, "read")
	result := make([]Projection, 0, len(rows))
	for _, row := range rows {
		if !security.GetDataScope(ctx).CanAccessRecord(d.Key, row.Dimensions) {
			continue
		}
		if len(rules) > 0 && (s.policies == nil || s.policies.Evaluate(ctx, rules, "read", row.Entity) != nil) {
			continue
		}
		fields := make(map[string]any)
		for _, field := range d.Fields {
			if FieldAllowed(ctx, d, field) {
				if v, ok := row.Fields[field.Name]; ok {
					fields[field.Name] = v
				}
			}
		}
		result = append(result, Projection{ID: row.ID, Fields: fields})
	}
	return result
}

type contextKey struct{}

func WithService(ctx context.Context, s *Service) context.Context {
	return context.WithValue(ctx, contextKey{}, s)
}
func FromContext(ctx context.Context) (*Service, bool) {
	s, ok := ctx.Value(contextKey{}).(*Service)
	return s, ok && s != nil
}

// Definitions and PolicyEngine expose framework-free dependencies to outbound
// query adapters. Application never receives their SQL representation.
func (s *Service) Definitions() Registry                { return s.registry }
func (s *Service) PolicyEngine() *security.PolicyEngine { return s.policies }
