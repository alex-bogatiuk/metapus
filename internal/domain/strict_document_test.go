package domain

import (
	"context"
	"testing"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"

	"github.com/stretchr/testify/require"
)

type strictTestDocument struct {
	id       id.ID
	unlocked bool
	value    string
}

func (d *strictTestDocument) GetID() id.ID                    { return d.id }
func (d *strictTestDocument) IsManualEditAllowed() bool       { return d.unlocked }
func (d *strictTestDocument) SetManualEditAllowed(value bool) { d.unlocked = value }

type strictTestRepo struct{ doc *strictTestDocument }

func (r *strictTestRepo) GetByID(context.Context, id.ID) (*strictTestDocument, error) {
	copy := *r.doc
	return &copy, nil
}

func (r *strictTestRepo) SetManualEditAllowed(_ context.Context, _ id.ID, allowed bool) error {
	r.doc.unlocked = allowed
	return nil
}

type strictTestService struct {
	repo          *strictTestRepo
	postCalls     int
	unpostCalls   int
	deletionCalls int
}

func (s *strictTestService) Create(context.Context, *strictTestDocument) error { return nil }
func (s *strictTestService) GetByID(ctx context.Context, docID id.ID) (*strictTestDocument, error) {
	return s.repo.GetByID(ctx, docID)
}
func (s *strictTestService) GetByIDForUpdate(ctx context.Context, docID id.ID) (*strictTestDocument, error) {
	return s.repo.GetByID(ctx, docID)
}
func (s *strictTestService) Update(_ context.Context, doc *strictTestDocument) error {
	copy := *doc
	s.repo.doc = &copy
	return nil
}
func (s *strictTestService) Delete(context.Context, id.ID) error { return nil }
func (s *strictTestService) Post(context.Context, id.ID) error {
	s.postCalls++
	return nil
}
func (s *strictTestService) Unpost(context.Context, id.ID) error {
	s.unpostCalls++
	return nil
}
func (s *strictTestService) PostAndSave(context.Context, *strictTestDocument) error { return nil }
func (s *strictTestService) UpdateAndRepost(ctx context.Context, doc *strictTestDocument) error {
	if err := s.Update(ctx, doc); err != nil {
		return err
	}
	s.postCalls++
	return nil
}
func (s *strictTestService) SetDeletionMark(context.Context, id.ID, bool) error {
	s.deletionCalls++
	return nil
}
func (s *strictTestService) List(context.Context, ListFilter) (CursorListResult[*strictTestDocument], error) {
	return CursorListResult[*strictTestDocument]{}, nil
}
func (s *strictTestService) ListIDs(context.Context, ListFilter, int) ([]id.ID, error) {
	return nil, nil
}

type strictTestTxManager struct{}

func (strictTestTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func TestStrictDocumentLifecycleUnlockUnpostEditPostAutoLock(t *testing.T) {
	doc := &strictTestDocument{id: id.New(), unlocked: true, value: "original"}
	repo := &strictTestRepo{doc: doc}
	raw := &strictTestService{repo: repo}
	guard := ProtectStrictDocument[*strictTestDocument](raw, repo, func(current, updated *strictTestDocument) error {
		require.Equal(t, current.id, updated.id)
		return nil
	})
	ctx := tenant.WithTxManager(context.Background(), strictTestTxManager{})
	ctx = appctx.WithUser(ctx, &appctx.UserContext{Roles: []string{"admin"}})

	require.NoError(t, guard.Unpost(ctx, doc.id))
	require.True(t, repo.doc.unlocked, "unpost must keep the emergency edit session open")
	require.Equal(t, 1, raw.unpostCalls)

	updated, err := guard.GetByID(ctx, doc.id)
	require.NoError(t, err)
	updated.value = "corrected"
	require.NoError(t, guard.Update(ctx, updated))
	require.Equal(t, "corrected", repo.doc.value)
	require.True(t, repo.doc.unlocked)

	require.NoError(t, guard.Post(ctx, doc.id))
	require.Equal(t, 1, raw.postCalls)
	require.False(t, repo.doc.unlocked, "successful posting must restore the strict lock")
}

func TestStrictDocumentRejectsDirectMutationForNonSystemAdmins(t *testing.T) {
	for _, roles := range [][]string{{"user"}, {"security_admin"}} {
		t.Run(roles[0], func(t *testing.T) {
			doc := &strictTestDocument{id: id.New(), unlocked: true}
			repo := &strictTestRepo{doc: doc}
			raw := &strictTestService{repo: repo}
			guard := ProtectStrictDocument[*strictTestDocument](raw, repo, nil)
			ctx := appctx.WithUser(context.Background(), &appctx.UserContext{Roles: roles})

			require.Error(t, guard.Update(ctx, doc))
			require.Error(t, guard.Unpost(ctx, doc.id))
			require.Error(t, guard.SetDeletionMark(ctx, doc.id, true))
			require.Zero(t, raw.unpostCalls)
			require.Zero(t, raw.deletionCalls)
		})
	}
}

func TestStrictDocumentDeletionMarkAutoLocks(t *testing.T) {
	doc := &strictTestDocument{id: id.New(), unlocked: true}
	repo := &strictTestRepo{doc: doc}
	raw := &strictTestService{repo: repo}
	guard := ProtectStrictDocument[*strictTestDocument](raw, repo, nil)
	ctx := tenant.WithTxManager(context.Background(), strictTestTxManager{})
	ctx = appctx.WithUser(ctx, &appctx.UserContext{IsAdmin: true})

	require.NoError(t, guard.SetDeletionMark(ctx, doc.id, true))
	require.Equal(t, 1, raw.deletionCalls)
	require.False(t, repo.doc.unlocked)
	require.Error(t, guard.Create(ctx, doc))
	require.Error(t, guard.Delete(ctx, doc.id))
}
