package dto

import "github.com/alex-bogatiuk/metapus/internal/domain"

type FindReferencesResponse struct {
	Items []domain.FoundReference `json:"items"`
	Total int                     `json:"total"`
}

// PlanMarkedRequest selects the marked objects a deletion plan is built for;
// an empty list selects nothing.
type PlanMarkedRequest struct {
	Items []domain.MarkedObjectRef `json:"items" binding:"max=5000,dive"`
}

type DeleteMarkedRequest struct {
	Items []domain.MarkedObjectRef `json:"items" binding:"required,min=1,max=1000,dive"`
}

type UpdateRateMappingRequest struct {
	ExternalID      string `json:"externalId" binding:"required"`
	IsActive        *bool  `json:"isActive" binding:"required"`
	ExpectedVersion *int   `json:"expectedVersion" binding:"required"`
}
