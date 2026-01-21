package dto

import (
	"metapus/internal/core/entity"
	"metapus/internal/domain/catalogs/counterparty"
)

// --- Request DTOs ---

// CreateCounterpartyRequest is the request body for creating a counterparty.
type CreateCounterpartyRequest struct {
	Code          string                        `json:"code"`
	Name          string                        `json:"name" binding:"required"`
	Type          counterparty.CounterpartyType `json:"type" binding:"required"`
	LegalForm     counterparty.LegalForm        `json:"legalForm" binding:"required"`
	FullName      *string                       `json:"fullName"`
	INN           *string                       `json:"inn"`
	KPP           *string                       `json:"kpp"`
	OGRN          *string                       `json:"ogrn"`
	LegalAddress  *string                       `json:"legalAddress"`
	ActualAddress *string                       `json:"actualAddress"`
	Phone         *string                       `json:"phone"`
	Email         *string                       `json:"email"`
	ContactPerson *string                       `json:"contactPerson"`
	Comment       *string                       `json:"comment"`
	ParentID      *string                       `json:"parentId"`
	IsFolder      bool                          `json:"isFolder"`
	Attributes    entity.Attributes             `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateCounterpartyRequest) ToEntity() *counterparty.Counterparty {
	cp := counterparty.NewCounterparty(r.Code, r.Name, r.Type, r.LegalForm)
	cp.FullName = r.FullName
	cp.INN = r.INN
	cp.KPP = r.KPP
	cp.OGRN = r.OGRN
	cp.LegalAddress = r.LegalAddress
	cp.ActualAddress = r.ActualAddress
	cp.Phone = r.Phone
	cp.Email = r.Email
	cp.ContactPerson = r.ContactPerson
	cp.Comment = r.Comment
	cp.ParentID = r.ParentID
	cp.IsFolder = r.IsFolder
	cp.Attributes = r.Attributes
	return cp
}

// UpdateCounterpartyRequest is the request body for updating a counterparty.
type UpdateCounterpartyRequest struct {
	Code          string                        `json:"code"`
	Name          string                        `json:"name" binding:"required"`
	Type          counterparty.CounterpartyType `json:"type" binding:"required"`
	LegalForm     counterparty.LegalForm        `json:"legalForm" binding:"required"`
	FullName      *string                       `json:"fullName"`
	INN           *string                       `json:"inn"`
	KPP           *string                       `json:"kpp"`
	OGRN          *string                       `json:"ogrn"`
	LegalAddress  *string                       `json:"legalAddress"`
	ActualAddress *string                       `json:"actualAddress"`
	Phone         *string                       `json:"phone"`
	Email         *string                       `json:"email"`
	ContactPerson *string                       `json:"contactPerson"`
	Comment       *string                       `json:"comment"`
	ParentID      *string                       `json:"parentId"`
	IsFolder      bool                          `json:"isFolder"`
	Attributes    entity.Attributes             `json:"attributes"`
	Version       int                           `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateCounterpartyRequest) ApplyTo(cp *counterparty.Counterparty) {
	cp.Code = r.Code
	cp.Name = r.Name
	cp.Type = r.Type
	cp.LegalForm = r.LegalForm
	cp.FullName = r.FullName
	cp.INN = r.INN
	cp.KPP = r.KPP
	cp.OGRN = r.OGRN
	cp.LegalAddress = r.LegalAddress
	cp.ActualAddress = r.ActualAddress
	cp.Phone = r.Phone
	cp.Email = r.Email
	cp.ContactPerson = r.ContactPerson
	cp.Comment = r.Comment
	cp.ParentID = r.ParentID
	cp.IsFolder = r.IsFolder
	cp.Attributes = r.Attributes
	cp.Version = r.Version
}

// --- Response DTOs ---

// CounterpartyResponse is the response body for a counterparty.
type CounterpartyResponse struct {
	ID            string                        `json:"id"`
	Code          string                        `json:"code"`
	Name          string                        `json:"name"`
	Type          counterparty.CounterpartyType `json:"type"`
	LegalForm     counterparty.LegalForm        `json:"legalForm"`
	FullName      *string                       `json:"fullName"`
	INN           *string                       `json:"inn"`
	KPP           *string                       `json:"kpp,omitempty"`
	OGRN          *string                       `json:"ogrn,omitempty"`
	LegalAddress  *string                       `json:"legalAddress,omitempty"`
	ActualAddress *string                       `json:"actualAddress,omitempty"`
	Phone         *string                       `json:"phone,omitempty"`
	Email         *string                       `json:"email,omitempty"`
	ContactPerson *string                       `json:"contactPerson,omitempty"`
	Comment       *string                       `json:"comment,omitempty"`
	ParentID      *string                       `json:"parentId,omitempty"`
	IsFolder      bool                          `json:"isFolder"`
	DeletionMark  bool                          `json:"deletionMark"`
	Version       int                           `json:"version"`
	Attributes    entity.Attributes             `json:"attributes,omitempty"`
}

// FromCounterparty creates response DTO from domain entity.
func FromCounterparty(cp *counterparty.Counterparty) *CounterpartyResponse {
	return &CounterpartyResponse{
		ID:            cp.ID.String(),
		Code:          cp.Code,
		Name:          cp.Name,
		Type:          cp.Type,
		LegalForm:     cp.LegalForm,
		FullName:      cp.FullName,
		INN:           cp.INN,
		KPP:           cp.KPP,
		OGRN:          cp.OGRN,
		LegalAddress:  cp.LegalAddress,
		ActualAddress: cp.ActualAddress,
		Phone:         cp.Phone,
		Email:         cp.Email,
		ContactPerson: cp.ContactPerson,
		Comment:       cp.Comment,
		ParentID:      cp.ParentID,
		IsFolder:      cp.IsFolder,
		DeletionMark:  cp.DeletionMark,
		Version:       cp.Version,
		Attributes:    cp.Attributes,
	}
}
