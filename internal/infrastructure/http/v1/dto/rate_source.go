package dto

import (
	"metapus/internal/core/entity"
	"metapus/internal/domain/catalogs/rate_source"
)

// --- Request DTOs ---

// CreateRateSourceRequest is the request body for creating a rate source.
type CreateRateSourceRequest struct {
	Code         string            `json:"code"`
	Name         string            `json:"name" binding:"required"`
	SourceType   string            `json:"sourceType" binding:"required"`
	BaseURL      *string           `json:"baseUrl"`
	RateLimitRPM int               `json:"rateLimitRpm"`
	Priority     int               `json:"priority"`
	IsActive     bool              `json:"isActive"`
	ParentID     *string           `json:"parentId"`
	IsFolder     bool              `json:"isFolder"`
	Attributes   entity.Attributes `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateRateSourceRequest) ToEntity() *rate_source.RateSource {
	rs := rate_source.NewRateSource(r.Code, r.Name, r.SourceType)
	rs.BaseURL = r.BaseURL
	if r.RateLimitRPM > 0 {
		rs.RateLimitRPM = r.RateLimitRPM
	}
	if r.Priority > 0 {
		rs.Priority = r.Priority
	}
	rs.IsActive = r.IsActive
	rs.ParentID = stringPtrToIDPtr(r.ParentID)
	rs.IsFolder = r.IsFolder
	rs.Attributes = r.Attributes
	return rs
}

// UpdateRateSourceRequest is the request body for updating a rate source.
type UpdateRateSourceRequest struct {
	Code         string            `json:"code"`
	Name         string            `json:"name" binding:"required"`
	SourceType   string            `json:"sourceType" binding:"required"`
	BaseURL      *string           `json:"baseUrl"`
	RateLimitRPM int               `json:"rateLimitRpm"`
	Priority     int               `json:"priority"`
	IsActive     bool              `json:"isActive"`
	ParentID     *string           `json:"parentId"`
	IsFolder     bool              `json:"isFolder"`
	Attributes   entity.Attributes `json:"attributes"`
	Version      int               `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateRateSourceRequest) ApplyTo(rs *rate_source.RateSource) {
	rs.Code = r.Code
	rs.Name = r.Name
	rs.SourceType = r.SourceType
	rs.BaseURL = r.BaseURL
	rs.RateLimitRPM = r.RateLimitRPM
	rs.Priority = r.Priority
	rs.IsActive = r.IsActive
	rs.ParentID = stringPtrToIDPtr(r.ParentID)
	rs.IsFolder = r.IsFolder
	rs.Attributes = r.Attributes
	rs.Version = r.Version
}

// --- Response DTOs ---

// RateSourceResponse is the response body for a rate source.
type RateSourceResponse struct {
	ID           string            `json:"id"`
	Code         string            `json:"code"`
	Name         string            `json:"name"`
	SourceType   string            `json:"sourceType"`
	BaseURL      *string           `json:"baseUrl,omitempty"`
	RateLimitRPM int               `json:"rateLimitRpm"`
	Priority     int               `json:"priority"`
	IsActive     bool              `json:"isActive"`
	ParentID     *string           `json:"parentId,omitempty"`
	IsFolder     bool              `json:"isFolder"`
	DeletionMark bool              `json:"deletionMark"`
	Version      int               `json:"version"`
	Attributes   entity.Attributes `json:"attributes,omitempty"`
}

// FromRateSource creates response DTO from domain entity.
func FromRateSource(rs *rate_source.RateSource) *RateSourceResponse {
	return &RateSourceResponse{
		ID:           rs.ID.String(),
		Code:         rs.Code,
		Name:         rs.Name,
		SourceType:   rs.SourceType,
		BaseURL:      rs.BaseURL,
		RateLimitRPM: rs.RateLimitRPM,
		Priority:     rs.Priority,
		IsActive:     rs.IsActive,
		ParentID:     idToStringPtr(rs.ParentID),
		IsFolder:     rs.IsFolder,
		DeletionMark: rs.DeletionMark,
		Version:      rs.Version,
		Attributes:   rs.Attributes,
	}
}
