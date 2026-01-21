// Package apperror provides structured error handling following RFC 7807 Problem Details.
// All business errors must use AppError for consistent API responses.
package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

// Error codes following domain-driven design
const (
	// Infrastructure errors (5xx)
	CodeInternal = "INTERNAL_ERROR"
	CodeDatabase = "DATABASE_ERROR"
	CodeTimeout  = "TIMEOUT_ERROR"

	// Validation errors (400)
	CodeValidation   = "VALIDATION_ERROR"
	CodeInvalidInput = "INVALID_INPUT"

	// Business rule violations (422)
	CodeBusinessRule           = "BUSINESS_RULE_VIOLATION"
	CodeInsufficientStock      = "INSUFFICIENT_STOCK"
	CodeDocumentPosted         = "DOCUMENT_ALREADY_POSTED"
	CodePeriodClosed           = "PERIOD_CLOSED"
	CodeConcurrentModification = "CONCURRENT_MODIFICATION"

	// Authorization errors (401, 403)
	CodeUnauthorized = "UNAUTHORIZED"
	CodeForbidden    = "FORBIDDEN"

	// Not found (404)
	CodeNotFound = "NOT_FOUND"

	// Conflict (409)
	CodeConflict    = "CONFLICT"
	CodeDuplicate   = "DUPLICATE_ENTRY"
	CodeIdempotency = "IDEMPOTENCY_CONFLICT"
)

// AppError is the standard error type for the platform.
// It implements error interface and provides structured details for API responses.
type AppError struct {
	// Code is a machine-readable error identifier
	Code string `json:"code"`

	// Message is a human-readable error description
	Message string `json:"message"`

	// Details contains additional context (field errors, quantities, etc.)
	Details map[string]any `json:"details,omitempty"`

	// HTTPStatus is the suggested HTTP status code
	HTTPStatus int `json:"-"`

	// Err is the underlying error (not exposed in JSON)
	Err error `json:"-"`
}

// Error implements error interface
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying error for errors.Is/As support
func (e *AppError) Unwrap() error {
	return e.Err
}

// WithDetail adds a key-value pair to error details
func (e *AppError) WithDetail(key string, value any) *AppError {
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	e.Details[key] = value
	return e
}

// WithCause sets the underlying error
func (e *AppError) WithCause(err error) *AppError {
	e.Err = err
	return e
}

// --- Factory functions for common errors ---

// NewValidation creates a validation error (400)
func NewValidation(message string) *AppError {
	return &AppError{
		Code:       CodeValidation,
		Message:    message,
		HTTPStatus: http.StatusBadRequest,
	}
}

// NewNotFound creates a not found error (404)
func NewNotFound(entity string, id any) *AppError {
	return &AppError{
		Code:       CodeNotFound,
		Message:    fmt.Sprintf("%s not found", entity),
		HTTPStatus: http.StatusNotFound,
		Details:    map[string]any{"entity": entity, "id": id},
	}
}

// NewBusinessRule creates a business rule violation error (422)
func NewBusinessRule(code, message string) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: http.StatusUnprocessableEntity,
	}
}

// NewInsufficientStock creates a stock shortage error
func NewInsufficientStock(productID string, requested, available float64) *AppError {
	return &AppError{
		Code:       CodeInsufficientStock,
		Message:    "Insufficient stock",
		HTTPStatus: http.StatusUnprocessableEntity,
		Details: map[string]any{
			"product_id": productID,
			"requested":  requested,
			"available":  available,
		},
	}
}

// NewConcurrentModification creates an optimistic locking error
func NewConcurrentModification(entity string, id any) *AppError {
	return &AppError{
		Code:       CodeConcurrentModification,
		Message:    "Record was modified by another user. Please refresh and try again.",
		HTTPStatus: http.StatusConflict,
		Details:    map[string]any{"entity": entity, "id": id},
	}
}

// NewInternal creates an internal server error (hides details from client)
func NewInternal(err error) *AppError {
	return &AppError{
		Code:       CodeInternal,
		Message:    "Internal server error",
		HTTPStatus: http.StatusInternalServerError,
		Err:        err,
	}
}

// NewUnauthorized creates an authentication error (401)
func NewUnauthorized(message string) *AppError {
	return &AppError{
		Code:       CodeUnauthorized,
		Message:    message,
		HTTPStatus: http.StatusUnauthorized,
	}
}

// NewForbidden creates an authorization error (403)
func NewForbidden(message string) *AppError {
	return &AppError{
		Code:       CodeForbidden,
		Message:    message,
		HTTPStatus: http.StatusForbidden,
	}
}

// NewIdempotencyConflict creates error when operation is already in progress
func NewIdempotencyConflict(key string) *AppError {
	return &AppError{
		Code:       CodeIdempotency,
		Message:    "Operation already in progress or completed",
		HTTPStatus: http.StatusConflict,
		Details:    map[string]any{"idempotency_key": key},
	}
}

// NewIdempotencyMismatch is returned when the same idempotency key is reused for
// a different request (different user/operation/body hash).
func NewIdempotencyMismatch(key string) *AppError {
	return &AppError{
		Code:       CodeIdempotency,
		Message:    "Idempotency key mismatch",
		HTTPStatus: http.StatusConflict,
		Details:    map[string]any{"idempotency_key": key},
	}
}

// NewPeriodClosed creates error when trying to modify closed period
func NewPeriodClosed(period string) *AppError {
	return &AppError{
		Code:       CodePeriodClosed,
		Message:    fmt.Sprintf("Period %s is closed for modifications", period),
		HTTPStatus: http.StatusUnprocessableEntity,
		Details:    map[string]any{"period": period},
	}
}

// NewConflict creates a conflict error (409)
func NewConflict(message string) *AppError {
	return &AppError{
		Code:       CodeConflict,
		Message:    message,
		HTTPStatus: http.StatusConflict,
	}
}

// NewDuplicate creates a duplicate entry error (409)
func NewDuplicate(entity, field, value string) *AppError {
	return &AppError{
		Code:       CodeDuplicate,
		Message:    fmt.Sprintf("%s with this %s already exists", entity, field),
		HTTPStatus: http.StatusConflict,
		Details:    map[string]any{"entity": entity, "field": field, "value": value},
	}
}

// --- Helper functions ---

// IsAppError checks if error is AppError
func IsAppError(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr)
}

// AsAppError extracts AppError from error chain
func AsAppError(err error) (*AppError, bool) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

// GetHTTPStatus returns appropriate HTTP status for any error
func GetHTTPStatus(err error) int {
	if appErr, ok := AsAppError(err); ok {
		return appErr.HTTPStatus
	}
	return http.StatusInternalServerError
}

// IsNotFound checks if error is CodeNotFound
func IsNotFound(err error) bool {
	if appErr, ok := AsAppError(err); ok {
		return appErr.Code == CodeNotFound
	}
	return false
}

// IsConcurrentModification checks if error is CodeConcurrentModification
func IsConcurrentModification(err error) bool {
	if appErr, ok := AsAppError(err); ok {
		return appErr.Code == CodeConcurrentModification
	}
	return false
}
