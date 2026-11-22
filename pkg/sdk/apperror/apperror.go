// Package apperror holds the errors the platform maps to HTTP responses.
package apperror

import core "metapus/internal/core/apperror"

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
