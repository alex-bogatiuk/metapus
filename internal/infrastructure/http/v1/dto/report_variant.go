package dto

import (
	"time"

	"github.com/google/uuid"

	"metapus/internal/domain/reports/variants"
)

type CreateVariantRequest struct {
	DatasetKey string                 `json:"datasetKey" binding:"required"`
	Name       string                 `json:"name" binding:"required"`
	Visibility variants.Visibility    `json:"visibility" binding:"required"`
	IsDefault  bool                   `json:"isDefault"`
	Config     variants.VariantConfig `json:"config"`
}

type UpdateVariantRequest struct {
	Name       string                 `json:"name" binding:"required"`
	Visibility variants.Visibility    `json:"visibility" binding:"required"`
	IsDefault  bool                   `json:"isDefault"`
	Config     variants.VariantConfig `json:"config"`
	Version    int                    `json:"version" binding:"required"`
}

type VariantResponse struct {
	ID         uuid.UUID              `json:"id"`
	DatasetKey string                 `json:"datasetKey"`
	Name       string                 `json:"name"`
	AuthorID   *uuid.UUID             `json:"authorId"`
	Visibility variants.Visibility    `json:"visibility"`
	IsDefault  bool                   `json:"isDefault"`
	Config     variants.VariantConfig `json:"config"`
	Version    int                    `json:"version"`
	CreatedAt  time.Time              `json:"createdAt"`
	UpdatedAt  time.Time              `json:"updatedAt"`
}

func MapVariantResponse(v *variants.ReportVariant) *VariantResponse {
	if v == nil {
		return nil
	}
	return &VariantResponse{
		ID:         v.ID,
		DatasetKey: v.DatasetKey,
		Name:       v.Name,
		AuthorID:   v.AuthorID,
		Visibility: v.Visibility,
		IsDefault:  v.IsDefault,
		Config:     v.Config,
		Version:    v.Version,
		CreatedAt:  v.CreatedAt,
		UpdatedAt:  v.UpdatedAt,
	}
}

func MapVariantListResponse(list []*variants.ReportVariant) []*VariantResponse {
	res := make([]*VariantResponse, len(list))
	for i, v := range list {
		res[i] = MapVariantResponse(v)
	}
	return res
}
