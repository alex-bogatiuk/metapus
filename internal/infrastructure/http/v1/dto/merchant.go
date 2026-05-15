package dto

import (
	"metapus/internal/core/entity"
	"metapus/internal/domain/catalogs/merchant"
)

// --- Request DTOs ---

// CreateMerchantRequest is the request body for creating a merchant.
type CreateMerchantRequest struct {
	Code           string            `json:"code"`
	Name           string            `json:"name" binding:"required"`
	LegalName      string            `json:"legalName"`
	WebhookURL     string            `json:"webhookUrl"`
	IsActive       bool              `json:"isActive"`
	ParentID       *string           `json:"parentId"`
	IsFolder       bool              `json:"isFolder"`
	Attributes     entity.Attributes `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateMerchantRequest) ToEntity() *merchant.Merchant {
	m := merchant.NewMerchant(r.Code, r.Name)
	m.LegalName = r.LegalName
	m.WebhookURL = r.WebhookURL
	m.IsActive = r.IsActive
	m.ParentID = stringPtrToIDPtr(r.ParentID)
	m.IsFolder = r.IsFolder
	m.Attributes = r.Attributes
	return m
}

// UpdateMerchantRequest is the request body for updating a merchant.
type UpdateMerchantRequest struct {
	Code           string            `json:"code"`
	Name           string            `json:"name" binding:"required"`
	LegalName      string            `json:"legalName"`
	WebhookURL     string            `json:"webhookUrl"`
	IsActive       bool              `json:"isActive"`
	KYBStatus      string            `json:"kybStatus"`
	ParentID       *string           `json:"parentId"`
	IsFolder       bool              `json:"isFolder"`
	Attributes     entity.Attributes `json:"attributes"`
	Version        int               `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateMerchantRequest) ApplyTo(m *merchant.Merchant) {
	m.Code = r.Code
	m.Name = r.Name
	m.LegalName = r.LegalName
	m.WebhookURL = r.WebhookURL
	m.IsActive = r.IsActive
	m.KYBStatus = merchant.KYBStatus(r.KYBStatus)
	m.ParentID = stringPtrToIDPtr(r.ParentID)
	m.IsFolder = r.IsFolder
	m.Attributes = r.Attributes
	m.Version = r.Version
}

// --- Response DTOs ---

// MerchantResponse is the response body for a merchant.
type MerchantResponse struct {
	ID             string            `json:"id"`
	Code           string            `json:"code"`
	Name           string            `json:"name"`
	LegalName      string            `json:"legalName"`
	WebhookURL     string            `json:"webhookUrl,omitempty"`
	IsActive       bool              `json:"isActive"`
	KYBStatus      string            `json:"kybStatus"`
	KYBStatusName  string            `json:"kybStatusName"`
	ParentID       *string           `json:"parentId,omitempty"`
	IsFolder       bool              `json:"isFolder"`
	DeletionMark   bool              `json:"deletionMark"`
	Version        int               `json:"version"`
	Attributes     entity.Attributes `json:"attributes,omitempty"`
}

// FromMerchant creates response DTO from domain entity.
func FromMerchant(m *merchant.Merchant) *MerchantResponse {
	return &MerchantResponse{
		ID:             m.ID.String(),
		Code:           m.Code,
		Name:           m.Name,
		LegalName:      m.LegalName,
		WebhookURL:     m.WebhookURL,
		IsActive:       m.IsActive,
		KYBStatus:      string(m.KYBStatus),
		KYBStatusName:  string(m.KYBStatus),
		ParentID:       idToStringPtr(m.ParentID),
		IsFolder:       m.IsFolder,
		DeletionMark:   m.DeletionMark,
		Version:        m.Version,
		Attributes:     m.Attributes,
	}
}
