package readaccess

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"strings"
)

// RequireFields checks trusted, explicitly declared policy dependencies. A
// computed amount may depend on several header and table-part fields.
func (s *Service) RequireFields(ctx context.Context, key string, fields ...string) error {
	d, err := s.Require(ctx, key)
	if err != nil {
		return err
	}
	policy := security.GetFieldPolicy(ctx, d.Key, "read")
	for _, field := range fields {
		for _, registered := range d.Fields {
			if (registered.Name == field || registered.Column == field) && registered.WriteOnly {
				return apperror.NewForbidden("report field is unavailable")
			}
		}
		if policy != nil && !policy.IsFieldAllowed(field) {
			return apperror.NewForbidden("report field is unavailable").WithDetail("field", field)
		}
		if root, column, nested := strings.Cut(field, "."); nested && policy != nil {
			if !policy.IsFieldAllowed(root) || !policy.IsTablePartFieldAllowed(root, column) {
				return apperror.NewForbidden("report field is unavailable").WithDetail("field", field)
			}
		}
	}
	return nil
}
