package dto

import (
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/organization"
)

// CreateOrganizationRequest is the DTO for creating an organization.
type CreateOrganizationRequest struct {
	Code           string `json:"code"`
	Name           string `json:"name" binding:"required"`
	FullName       string `json:"fullName"`
	INN            string `json:"inn"`
	KPP            string `json:"kpp"`
	BaseCurrencyID id.ID  `json:"baseCurrencyId"`
	IsDefault      bool   `json:"isDefault"`
}

func (r CreateOrganizationRequest) ToEntity() *organization.Organization {
	org := organization.NewOrganization(r.Code, r.Name, r.BaseCurrencyID)
	if r.FullName != "" {
		org.FullName = &r.FullName
	}
	if r.INN != "" {
		org.INN = &r.INN
	}
	if r.KPP != "" {
		org.KPP = &r.KPP
	}
	org.IsDefault = r.IsDefault
	return org
}

// UpdateOrganizationRequest is the DTO for updating an organization.
type UpdateOrganizationRequest struct {
	ID             id.ID  `json:"id" binding:"required"`
	Version        int    `json:"version" binding:"required"`
	Code           string `json:"code"`
	Name           string `json:"name" binding:"required"`
	FullName       string `json:"fullName"`
	INN            string `json:"inn"`
	KPP            string `json:"kpp"`
	BaseCurrencyID id.ID  `json:"baseCurrencyId"`
	IsDefault      bool   `json:"isDefault"`
	DeletionMark   bool   `json:"deletionMark"`
}

func (r UpdateOrganizationRequest) ApplyTo(org *organization.Organization) {
	org.Code = r.Code
	org.Name = r.Name
	if r.FullName != "" {
		org.FullName = &r.FullName
	} else {
		org.FullName = nil
	}
	if r.INN != "" {
		org.INN = &r.INN
	} else {
		org.INN = nil
	}
	if r.KPP != "" {
		org.KPP = &r.KPP
	} else {
		org.KPP = nil
	}
	org.BaseCurrencyID = r.BaseCurrencyID
	org.IsDefault = r.IsDefault
	org.DeletionMark = r.DeletionMark
}

// OrganizationResponse is the DTO for returning organization data.
type OrganizationResponse struct {
	ID             id.ID  `json:"id"`
	Version        int    `json:"version"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	FullName       string `json:"fullName"`
	INN            string `json:"inn"`
	KPP            string `json:"kpp"`
	BaseCurrencyID id.ID  `json:"baseCurrencyId"`
	IsDefault      bool   `json:"isDefault"`
	DeletionMark   bool   `json:"deletionMark"`
}

func FromOrganization(org *organization.Organization) OrganizationResponse {
	resp := OrganizationResponse{
		ID:             org.ID,
		Version:        org.Version,
		Code:           org.Code,
		Name:           org.Name,
		BaseCurrencyID: org.BaseCurrencyID,
		IsDefault:      org.IsDefault,
		DeletionMark:   org.DeletionMark,
	}
	if org.FullName != nil {
		resp.FullName = *org.FullName
	}
	if org.INN != nil {
		resp.INN = *org.INN
	}
	if org.KPP != nil {
		resp.KPP = *org.KPP
	}
	return resp
}
