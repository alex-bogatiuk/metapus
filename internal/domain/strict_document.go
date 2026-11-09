package domain

import (
	"context"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
)

// StrictDocument marks finalized/system-owned documents whose mutation is
// locked by default and may be temporarily enabled by a system administrator.
type StrictDocument interface {
	GetID() id.ID
	IsManualEditAllowed() bool
	SetManualEditAllowed(bool)
}

// StrictDocumentRepository is the small persistence surface needed by the
// reusable protection guard. Implementations must use the current transaction.
type StrictDocumentRepository[T StrictDocument] interface {
	GetByID(ctx context.Context, docID id.ID) (T, error)
	SetManualEditAllowed(ctx context.Context, docID id.ID, allowed bool) error
}

type StrictDocumentGuard[T StrictDocument] struct {
	next           DocumentService[T]
	repo           StrictDocumentRepository[T]
	validateUpdate func(current, updated T) error
}

func ProtectStrictDocument[T StrictDocument](
	next DocumentService[T],
	repo StrictDocumentRepository[T],
	validateUpdate func(current, updated T) error,
) *StrictDocumentGuard[T] {
	return &StrictDocumentGuard[T]{next: next, repo: repo, validateUpdate: validateUpdate}
}

func (s *StrictDocumentGuard[T]) requireUnlocked(ctx context.Context, docID id.ID) (T, error) {
	var zero T
	if !appctx.IsSystemAdmin(appctx.GetUser(ctx)) {
		return zero, apperror.NewForbidden("system administrator access is required")
	}
	doc, err := s.repo.GetByID(ctx, docID)
	if err != nil {
		return zero, err
	}
	if !doc.IsManualEditAllowed() {
		return zero, apperror.NewForbidden("strict document is locked")
	}
	return doc, nil
}

func (s *StrictDocumentGuard[T]) Create(context.Context, T) error {
	return apperror.NewForbidden("strict documents can only be created by a system workflow")
}

func (s *StrictDocumentGuard[T]) GetByID(ctx context.Context, docID id.ID) (T, error) {
	return s.next.GetByID(ctx, docID)
}

func (s *StrictDocumentGuard[T]) GetByIDForUpdate(ctx context.Context, docID id.ID) (T, error) {
	return s.next.GetByIDForUpdate(ctx, docID)
}

func (s *StrictDocumentGuard[T]) Update(ctx context.Context, updated T) error {
	current, err := s.requireUnlocked(ctx, updated.GetID())
	if err != nil {
		return err
	}
	if s.validateUpdate != nil {
		if err := s.validateUpdate(current, updated); err != nil {
			return err
		}
	}
	return s.next.Update(ctx, updated)
}

func (s *StrictDocumentGuard[T]) Delete(context.Context, id.ID) error {
	return apperror.NewForbidden("physical deletion of strict documents is forbidden")
}

func (s *StrictDocumentGuard[T]) Post(ctx context.Context, docID id.ID) error {
	if _, err := s.requireUnlocked(ctx, docID); err != nil {
		return err
	}
	return s.runWithAutoLock(ctx, docID, func(txCtx context.Context) error {
		return s.next.Post(txCtx, docID)
	})
}

func (s *StrictDocumentGuard[T]) Unpost(ctx context.Context, docID id.ID) error {
	if _, err := s.requireUnlocked(ctx, docID); err != nil {
		return err
	}
	// Deliberately stays unlocked so the administrator can unpost, correct the
	// allowed accounting fields, and repost in one explicit repair flow.
	return s.next.Unpost(ctx, docID)
}

func (s *StrictDocumentGuard[T]) PostAndSave(context.Context, T) error {
	return apperror.NewForbidden("strict documents can only be created by a system workflow")
}

func (s *StrictDocumentGuard[T]) UpdateAndRepost(ctx context.Context, updated T) error {
	current, err := s.requireUnlocked(ctx, updated.GetID())
	if err != nil {
		return err
	}
	if s.validateUpdate != nil {
		if err := s.validateUpdate(current, updated); err != nil {
			return err
		}
	}
	updated.SetManualEditAllowed(false)
	return s.runWithAutoLock(ctx, updated.GetID(), func(txCtx context.Context) error {
		return s.next.UpdateAndRepost(txCtx, updated)
	})
}

func (s *StrictDocumentGuard[T]) SetDeletionMark(ctx context.Context, docID id.ID, marked bool) error {
	if _, err := s.requireUnlocked(ctx, docID); err != nil {
		return err
	}
	if !marked {
		return s.next.SetDeletionMark(ctx, docID, false)
	}
	return s.runWithAutoLock(ctx, docID, func(txCtx context.Context) error {
		return s.next.SetDeletionMark(txCtx, docID, true)
	})
}

func (s *StrictDocumentGuard[T]) List(ctx context.Context, filter ListFilter) (CursorListResult[T], error) {
	return s.next.List(ctx, filter)
}

func (s *StrictDocumentGuard[T]) ListIDs(ctx context.Context, filter ListFilter, maxIDs int) ([]id.ID, error) {
	return s.next.ListIDs(ctx, filter, maxIDs)
}

func (s *StrictDocumentGuard[T]) runWithAutoLock(ctx context.Context, docID id.ID, action func(context.Context) error) error {
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return err
	}
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := action(txCtx); err != nil {
			return err
		}
		return s.repo.SetManualEditAllowed(txCtx, docID, false)
	})
}
