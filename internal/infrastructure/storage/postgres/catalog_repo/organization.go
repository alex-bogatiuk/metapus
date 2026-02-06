package catalog_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/apperror"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/organization"
	"metapus/internal/infrastructure/storage/postgres"
)

const organizationTable = "cat_organizations"

// OrganizationRepo implements organization.Repository.
type OrganizationRepo struct {
	*BaseCatalogRepo[*organization.Organization]
}

// NewOrganizationRepo creates a new organization repository.
func NewOrganizationRepo() *OrganizationRepo {
	return &OrganizationRepo{
		BaseCatalogRepo: NewBaseCatalogRepo[*organization.Organization](
			organizationTable,
			postgres.ExtractDBColumns[organization.Organization](),
			func() *organization.Organization { return &organization.Organization{} },
		),
	}
}

// GetDefault retrieves the default organization.
func (r *OrganizationRepo) GetDefault(ctx context.Context) (*organization.Organization, error) {
	org := &organization.Organization{}

	q := r.Builder().
		Select(r.selectCols...).
		From(organizationTable).
		Where(squirrel.Eq{"is_default": true, "deletion_mark": false}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, org, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound(organizationTable, "default")
		}
		return nil, fmt.Errorf("get default organization: %w", err)
	}

	return org, nil
}

// List implements organization.Repository.
func (r *OrganizationRepo) List(ctx context.Context, filter domain.ListFilter) (domain.ListResult[*organization.Organization], error) {
	return r.BaseCatalogRepo.List(ctx, filter)
}
