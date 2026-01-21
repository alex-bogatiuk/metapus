package dto

import (
	"github.com/shopspring/decimal"

	"metapus/internal/core/entity"
	"metapus/internal/domain/catalogs/unit"
)

// --- Request DTOs ---

// CreateUnitRequest is the request body for creating a unit.
type CreateUnitRequest struct {
	Code              string            `json:"code"`
	Name              string            `json:"name" binding:"required"`
	Type              unit.UnitType     `json:"type" binding:"required"`
	Symbol            string            `json:"symbol" binding:"required"`
	InternationalCode *string           `json:"internationalCode"`
	BaseUnitID        *string           `json:"baseUnitId"`
	ConversionFactor  decimal.Decimal   `json:"conversionFactor"`
	IsBase            bool              `json:"isBase"`
	Description       *string           `json:"comment"`
	ParentID          *string           `json:"parentId"`
	IsFolder          bool              `json:"isFolder"`
	Attributes        entity.Attributes `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateUnitRequest) ToEntity() *unit.Unit {
	u := unit.NewUnit(r.Code, r.Name, r.Symbol, r.Type)
	u.InternationalCode = r.InternationalCode
	u.BaseUnitID = r.BaseUnitID
	if !r.ConversionFactor.IsZero() {
		u.ConversionFactor = r.ConversionFactor
	}
	u.IsBase = r.IsBase
	u.Description = r.Description
	u.ParentID = r.ParentID
	u.IsFolder = r.IsFolder
	u.Attributes = r.Attributes
	return u
}

// UpdateUnitRequest is the request body for updating a unit.
type UpdateUnitRequest struct {
	Code              string            `json:"code"`
	Name              string            `json:"name" binding:"required"`
	Type              unit.UnitType     `json:"type" binding:"required"`
	Symbol            string            `json:"symbol" binding:"required"`
	InternationalCode *string           `json:"internationalCode"`
	BaseUnitID        *string           `json:"baseUnitId"`
	ConversionFactor  decimal.Decimal   `json:"conversionFactor"`
	IsBase            bool              `json:"isBase"`
	Description       *string           `json:"description"`
	ParentID          *string           `json:"parentId"`
	IsFolder          bool              `json:"isFolder"`
	Attributes        entity.Attributes `json:"attributes"`
	Version           int               `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateUnitRequest) ApplyTo(u *unit.Unit) {
	u.Code = r.Code
	u.Name = r.Name
	u.Type = r.Type
	u.Symbol = r.Symbol
	u.InternationalCode = r.InternationalCode
	u.BaseUnitID = r.BaseUnitID
	u.ConversionFactor = r.ConversionFactor
	u.IsBase = r.IsBase
	u.Description = r.Description
	u.ParentID = r.ParentID
	u.IsFolder = r.IsFolder
	u.Attributes = r.Attributes
	u.Version = r.Version
}

// --- Response DTOs ---

// UnitResponse is the response body for a unit.
type UnitResponse struct {
	ID                string            `json:"id"`
	Code              string            `json:"code"`
	Name              string            `json:"name"`
	Type              unit.UnitType     `json:"type"`
	Symbol            string            `json:"symbol"`
	InternationalCode *string           `json:"internationalCode,omitempty"`
	BaseUnitID        *string           `json:"baseUnitId,omitempty"`
	ConversionFactor  decimal.Decimal   `json:"conversionFactor"`
	IsBase            bool              `json:"isBase"`
	Description       *string           `json:"description,omitempty"`
	ParentID          *string           `json:"parentId,omitempty"`
	IsFolder          bool              `json:"isFolder"`
	DeletionMark      bool              `json:"deletionMark"`
	Version           int               `json:"version"`
	Attributes        entity.Attributes `json:"attributes,omitempty"`
}

// FromUnit creates response DTO from domain entity.
func FromUnit(u *unit.Unit) *UnitResponse {
	return &UnitResponse{
		ID:                u.ID.String(),
		Code:              u.Code,
		Name:              u.Name,
		Type:              u.Type,
		Symbol:            u.Symbol,
		InternationalCode: u.InternationalCode,
		BaseUnitID:        u.BaseUnitID,
		ConversionFactor:  u.ConversionFactor,
		IsBase:            u.IsBase,
		Description:       u.Description,
		ParentID:          u.ParentID,
		IsFolder:          u.IsFolder,
		DeletionMark:      u.DeletionMark,
		Version:           u.Version,
		Attributes:        u.Attributes,
	}
}
