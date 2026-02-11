package dto

import (
	"github.com/shopspring/decimal"

	"metapus/internal/core/entity"
	"metapus/internal/domain/catalogs/vat_rate"
)

// --- Request DTOs ---

// CreateVATRateRequest is the request body for creating a VAT rate.
type CreateVATRateRequest struct {
	Code        string            `json:"code"`
	Name        string            `json:"name" binding:"required"`
	Rate        decimal.Decimal   `json:"rate"`
	IsTaxExempt bool              `json:"isTaxExempt"`
	Description *string           `json:"description"`
	ParentID    *string           `json:"parentId"`
	IsFolder    bool              `json:"isFolder"`
	Attributes  entity.Attributes `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateVATRateRequest) ToEntity() *vat_rate.VATRate {
	vr := vat_rate.NewVATRate(r.Code, r.Name, r.Rate)
	vr.IsTaxExempt = r.IsTaxExempt
	vr.Description = r.Description
	vr.ParentID = stringPtrToIDPtr(r.ParentID)
	vr.IsFolder = r.IsFolder
	vr.Attributes = r.Attributes
	return vr
}

// UpdateVATRateRequest is the request body for updating a VAT rate.
type UpdateVATRateRequest struct {
	Code        string            `json:"code"`
	Name        string            `json:"name" binding:"required"`
	Rate        decimal.Decimal   `json:"rate"`
	IsTaxExempt bool              `json:"isTaxExempt"`
	Description *string           `json:"description"`
	ParentID    *string           `json:"parentId"`
	IsFolder    bool              `json:"isFolder"`
	Attributes  entity.Attributes `json:"attributes"`
	Version     int               `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateVATRateRequest) ApplyTo(vr *vat_rate.VATRate) {
	vr.Code = r.Code
	vr.Name = r.Name
	vr.Rate = r.Rate
	vr.IsTaxExempt = r.IsTaxExempt
	vr.Description = r.Description
	vr.ParentID = stringPtrToIDPtr(r.ParentID)
	vr.IsFolder = r.IsFolder
	vr.Attributes = r.Attributes
	vr.Version = r.Version
}

// --- Response DTOs ---

// VATRateResponse is the response body for a VAT rate.
type VATRateResponse struct {
	ID           string            `json:"id"`
	Code         string            `json:"code"`
	Name         string            `json:"name"`
	Rate         decimal.Decimal   `json:"rate"`
	IsTaxExempt  bool              `json:"isTaxExempt"`
	Description  *string           `json:"description,omitempty"`
	ParentID     *string           `json:"parentId,omitempty"`
	IsFolder     bool              `json:"isFolder"`
	DeletionMark bool              `json:"deletionMark"`
	Version      int               `json:"version"`
	Attributes   entity.Attributes `json:"attributes,omitempty"`
}

// FromVATRate creates response DTO from domain entity.
func FromVATRate(vr *vat_rate.VATRate) *VATRateResponse {
	return &VATRateResponse{
		ID:           vr.ID.String(),
		Code:         vr.Code,
		Name:         vr.Name,
		Rate:         vr.Rate,
		IsTaxExempt:  vr.IsTaxExempt,
		Description:  vr.Description,
		ParentID:     idToStringPtr(vr.ParentID),
		IsFolder:     vr.IsFolder,
		DeletionMark: vr.DeletionMark,
		Version:      vr.Version,
		Attributes:   vr.Attributes,
	}
}
