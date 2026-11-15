package dto

import (
	"metapus/internal/core/entity"
	"metapus/internal/domain/catalogs/rate_source"
)

// --- Request DTOs ---

// CreateRateSourceRequest is the request body for creating a rate source.
type CreateRateSourceRequest struct {
	IntegrationPoolID *string           `json:"integrationPoolId"`
	Code              string            `json:"code"`
	Name              string            `json:"name" binding:"required"`
	SourceType        string            `json:"sourceType" binding:"required"`
	RateLimitRPM      int               `json:"rateLimitRpm"`
	APIKey            *string           `json:"apiKey"`
	IsActive          bool              `json:"isActive"`
	ParentID          *string           `json:"parentId"`
	IsFolder          bool              `json:"isFolder"`
	Attributes        entity.Attributes `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateRateSourceRequest) ToEntity() *rate_source.RateSource {
	rs := rate_source.NewRateSource(r.Code, r.Name, r.SourceType)
	if r.RateLimitRPM > 0 {
		rs.RateLimitRPM = r.RateLimitRPM
	}
	if r.APIKey != nil {
		rs.SetAPIKey(*r.APIKey)
	}
	rs.IntegrationPoolID = StringPtrToIDPtr(r.IntegrationPoolID)
	rs.IsActive = r.IsActive
	rs.ParentID = StringPtrToIDPtr(r.ParentID)
	rs.IsFolder = r.IsFolder
	rs.Attributes = r.Attributes
	return rs
}

// UpdateRateSourceRequest is the request body for updating a rate source.
type UpdateRateSourceRequest struct {
	IntegrationPoolID *string `json:"integrationPoolId"`
	Code              string  `json:"code"`
	Name              string  `json:"name" binding:"required"`
	SourceType        string  `json:"sourceType" binding:"required"`
	RateLimitRPM      int     `json:"rateLimitRpm"`
	// APIKey is write-only. nil means "leave unchanged"; an empty string clears it.
	APIKey     *string           `json:"apiKey"`
	IsActive   bool              `json:"isActive"`
	ParentID   *string           `json:"parentId"`
	IsFolder   bool              `json:"isFolder"`
	Attributes entity.Attributes `json:"attributes"`
	Version    int               `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateRateSourceRequest) ApplyTo(rs *rate_source.RateSource) {
	rs.Code = r.Code
	rs.Name = r.Name
	rs.SourceType = rate_source.SourceType(r.SourceType)
	rs.RateLimitRPM = r.RateLimitRPM
	if r.APIKey != nil {
		rs.SetAPIKey(*r.APIKey)
	}
	if r.IntegrationPoolID != nil {
		rs.IntegrationPoolID = StringPtrToIDPtr(r.IntegrationPoolID)
	}
	rs.IsActive = r.IsActive
	rs.ParentID = StringPtrToIDPtr(r.ParentID)
	rs.IsFolder = r.IsFolder
	rs.Attributes = r.Attributes
	rs.Version = r.Version
}

// --- Response DTOs ---

// RateSourceResponse is the response body for a rate source.
type RateSourceResponse struct {
	IntegrationPoolID *string           `json:"integrationPoolId,omitempty"`
	ID                string            `json:"id"`
	Code              string            `json:"code"`
	Name              string            `json:"name"`
	SourceType        string            `json:"sourceType"`
	BaseURL           *string           `json:"baseUrl,omitempty"`
	RateLimitRPM      int               `json:"rateLimitRpm"`
	APIKeyConfigured  bool              `json:"apiKeyConfigured"`
	Priority          int               `json:"priority"`
	IsActive          bool              `json:"isActive"`
	ParentID          *string           `json:"parentId,omitempty"`
	IsFolder          bool              `json:"isFolder"`
	DeletionMark      bool              `json:"deletionMark"`
	Version           int               `json:"version"`
	Attributes        entity.Attributes `json:"attributes,omitempty"`
}

// FromRateSource creates response DTO from domain entity.
func FromRateSource(rs *rate_source.RateSource) *RateSourceResponse {
	return &RateSourceResponse{
		IntegrationPoolID: IDToStringPtr(rs.IntegrationPoolID),
		ID:                rs.ID.String(),
		Code:              rs.Code,
		Name:              rs.Name,
		SourceType:        string(rs.SourceType),
		BaseURL:           rs.BaseURL,
		RateLimitRPM:      rs.RateLimitRPM,
		APIKeyConfigured:  rs.APIKey != nil,
		Priority:          rs.Priority,
		IsActive:          rs.IsActive,
		ParentID:          IDToStringPtr(rs.ParentID),
		IsFolder:          rs.IsFolder,
		DeletionMark:      rs.DeletionMark,
		Version:           rs.Version,
		Attributes:        rs.Attributes,
	}
}
