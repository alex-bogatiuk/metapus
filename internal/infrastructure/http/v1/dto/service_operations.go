package dto

import "metapus/internal/domain"

type FeeScheduleListResponse struct {
	Items []FeeScheduleResponse `json:"items"`
	Total int                   `json:"total"`
}

type FindReferencesResponse struct {
	Items []domain.FoundReference `json:"items"`
	Total int                     `json:"total"`
}

type MarkedObjectsResponse struct {
	Items []domain.MarkedObject `json:"items"`
	Total int                   `json:"total"`
}

type DeleteMarkedRequest struct {
	Items []domain.DeleteMarkedRequest `json:"items" binding:"required,min=1"`
}

type UpdateRateMappingRequest struct {
	ExternalID      string `json:"externalId" binding:"required"`
	IsActive        *bool  `json:"isActive" binding:"required"`
	ExpectedVersion *int   `json:"expectedVersion" binding:"required"`
}
