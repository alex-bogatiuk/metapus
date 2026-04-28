package dto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"metapus/internal/domain/listview"
)

// CreateListViewRequest is the request body for creating a new list view.
type CreateListViewRequest struct {
	EntityType string              `json:"entityType" binding:"required"`
	Name       string              `json:"name" binding:"required"`
	Visibility listview.Visibility `json:"visibility" binding:"required"`
	IsDefault  bool                `json:"isDefault"`
	Config     listview.Config     `json:"config"`
}

// UpdateListViewRequest is the request body for updating a list view.
type UpdateListViewRequest struct {
	Name       string              `json:"name" binding:"required"`
	Visibility listview.Visibility `json:"visibility" binding:"required"`
	IsDefault  bool                `json:"isDefault"`
	Config     listview.Config     `json:"config"`
	Version    int                 `json:"version" binding:"required"`
}

// ListViewResponse is the response DTO for a list view.
type ListViewResponse struct {
	ID         uuid.UUID           `json:"id"`
	EntityType string              `json:"entityType"`
	Name       string              `json:"name"`
	AuthorID   *uuid.UUID          `json:"authorId"`
	Visibility listview.Visibility `json:"visibility"`
	IsDefault  bool                `json:"isDefault"`
	SortOrder  int                 `json:"sortOrder"`
	Config     listview.Config     `json:"config"`
	Version    int                 `json:"version"`
	CreatedAt  time.Time           `json:"createdAt"`
	UpdatedAt  time.Time           `json:"updatedAt"`
}

// MapListViewResponse converts a domain ListView to a response DTO.
func MapListViewResponse(v *listview.ListView) *ListViewResponse {
	if v == nil {
		return nil
	}
	cfg := v.Config
	// Ensure non-nil slices in JSON output.
	if cfg.Columns == nil {
		cfg.Columns = make([]string, 0)
	}
	if cfg.Filters == nil {
		cfg.Filters = json.RawMessage("{}")
	}
	return &ListViewResponse{
		ID:         v.ID,
		EntityType: v.EntityType,
		Name:       v.Name,
		AuthorID:   v.AuthorID,
		Visibility: v.Visibility,
		IsDefault:  v.IsDefault,
		SortOrder:  v.SortOrder,
		Config:     cfg,
		Version:    v.Version,
		CreatedAt:  v.CreatedAt,
		UpdatedAt:  v.UpdatedAt,
	}
}

// MapListViewListResponse converts a slice of domain ListViews to response DTOs.
func MapListViewListResponse(list []*listview.ListView) []*ListViewResponse {
	res := make([]*ListViewResponse, len(list))
	for i, v := range list {
		res[i] = MapListViewResponse(v)
	}
	return res
}
