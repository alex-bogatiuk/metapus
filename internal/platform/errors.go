package platform

// Re-export error types for client extensions.
// Extensions should use platform.NewValidation() etc. instead of
// importing "github.com/alex-bogatiuk/metapus/internal/core/apperror" directly.

import "github.com/alex-bogatiuk/metapus/internal/core/apperror"

// ── Error constructors ──────────────────────────────────────────────────

// NewValidation creates a validation error (HTTP 422).
var NewValidation = apperror.NewValidation

// NewConflict creates a conflict error (HTTP 409).
var NewConflict = apperror.NewConflict

// NewNotFound creates a not-found error (HTTP 404).
var NewNotFound = apperror.NewNotFound

// ── Error types ─────────────────────────────────────────────────────────

// AppError is the standard application error type.
type AppError = apperror.AppError
