// Package dto provides Data Transfer Objects for API requests/responses.
package dto

import (
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// --- Pagination ---

// PaginationRequest contains pagination parameters.
type PaginationRequest struct {
	Page     int `form:"page" binding:"min=1"`
	PageSize int `form:"pageSize" binding:"min=1,max=100"`
}

// Defaults sets default pagination values.
func (p *PaginationRequest) Defaults() {
	if p.Page == 0 {
		p.Page = 1
	}
	if p.PageSize == 0 {
		p.PageSize = 20
	}
}

// Offset calculates SQL offset.
func (p *PaginationRequest) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// PaginationResponse contains pagination metadata.
type PaginationResponse struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"pageSize"`
	TotalItems int64 `json:"totalItems"`
	TotalPages int   `json:"totalPages"`
}

// NewPaginationResponse creates pagination response.
func NewPaginationResponse(page, pageSize int, totalItems int64) PaginationResponse {
	totalPages := int(totalItems) / pageSize
	if int(totalItems)%pageSize > 0 {
		totalPages++
	}
	return PaginationResponse{
		Page:       page,
		PageSize:   pageSize,
		TotalItems: totalItems,
		TotalPages: totalPages,
	}
}

// --- List Response ---

// ListResponse wraps list results with pagination.
type ListResponse struct {
	Items      any   `json:"items"`
	TotalCount int64 `json:"totalCount"`
	Limit      int   `json:"limit"`
	Offset     int   `json:"offset"`
}

// GenericListResponse wraps list results with pagination (generic version).
type GenericListResponse[T any] struct {
	Data       []T                `json:"data"`
	Pagination PaginationResponse `json:"pagination"`
}

// --- Common Filters ---

// BaseFilter contains common filter parameters.
type BaseFilter struct {
	Search       string     `form:"search"`
	IDs          []string   `form:"ids"`
	DeletionMark *bool      `form:"deletionMark"`
	CreatedFrom  *time.Time `form:"createdFrom"`
	CreatedTo    *time.Time `form:"createdTo"`
}

// --- Base DTOs ---

// BaseResponse contains common response fields.
type BaseResponse struct {
	ID           string            `json:"id"`
	DeletionMark bool              `json:"deletionMark"`
	Version      int               `json:"version"`
	Attributes   entity.Attributes `json:"attributes,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

// FromBase creates BaseResponse from entity.BaseResponse
func FromBaseCatalog(b entity.BaseCatalog) BaseResponse {
	return BaseResponse{
		ID:           b.ID.String(),
		DeletionMark: b.DeletionMark,
		Version:      b.Version,
		Attributes:   b.Attributes,
	}
}

func FromBaseDocument(b entity.BaseDocument) BaseResponse {
	return BaseResponse{
		ID:           b.ID.String(),
		DeletionMark: b.DeletionMark,
		Version:      b.Version,
		Attributes:   b.Attributes,
		CreatedAt:    b.CreatedAt,
		UpdatedAt:    b.UpdatedAt,
	}
}

// --- Catalog DTOs ---

// CatalogResponse contains catalog fields.
type CatalogResponse struct {
	BaseResponse
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	ParentID *string `json:"parentId,omitempty"`
	IsFolder bool    `json:"isFolder"`
}

// FromCatalog creates CatalogResponse from entity.Catalog.
func FromCatalog(c entity.Catalog) CatalogResponse {
	return CatalogResponse{
		BaseResponse: FromBaseCatalog(c.BaseCatalog),
		Code:         c.Code,
		Name:         c.Name,
		ParentID:     c.ParentID,
		IsFolder:     c.IsFolder,
	}
}

// CreateCatalogRequest for creating catalogs.
type CreateCatalogRequest struct {
	Code       string            `json:"code"`
	Name       string            `json:"name" binding:"required"`
	ParentID   *string           `json:"parentId"`
	IsFolder   bool              `json:"isFolder"`
	Attributes entity.Attributes `json:"attributes"`
}

// UpdateCatalogRequest for updating catalogs.
type UpdateCatalogRequest struct {
	Code       *string           `json:"code"`
	Name       *string           `json:"name"`
	ParentID   *string           `json:"parentId"`
	IsFolder   *bool             `json:"isFolder"`
	Attributes entity.Attributes `json:"attributes"`
	Version    int               `json:"version" binding:"required,min=1"`
}

// --- Document DTOs ---

// DocumentResponse contains document fields.
type DocumentResponse struct {
	BaseResponse
	Number         string    `json:"number"`
	Date           time.Time `json:"date"`
	Posted         bool      `json:"posted"`
	PostedVersion  int       `json:"postedVersion"`
	OrganizationID string    `json:"organizationId"`
	Description    string    `json:"description,omitempty"`
}

// FromDocument creates DocumentResponse from entity.Document.
func FromDocument(d entity.Document) DocumentResponse {
	return DocumentResponse{
		BaseResponse:   FromBaseDocument(d.BaseDocument),
		Number:         d.Number,
		Date:           d.Date,
		Posted:         d.Posted,
		PostedVersion:  d.PostedVersion,
		OrganizationID: d.OrganizationID,
		Description:    d.Description,
	}
}

// --- ID Response ---

// IDResponse for create operations.
type IDResponse struct {
	ID string `json:"id"`
}

// NewIDResponse creates ID response.
func NewIDResponse(i id.ID) IDResponse {
	return IDResponse{ID: i.String()}
}

// --- Success Response ---

// SuccessResponse for operations without data.
type SuccessResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// --- Error Response ---

// ErrorResponse for error details.
type ErrorResponse struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// --- Deletion ---
type SetDeletionMarkRequest struct {
	Marked bool `json:"marked"`
}
