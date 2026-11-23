package listview

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	corectx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
)

type viewRepositoryStub struct {
	Repository
	view            ListView
	previousDefault bool
	cleared         bool
}

func (r *viewRepositoryStub) GetByID(context.Context, uuid.UUID) (*ListView, error) {
	v := r.view
	return &v, nil
}
func (r *viewRepositoryStub) ClearDefault(_ context.Context, _ *ListView) error {
	r.cleared = true
	r.previousDefault = false
	return nil
}
func (r *viewRepositoryStub) Update(_ context.Context, v *ListView) error {
	if v.Version != r.view.Version {
		return apperror.NewConcurrentModification("ListView", v.ID)
	}
	r.view = *v
	r.view.Version++
	return nil
}

type rollbackManager struct{ repo *viewRepositoryStub }

func (m rollbackManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	before := m.repo.previousDefault
	if err := fn(ctx); err != nil {
		m.repo.previousDefault = before
		return err
	}
	return nil
}

func TestReadersCannotChangeSharedViewOrDefault(t *testing.T) {
	owner := uuid.New()
	repo := &viewRepositoryStub{view: ListView{ID: uuid.New(), EntityType: "Counterparty", Name: "Shared", Visibility: VisibilityShared, AuthorID: &owner, Version: 1}}
	ctx := corectx.WithUser(context.Background(), &corectx.UserContext{UserID: uuid.NewString()})
	svc := NewService(repo)
	v := repo.view
	require.Error(t, svc.Update(ctx, &v))
	require.Error(t, svc.Delete(ctx, v.ID))
	require.Error(t, svc.SetDefault(ctx, v.ID))
	require.False(t, repo.cleared)
}

func TestDefaultResetRollsBackOnVersionConflict(t *testing.T) {
	owner := uuid.New()
	repo := &viewRepositoryStub{view: ListView{ID: uuid.New(), EntityType: "Counterparty", Name: "Mine", Visibility: VisibilityPersonal, AuthorID: &owner, Version: 8}, previousDefault: true}
	ctx := corectx.WithUser(context.Background(), &corectx.UserContext{UserID: owner.String()})
	ctx = tenant.WithTxManager(ctx, rollbackManager{repo})
	incoming := repo.view
	incoming.Version, incoming.IsDefault = 7, true
	err := NewService(repo).Update(ctx, &incoming)
	require.True(t, apperror.IsConcurrentModification(err))
	require.True(t, repo.previousDefault, "a failed save must not remove the existing default")
	require.False(t, repo.view.IsDefault)
	incoming.Version = 8
	require.NoError(t, NewService(repo).Update(ctx, &incoming))
	require.True(t, repo.view.IsDefault)
	require.False(t, repo.previousDefault)
}
