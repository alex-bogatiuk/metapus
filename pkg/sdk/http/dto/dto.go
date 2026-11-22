// Package dto holds the platform's shared HTTP response shapes.
package dto

import (
	"metapus/internal/core/id"
	core "metapus/internal/infrastructure/http/v1/dto"
)

type (
	// BaseResponse contains common response fields.
	BaseResponse              = core.BaseResponse
	DocumentMovementsResponse = core.DocumentMovementsResponse
	// DocumentResponse contains document fields.
	DocumentResponse = core.DocumentResponse
	// StatusResponse acknowledges a command whose only result is its status.
	StatusResponse = core.StatusResponse
)

// IDToStringPtr converts *id.ID to *string for JSON response.
func IDToStringPtr(v *id.ID) *string { return core.IDToStringPtr(v) }

// StringPtrToIDPtr converts *string to *id.ID for entity mapping.
func StringPtrToIDPtr(s *string) *id.ID { return core.StringPtrToIDPtr(s) }
