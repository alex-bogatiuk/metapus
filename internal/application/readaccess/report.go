package readaccess

import (
	"context"
	"slices"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/domain/reports/schema"
)

func (s *Service) RequireReport(ctx context.Context, ds *schema.Dataset) error {
	user := appctx.GetUser(ctx)
	if user == nil {
		return apperror.NewUnauthorized("report executor is required")
	}
	if ds.Permission == "" || (!user.IsAdmin && !slices.Contains(user.Permissions, ds.Permission)) {
		return apperror.NewForbidden("insufficient report permissions")
	}
	return nil
}

func (s *Service) RequireReportFields(ctx context.Context, access []schema.FieldAccess) error {
	for _, dependency := range access {
		if err := s.RequireFields(ctx, dependency.Entity, dependency.Fields...); err != nil {
			return err
		}
	}
	return nil
}
