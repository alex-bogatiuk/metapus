package dto

import (
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/warehouse"
	"metapus/internal/infrastructure/storage/postgres"
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
	if r.OrganizationID != "" {
		orgID, err := id.Parse(r.OrganizationID)
		if err == nil {
			wh.OrganizationID = orgID
		}
	}
	wh.Description = r.Description
	wh.ParentID = stringPtrToIDPtr(r.ParentID)
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
	if r.OrganizationID != "" {
		orgID, err := id.Parse(r.OrganizationID)
		if err == nil {
			wh.OrganizationID = orgID
		}
	}
	wh.Description = r.Description
	wh.ParentID = stringPtrToIDPtr(r.ParentID)
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

	// Resolved reference display names (populated by ResolveRefs)
	Organization *postgres.RefDisplay `json:"organization,omitempty"`
}

// FromWarehouse creates response DTO from domain entity.
// Pass nil for refs if reference resolution is not needed.
func FromWarehouse(wh *warehouse.Warehouse, refs ...postgres.ResolvedRefs) *WarehouseResponse {
	resp := &WarehouseResponse{
		ID:                 wh.ID.String(),
		Code:               wh.Code,
		Name:               wh.Name,
		Type:               wh.Type,
		Address:            wh.Address,
		IsActive:           wh.IsActive,
		AllowNegativeStock: wh.AllowNegativeStock,
		IsDefault:          wh.IsDefault,
		OrganizationID:     wh.OrganizationID.String(),
		Description:        wh.Description,
		ParentID:           idToStringPtr(wh.ParentID),
		IsFolder:           wh.IsFolder,
		DeletionMark:       wh.DeletionMark,
		Version:            wh.Version,
		Attributes:         wh.Attributes,
	}

	// Populate resolved reference display names
	if len(refs) > 0 && refs[0] != nil {
		resolved := refs[0]
		if !id.IsNil(wh.OrganizationID) {
			d := resolved.Get(TableOrganizations, wh.OrganizationID)
			if d.ID != "" {
				resp.Organization = &d
			}
		}
	}

	return resp
}

// CollectWarehouseRefs registers all reference IDs from a Warehouse
// into the resolver for batch resolution.
func CollectWarehouseRefs(resolver *postgres.ReferenceResolver, wh *warehouse.Warehouse) {
	if !id.IsNil(wh.OrganizationID) {
		resolver.Add(TableOrganizations, wh.OrganizationID)
	}
}
