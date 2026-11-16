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
	CodeDocumentPosted         = "DOCUMENT_ALREADY_POSTED"
	CodeDocumentDeletionMarked = "DOCUMENT_DELETION_MARKED"
	CodePeriodClosed           = "PERIOD_CLOSED"
	CodeConcurrentModification = "CONCURRENT_MODIFICATION"

	// Authorization errors (401, 403)
	CodeUnauthorized          = "UNAUTHORIZED"
	CodeForbidden             = "FORBIDDEN"
	CodeTokenStale            = "TOKEN_STALE"
	CodeSessionRevoked        = "SESSION_REVOKED"
	CodeMFARequired           = "MFA_REQUIRED"
	CodeMFAEnrollmentRequired = "MFA_ENROLLMENT_REQUIRED"
	CodeMFADenied             = "MFA_DENIED"
	CodeMFALocked             = "MFA_LOCKED"
	CodeStepUpRequired        = "STEP_UP_REQUIRED"
	CodeAdminIPDenied         = "ADMIN_IP_DENIED"

	ReasonMFANotEnrolled = "MFA_NOT_ENROLLED"

	// Not found (404)
	CodeNotFound = "NOT_FOUND"

	// Conflict (409)
	CodeConflict    = "CONFLICT"
	CodeDuplicate   = "DUPLICATE_ENTRY"
	CodeIdempotency = "IDEMPOTENCY_CONFLICT"

	// Method Not Allowed (405)
	CodeMethodNotAllowed = "METHOD_NOT_ALLOWED"
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

// NewInvalidRequest creates a public validation error while retaining the
// parser/validator cause exclusively for structured server-side logging.
func NewInvalidRequest(cause error) *AppError {
	return NewValidation("invalid request body").WithCause(cause)
}

// NewInvalidQuery creates a public validation error while retaining the
// parser/validator cause exclusively for structured server-side logging.
func NewInvalidQuery(cause error) *AppError {
	return NewValidation("invalid query parameters").WithCause(cause)
}

// NewNotFound creates a not found error (404)
func NewNotFound(entity string, id any) *AppError {
	return &AppError{
		Code:       CodeNotFound,
		Message:    "Запись не найдена. Возможно, она была удалена.",
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

// NewMethodNotAllowed creates a method not allowed error (405)
func NewMethodNotAllowed(message string) *AppError {
	return &AppError{
		Code:       CodeMethodNotAllowed,
		Message:    message,
		HTTPStatus: http.StatusMethodNotAllowed,
	}
}

// NewConcurrentModification creates an optimistic locking error
func NewConcurrentModification(entity string, id any) *AppError {
	return &AppError{
		Code:       CodeConcurrentModification,
		Message:    "Документ изменен другим пользователем. Обновите страницу.",
		HTTPStatus: http.StatusConflict,
		Details:    map[string]any{"entity": entity, "id": id},
	}
}

// NewInternal creates an internal server error (hides details from client)
func NewInternal(err error) *AppError {
	return &AppError{
		Code:       CodeInternal,
		Message:    "Внутренняя ошибка сервера. Обратитесь к администратору.",
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

// NewTokenStale creates an error for access tokens with stale auth epochs.
func NewTokenStale() *AppError {
	return &AppError{
		Code:       CodeTokenStale,
		Message:    "access token is stale",
		HTTPStatus: http.StatusUnauthorized,
	}
}

// NewSessionRevoked creates an error for revoked or expired auth sessions.
func NewSessionRevoked() *AppError {
	return &AppError{
		Code:       CodeSessionRevoked,
		Message:    "session revoked",
		HTTPStatus: http.StatusUnauthorized,
	}
}

// NewMFARequired creates an authorization error for routes that require MFA.
func NewMFARequired(message string) *AppError {
	return &AppError{
		Code:       CodeMFARequired,
		Message:    message,
		HTTPStatus: http.StatusForbidden,
	}
}

// NewMFAEnrollmentRequired creates an authorization error for routes that require MFA enrollment.
func NewMFAEnrollmentRequired(message string) *AppError {
	return &AppError{
		Code:       CodeMFAEnrollmentRequired,
		Message:    message,
		HTTPStatus: http.StatusForbidden,
	}
}

// NewMFADenied creates an authentication error for invalid MFA proof.
func NewMFADenied(message string) *AppError {
	return &AppError{
		Code:       CodeMFADenied,
		Message:    message,
		HTTPStatus: http.StatusUnauthorized,
	}
}

// NewMFANotEnrolled creates a stable MFA denial for clients that need to
// branch into enrollment UI without parsing human-readable copy.
func NewMFANotEnrolled() *AppError {
	return NewMFADenied("TOTP is not enrolled").
		WithDetail("reason", ReasonMFANotEnrolled)
}

// NewMFALocked creates an authentication error for temporarily locked MFA verification.
func NewMFALocked(message string) *AppError {
	return &AppError{
		Code:       CodeMFALocked,
		Message:    message,
		HTTPStatus: http.StatusForbidden,
	}
}

// NewStepUpRequired creates an authorization error that asks the client to perform step-up.
func NewStepUpRequired(actionCode string) *AppError {
	return &AppError{
		Code:       CodeStepUpRequired,
		Message:    "fresh step-up confirmation is required",
		HTTPStatus: http.StatusForbidden,
		Details:    map[string]any{"actionCode": actionCode},
	}
}

// NewAdminIPDenied creates an authorization error for admin CIDR enforcement.
func NewAdminIPDenied() *AppError {
	return &AppError{
		Code:       CodeAdminIPDenied,
		Message:    "admin access is not allowed from this IP address",
		HTTPStatus: http.StatusForbidden,
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
		Code:       "IDEMPOTENCY_IN_PROGRESS",
		Message:    "Операция уже выполняется или завершена.",
		HTTPStatus: http.StatusConflict,
		Details:    map[string]any{"idempotency_key": key},
	}
}

// NewIdempotencyMismatch is returned when the same idempotency key is reused for
// a different request (different user/operation/body hash).
func NewIdempotencyMismatch(key string) *AppError {
	return &AppError{
		Code:       "IDEMPOTENCY_KEY_REUSED",
		Message:    "Ключ идемпотентности не совпадает.",
		HTTPStatus: http.StatusConflict,
		Details:    map[string]any{"idempotency_key": key},
	}
}

// NewIdempotencyKeyRequired reports a missing durable command identity.
func NewIdempotencyKeyRequired() *AppError {
	return &AppError{
		Code:       "IDEMPOTENCY_KEY_REQUIRED",
		Message:    "Idempotency-Key is required for this operation.",
		HTTPStatus: http.StatusBadRequest,
	}
}

// NewPeriodClosed creates error when trying to modify closed period
func NewPeriodClosed(period string) *AppError {
	return &AppError{
		Code:       CodePeriodClosed,
		Message:    fmt.Sprintf("Период %s закрыт. Изменение данных запрещено.", period),
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
		Message:    fmt.Sprintf("Запись с таким значением поля '%s' уже существует.", field),
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
	if appErr, ok := errors.AsType[*AppError](err); ok {
		return appErr, true
	}
	return nil, false
}

// Public converts an arbitrary error to an error that is safe to serialize.
// Existing AppErrors already define their public contract; every other error
// is treated as an internal cause and never copied into message/details.
func Public(err error) *AppError {
	if appErr, ok := AsAppError(err); ok {
		return appErr
	}
	return NewInternal(err)
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

// IsForbidden checks if error is CodeForbidden
func IsForbidden(err error) bool {
	if appErr, ok := AsAppError(err); ok {
		return appErr.Code == CodeForbidden
	}
	return false
}
