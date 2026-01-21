package dto

import (
	"metapus/internal/core/entity"
	"metapus/internal/domain/catalogs/warehouse"
)

// --- Request DTOs ---

// CreateWarehouseRequest is the request body for creating a warehouse.
type CreateWarehouseRequest struct {
	Code               string                  `json:"code"`
	Name               string                  `json:"name" binding:"required"`
	Type               warehouse.WarehouseType `json:"type" binding:"required"`
	Address            *string                 `json:"address"`
	IsActive           bool                    `json:"isActive"`
	AllowNegativeStock bool                    `json:"allowNegativeStock"`
	IsDefault          bool                    `json:"isDefault"`
	OrganizationID     string                  `json:"organizationId"`
	Description        *string                 `json:"description"`
	ParentID           *string                 `json:"parentId"`
	IsFolder           bool                    `json:"isFolder"`
	Attributes         entity.Attributes       `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateWarehouseRequest) ToEntity() *warehouse.Warehouse {
	wh := warehouse.NewWarehouse(r.Code, r.Name, r.Type)
	wh.Address = r.Address
	wh.IsActive = r.IsActive
	wh.AllowNegativeStock = r.AllowNegativeStock
	wh.IsDefault = r.IsDefault
	wh.OrganizationID = r.OrganizationID
	wh.Description = r.Description
	wh.ParentID = r.ParentID
	wh.IsFolder = r.IsFolder
	wh.Attributes = r.Attributes
	return wh
}

// UpdateWarehouseRequest is the request body for updating a warehouse.
type UpdateWarehouseRequest struct {
	Code               string                  `json:"code"`
	Name               string                  `json:"name" binding:"required"`
	Type               warehouse.WarehouseType `json:"type" binding:"required"`
	Address            *string                 `json:"address,omitempty"`
	IsActive           bool                    `json:"isActive"`
	AllowNegativeStock bool                    `json:"allowNegativeStock"`
	IsDefault          bool                    `json:"isDefault"`
	OrganizationID     string                  `json:"organizationId"`
	Description        *string                 `json:"description,omitempty"`
	ParentID           *string                 `json:"parentId,omitempty"`
	IsFolder           bool                    `json:"isFolder"`
	Attributes         entity.Attributes       `json:"attributes"`
	Version            int                     `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateWarehouseRequest) ApplyTo(wh *warehouse.Warehouse) {
	wh.Code = r.Code
	wh.Name = r.Name
	wh.Type = r.Type
	wh.Address = r.Address
	wh.IsActive = r.IsActive
	wh.AllowNegativeStock = r.AllowNegativeStock
	wh.IsDefault = r.IsDefault
	wh.OrganizationID = r.OrganizationID
	wh.Description = r.Description
	wh.ParentID = r.ParentID
	wh.IsFolder = r.IsFolder
	wh.Attributes = r.Attributes
	wh.Version = r.Version
}

// --- Response DTOs ---

// WarehouseResponse is the response body for a warehouse.
type WarehouseResponse struct {
	ID                 string                  `json:"id"`
	Code               string                  `json:"code"`
	Name               string                  `json:"name"`
	Type               warehouse.WarehouseType `json:"type"`
	Address            *string                 `json:"address,omitempty"`
	IsActive           bool                    `json:"isActive"`
	AllowNegativeStock bool                    `json:"allowNegativeStock"`
	IsDefault          bool                    `json:"isDefault"`
	OrganizationID     string                  `json:"organizationId,omitempty"`
	Description        *string                 `json:"description,omitempty"`
	ParentID           *string                 `json:"parentId,omitempty"`
	IsFolder           bool                    `json:"isFolder"`
	DeletionMark       bool                    `json:"deletionMark"`
	Version            int                     `json:"version"`
	Attributes         entity.Attributes       `json:"attributes,omitempty"`
}

// FromWarehouse creates response DTO from domain entity.
func FromWarehouse(wh *warehouse.Warehouse) *WarehouseResponse {
	return &WarehouseResponse{
		ID:                 wh.ID.String(),
		Code:               wh.Code,
		Name:               wh.Name,
		Type:               wh.Type,
		Address:            wh.Address,
		IsActive:           wh.IsActive,
		AllowNegativeStock: wh.AllowNegativeStock,
		IsDefault:          wh.IsDefault,
		OrganizationID:     wh.OrganizationID,
		Description:        wh.Description,
		ParentID:           wh.ParentID,
		IsFolder:           wh.IsFolder,
		DeletionMark:       wh.DeletionMark,
		Version:            wh.Version,
		Attributes:         wh.Attributes,
	}
}
