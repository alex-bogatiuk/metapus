package entity

import (
	"metapus/internal/core/apperror"
)

// DocumentStateName identifies the lifecycle state of a document.
type DocumentStateName string

const (
	// StateDraft — document is not posted and not marked for deletion.
	// Allowed: modify, post, delete. Denied: unpost (not posted).
	StateDraft DocumentStateName = "draft"

	// StatePosted — document movements are recorded in registers.
	// Allowed: repost, unpost. Denied: modify, delete (unpost first).
	StatePosted DocumentStateName = "posted"

	// StateMarkedForDeletion — document is soft-deleted (not posted).
	// Allowed: modify, delete, unmark. Denied: post, unpost.
	StateMarkedForDeletion DocumentStateName = "marked_for_deletion"
)

// DocumentState encapsulates lifecycle rules for a specific document state.
// Each state knows which operations are permitted and returns nil (allowed)
// or a descriptive AppError (denied).
//
// Usage:
//
//	if err := doc.State().CanModify(); err != nil { return err }
type DocumentState interface {
	// Name returns the state identifier.
	Name() DocumentStateName

	// CanModify checks if document fields can be edited.
	CanModify() error

	// CanPost checks if document can be posted (or reposted).
	CanPost() error

	// CanUnpost checks if document can be unposted.
	CanUnpost() error

	// CanDelete checks if document can be hard-deleted.
	CanDelete() error
}

// Singleton state instances (stateless — safe for concurrent use).
var (
	draftState              DocumentState = &draftDocumentState{}
	postedState             DocumentState = &postedDocumentState{}
	markedForDeletionState  DocumentState = &markedForDeletionDocumentState{}
)

// ResolveDocumentState returns the appropriate state for given flags.
// Used by Document.State() and can be used in tests.
func ResolveDocumentState(posted, deletionMark bool) DocumentState {
	switch {
	case posted:
		return postedState
	case deletionMark:
		return markedForDeletionState
	default:
		return draftState
	}
}

// ---------------------------------------------------------------------------
// Draft state
// ---------------------------------------------------------------------------

type draftDocumentState struct{}

func (s *draftDocumentState) Name() DocumentStateName { return StateDraft }

func (s *draftDocumentState) CanModify() error { return nil }

func (s *draftDocumentState) CanPost() error { return nil }

func (s *draftDocumentState) CanUnpost() error {
	return apperror.NewBusinessRule(
		"DOCUMENT_NOT_POSTED",
		"Document is not posted.",
	)
}

func (s *draftDocumentState) CanDelete() error { return nil }

// ---------------------------------------------------------------------------
// Posted state
// ---------------------------------------------------------------------------

type postedDocumentState struct{}

func (s *postedDocumentState) Name() DocumentStateName { return StatePosted }

func (s *postedDocumentState) CanModify() error {
	return apperror.NewBusinessRule(
		apperror.CodeDocumentPosted,
		"Cannot modify posted document. Unpost first.",
	)
}

// CanPost returns nil — reposting a posted document is allowed.
// The posting engine detects the repost and reverses old movements automatically.
func (s *postedDocumentState) CanPost() error { return nil }

func (s *postedDocumentState) CanUnpost() error { return nil }

func (s *postedDocumentState) CanDelete() error {
	return apperror.NewBusinessRule(
		apperror.CodeDocumentPosted,
		"Cannot delete posted document. Unpost first.",
	)
}

// ---------------------------------------------------------------------------
// MarkedForDeletion state
// ---------------------------------------------------------------------------

type markedForDeletionDocumentState struct{}

func (s *markedForDeletionDocumentState) Name() DocumentStateName {
	return StateMarkedForDeletion
}

// CanModify returns nil — editing a marked document is allowed (current behaviour).
func (s *markedForDeletionDocumentState) CanModify() error { return nil }

func (s *markedForDeletionDocumentState) CanPost() error {
	return apperror.NewBusinessRule(
		apperror.CodeDocumentDeletionMarked,
		"Cannot post a deletion-marked document. Remove the deletion mark first.",
	)
}

func (s *markedForDeletionDocumentState) CanUnpost() error {
	return apperror.NewBusinessRule(
		"DOCUMENT_NOT_POSTED",
		"Document is not posted.",
	)
}

func (s *markedForDeletionDocumentState) CanDelete() error { return nil }
