package dto

import (
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/contract"
)

// --- Request DTOs ---

// CreateContractRequest is the request body for creating a contract.
type CreateContractRequest struct {
	Code            string            `json:"code"`
	Name            string            `json:"name" binding:"required"`
	CounterpartyID  string            `json:"counterpartyId" binding:"required"`
	Type            contract.ContractType `json:"type" binding:"required"`
	CurrencyID      *string           `json:"currencyId"`
	ValidFrom       *time.Time        `json:"validFrom"`
	ValidTo         *time.Time        `json:"validTo"`
	PaymentTermDays int               `json:"paymentTermDays"`
	Description     *string           `json:"description"`
	ParentID        *string           `json:"parentId"`
	IsFolder        bool              `json:"isFolder"`
	Attributes      entity.Attributes `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateContractRequest) ToEntity() *contract.Contract {
	counterpartyID, _ := id.Parse(r.CounterpartyID)
	c := contract.NewContract(r.Code, r.Name, counterpartyID, r.Type)
	if r.CurrencyID != nil {
		currID, _ := id.Parse(*r.CurrencyID)
		c.CurrencyID = &currID
	}
	c.ValidFrom = r.ValidFrom
	c.ValidTo = r.ValidTo
	c.PaymentTermDays = r.PaymentTermDays
	c.Description = r.Description
	c.ParentID = stringPtrToIDPtr(r.ParentID)
	c.IsFolder = r.IsFolder
	c.Attributes = r.Attributes
	return c
}

// UpdateContractRequest is the request body for updating a contract.
type UpdateContractRequest struct {
	Code            string            `json:"code"`
	Name            string            `json:"name" binding:"required"`
	CounterpartyID  string            `json:"counterpartyId" binding:"required"`
	Type            contract.ContractType `json:"type" binding:"required"`
	CurrencyID      *string           `json:"currencyId"`
	ValidFrom       *time.Time        `json:"validFrom"`
	ValidTo         *time.Time        `json:"validTo"`
	PaymentTermDays int               `json:"paymentTermDays"`
	Description     *string           `json:"description"`
	ParentID        *string           `json:"parentId"`
	IsFolder        bool              `json:"isFolder"`
	Attributes      entity.Attributes `json:"attributes"`
	Version         int               `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateContractRequest) ApplyTo(c *contract.Contract) {
	c.Code = r.Code
	c.Name = r.Name
	counterpartyID, _ := id.Parse(r.CounterpartyID)
	c.CounterpartyID = counterpartyID
	c.Type = r.Type
	if r.CurrencyID != nil {
		currID, _ := id.Parse(*r.CurrencyID)
		c.CurrencyID = &currID
	} else {
		c.CurrencyID = nil
	}
	c.ValidFrom = r.ValidFrom
	c.ValidTo = r.ValidTo
	c.PaymentTermDays = r.PaymentTermDays
	c.Description = r.Description
	c.ParentID = stringPtrToIDPtr(r.ParentID)
	c.IsFolder = r.IsFolder
	c.Attributes = r.Attributes
	c.Version = r.Version
}

// --- Response DTOs ---

// ContractResponse is the response body for a contract.
type ContractResponse struct {
	ID              string            `json:"id"`
	Code            string            `json:"code"`
	Name            string            `json:"name"`
	CounterpartyID  string            `json:"counterpartyId"`
	Type            contract.ContractType `json:"type"`
	CurrencyID      *string           `json:"currencyId,omitempty"`
	ValidFrom       *time.Time        `json:"validFrom,omitempty"`
	ValidTo         *time.Time        `json:"validTo,omitempty"`
	PaymentTermDays int               `json:"paymentTermDays"`
	Description     *string           `json:"description,omitempty"`
	ParentID        *string           `json:"parentId,omitempty"`
	IsFolder        bool              `json:"isFolder"`
	DeletionMark    bool              `json:"deletionMark"`
	Version         int               `json:"version"`
	Attributes      entity.Attributes `json:"attributes,omitempty"`
}

// FromContract creates response DTO from domain entity.
func FromContract(c *contract.Contract) *ContractResponse {
	resp := &ContractResponse{
		ID:              c.ID.String(),
		Code:            c.Code,
		Name:            c.Name,
		CounterpartyID:  c.CounterpartyID.String(),
		Type:            c.Type,
		ValidFrom:       c.ValidFrom,
		ValidTo:         c.ValidTo,
		PaymentTermDays: c.PaymentTermDays,
		Description:     c.Description,
		ParentID:        idToStringPtr(c.ParentID),
		IsFolder:        c.IsFolder,
		DeletionMark:    c.DeletionMark,
		Version:         c.Version,
		Attributes:      c.Attributes,
	}
	if c.CurrencyID != nil {
		s := c.CurrencyID.String()
		resp.CurrencyID = &s
	}
	return resp
}
