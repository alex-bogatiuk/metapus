package dto

import (
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/domain/printing"
)

type BatchActionRequest struct {
	IDs    []string `json:"ids" binding:"required,min=1,max=500"`
	Action string   `json:"action" binding:"required,oneof=post unpost setDeletionMark clearDeletionMark"`
}
type BatchActionResult struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Code    string `json:"code,omitempty"`
	Error   string `json:"error,omitempty"`
}
type BatchActionResponse struct {
	Results []BatchActionResult `json:"results"`
	Total   int                 `json:"total"`
	Success int                 `json:"success"`
	Failed  int                 `json:"failed"`
}
type BatchActionByFilterRequest struct {
	Filter         json.RawMessage `json:"filter"` // JSON-encoded []filter.Item
	Action         string          `json:"action" binding:"required,oneof=post unpost setDeletionMark clearDeletionMark"`
	ExcludeIDs     []string        `json:"excludeIds"`     // IDs to skip (user manually unchecked)
	IncludeDeleted bool            `json:"includeDeleted"` // match current list view
	OrderBy        string          `json:"orderBy"`        // current sort (for filter consistency)
	Search         string          `json:"search"`         // current search text
}
type DocumentMovementsResponse struct {
	Movements []entity.DocumentMovement `json:"movements"`
	Count     int                       `json:"count"`
}
type RelatedDocumentsResponse = domain.RelatedDocumentsResult
type PrintFormsResponse = []printing.PrintFormSummary
