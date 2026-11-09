package variants

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/apperror"
	corectx "metapus/internal/core/context"
)

type variantRepositoryStub struct {
	Repository
	existing       ReportVariant
	written        *ReportVariant
	deletedVersion int
}

func (r *variantRepositoryStub) GetByID(context.Context, uuid.UUID) (*ReportVariant, error) {
	v := r.existing
	return &v, nil
}
func (r *variantRepositoryStub) Create(_ context.Context, v *ReportVariant) error {
	saved := *v
	r.written = &saved
	return nil
}
func (r *variantRepositoryStub) Update(ctx context.Context, v *ReportVariant) error {
	return r.Create(ctx, v)
}
func (r *variantRepositoryStub) Delete(_ context.Context, _ uuid.UUID, version int) error {
	r.deletedVersion = version
	return nil
}
func (r *variantRepositoryStub) GetList(context.Context, string, uuid.UUID) ([]*ReportVariant, error) {
	return []*ReportVariant{}, nil
}

func TestSharedReadersCannotUpdateDeleteOrPromoteVariants(t *testing.T) {
	owner, reader := uuid.New(), uuid.New()
	ctx := corectx.WithUser(context.Background(), &corectx.UserContext{UserID: reader.String(), Permissions: []string{"reports.read"}})
	repo := &variantRepositoryStub{existing: ReportVariant{ID: uuid.New(), DatasetKey: "sales", Name: "Shared", Visibility: VisibilityShared, AuthorID: &owner, Version: 7}}
	svc := NewService(repo, map[string]string{"sales": "reports.read"})
	for _, visibility := range []Visibility{VisibilityShared, VisibilityPersonal, VisibilitySystem} {
		incoming := ReportVariant{ID: repo.existing.ID, Name: "Changed", Visibility: visibility, Version: 7}
		require.Error(t, svc.Update(ctx, &incoming))
		require.Nil(t, repo.written)
	}
	require.Error(t, svc.Delete(ctx, repo.existing.ID))
	require.Zero(t, repo.deletedVersion)
	_, err := svc.GetList(ctx, "sales")
	require.NoError(t, err, "shared visibility permits reading, not editing")
}

func TestVariantOwnershipAndSystemTransitions(t *testing.T) {
	owner, admin, spoofed := uuid.New(), uuid.New(), uuid.New()
	ownerCtx := corectx.WithUser(context.Background(), &corectx.UserContext{UserID: owner.String(), Permissions: []string{"reports.read"}})
	adminCtx := corectx.WithUser(context.Background(), &corectx.UserContext{UserID: admin.String(), IsAdmin: true})
	repo := &variantRepositoryStub{}
	svc := NewService(repo, map[string]string{"sales": "reports.read"})
	v := &ReportVariant{ID: uuid.New(), DatasetKey: "sales", Name: "Mine", Visibility: VisibilityShared, AuthorID: &spoofed}
	require.NoError(t, svc.Create(ownerCtx, v))
	require.Equal(t, owner, *repo.written.AuthorID)
	repo.existing = *repo.written
	v.Version, v.Visibility = 4, VisibilitySystem
	require.Error(t, svc.Update(ownerCtx, v))
	require.NoError(t, svc.Update(adminCtx, v))
	require.Nil(t, repo.written.AuthorID)
	require.Equal(t, 4, repo.written.Version, "client version must reach the repository unchanged")
	repo.existing = *repo.written
	v.DatasetKey, v.Visibility = "spoofed", VisibilityShared
	require.NoError(t, svc.Update(adminCtx, v))
	require.Equal(t, "sales", repo.written.DatasetKey)
	require.Equal(t, admin, *repo.written.AuthorID)
}

func TestDatasetPermissionCoversEveryVariantOperation(t *testing.T) {
	owner := uuid.New()
	repo := &variantRepositoryStub{existing: ReportVariant{ID: uuid.New(), DatasetKey: "sales", Name: "Mine", Visibility: VisibilityPersonal, AuthorID: &owner, Version: 3}}
	svc := NewService(repo, map[string]string{"sales": "reports.read"})
	ctx := corectx.WithUser(context.Background(), &corectx.UserContext{UserID: owner.String()})
	v := repo.existing
	for _, operation := range []func() error{
		func() error { return svc.Create(ctx, &v) },
		func() error { return svc.Update(ctx, &v) },
		func() error { return svc.Delete(ctx, v.ID) },
		func() error { _, err := svc.GetList(ctx, v.DatasetKey); return err },
	} {
		var appErr *apperror.AppError
		require.ErrorAs(t, operation(), &appErr)
		require.Equal(t, apperror.CodeForbidden, appErr.Code)
	}
	require.Nil(t, repo.written)
	require.Zero(t, repo.deletedVersion)
	v.DatasetKey = "unregistered"
	ctx = corectx.WithUser(ctx, &corectx.UserContext{UserID: owner.String(), IsAdmin: true})
	require.Error(t, svc.Create(ctx, &v), "even administrators cannot create configurations for an unknown dataset")
}
