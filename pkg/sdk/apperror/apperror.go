// Package apperror holds the errors the platform maps to HTTP responses.
package apperror

import (
	core "github.com/alex-bogatiuk/metapus/internal/core/apperror"
)

// AppError is an error with a code the API returns.
type AppError = core.AppError

// NewValidation is a 400 error about the request's data.
func NewValidation(message string) *AppError { return core.NewValidation(message) }

// NewNotFound is a 404 error about entity id.
func NewNotFound(entity string, id any) *AppError { return core.NewNotFound(entity, id) }

// NewConflict is a 409 error.
func NewConflict(message string) *AppError { return core.NewConflict(message) }

// NewForbidden is a 403 error.
func NewForbidden(message string) *AppError { return core.NewForbidden(message) }

const (
	// Conflict (409)
	CodeConflict  = core.CodeConflict
	CodeForbidden = core.CodeForbidden
	// Infrastructure errors (5xx)
	CodeInternal = core.CodeInternal
	// Not found (404)
	CodeNotFound = core.CodeNotFound
	// Validation errors (400)
	CodeValidation = core.CodeValidation
)

// AsAppError extracts AppError from error chain
func AsAppError(err error) (*core.AppError, bool) { return core.AsAppError(err) }

// GetHTTPStatus returns appropriate HTTP status for any error
func GetHTTPStatus(err error) int { return core.GetHTTPStatus(err) }

// IsForbidden checks if error is CodeForbidden
func IsForbidden(err error) bool { return core.IsForbidden(err) }

// IsNotFound checks if error is CodeNotFound
func IsNotFound(err error) bool { return core.IsNotFound(err) }

// NewBusinessRule creates a business rule violation error (422)
func NewBusinessRule(code string, message string) *core.AppError {
	return core.NewBusinessRule(code, message)
}

// NewConcurrentModification creates an optimistic locking error
func NewConcurrentModification(p0 string, p1 any) *core.AppError {
	return core.NewConcurrentModification(p0, p1)
}

// NewIdempotencyConflict creates error when operation is already in progress
func NewIdempotencyConflict(key string) *core.AppError { return core.NewIdempotencyConflict(key) }

// NewIdempotencyKeyRequired reports a missing durable command identity.
func NewIdempotencyKeyRequired() *core.AppError { return core.NewIdempotencyKeyRequired() }

// NewIdempotencyMismatch is returned when the same idempotency key is reused for
// a different request (different user/operation/body hash).
func NewIdempotencyMismatch(key string) *core.AppError { return core.NewIdempotencyMismatch(key) }

// NewInternal creates an internal server error (hides details from client)
func NewInternal(err error) *core.AppError { return core.NewInternal(err) }

// NewInvalidRequest creates a public validation error while retaining the
// parser/validator cause exclusively for structured server-side logging.
func NewInvalidRequest(cause error) *core.AppError { return core.NewInvalidRequest(cause) }

// NewMethodNotAllowed creates a method not allowed error (405)
func NewMethodNotAllowed(message string) *core.AppError { return core.NewMethodNotAllowed(message) }

// NewStepUpRequired creates an authorization error that asks the client to perform step-up.
func NewStepUpRequired(actionCode string) *core.AppError { return core.NewStepUpRequired(actionCode) }

// NewUnauthorized creates an authentication error (401)
func NewUnauthorized(message string) *core.AppError { return core.NewUnauthorized(message) }
