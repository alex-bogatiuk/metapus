package dto

import (
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/organization"
)

// ── Helpers ─────────────────────────────────────────────────────────────

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func idPtr(v id.ID) *id.ID {
	if id.IsNil(v) {
		return nil
	}
	return &v
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefID(p *id.ID) id.ID {
	if p == nil {
		return id.ID{}
	}
	return *p
}

// ── Create ──────────────────────────────────────────────────────────────

// CreateOrganizationRequest is the DTO for creating an organization.
type CreateOrganizationRequest struct {
	Code string `json:"code"`
	Name string `json:"name" binding:"required"`

	// Requisites
	FullName string `json:"fullName"`
	INN      string `json:"inn"`
	KPP      string `json:"kpp"`
	OGRN     string `json:"ogrn"`

	// Addresses
	LegalAddress  string `json:"legalAddress"`
	ActualAddress string `json:"actualAddress"`

	// Contacts
	Phone   string `json:"phone"`
	Email   string `json:"email"`
	Website string `json:"website"`

	// Currency & default
	BaseCurrencyID id.ID `json:"baseCurrencyId"`
	IsDefault      bool  `json:"isDefault"`

	// Responsible persons
	Director   string `json:"director"`
	Accountant string `json:"accountant"`
	LogoURL    string `json:"logoUrl"`

	// Accounting policy
	TaxSystem        string `json:"taxSystem"`
	VatPayer         bool   `json:"vatPayer"`
	DefaultVatRateID id.ID  `json:"defaultVatRateId"`
	InventoryMethod  string `json:"inventoryMethod"`
	FiscalYearStart  string `json:"fiscalYearStart"`
}

func (r CreateOrganizationRequest) ToEntity() *organization.Organization {
	org := organization.NewOrganization(r.Code, r.Name, r.BaseCurrencyID)
	org.FullName = strPtr(r.FullName)
	org.INN = strPtr(r.INN)
	org.KPP = strPtr(r.KPP)
	org.OGRN = strPtr(r.OGRN)
	org.LegalAddress = strPtr(r.LegalAddress)
	org.ActualAddress = strPtr(r.ActualAddress)
	org.Phone = strPtr(r.Phone)
	org.Email = strPtr(r.Email)
	org.Website = strPtr(r.Website)
	org.IsDefault = r.IsDefault
	org.Director = strPtr(r.Director)
	org.Accountant = strPtr(r.Accountant)
	org.LogoURL = strPtr(r.LogoURL)
	if r.TaxSystem != "" {
		org.TaxSystem = organization.TaxSystem(r.TaxSystem)
	}
	org.VatPayer = r.VatPayer
	org.DefaultVatRateID = idPtr(r.DefaultVatRateID)
	if r.InventoryMethod != "" {
		org.InventoryMethod = organization.InventoryMethod(r.InventoryMethod)
	}
	if r.FiscalYearStart != "" {
		org.FiscalYearStart = r.FiscalYearStart
	}
	return org
}

// ── Update ──────────────────────────────────────────────────────────────

// UpdateOrganizationRequest is the DTO for updating an organization.
type UpdateOrganizationRequest struct {
	ID      id.ID `json:"id" binding:"required"`
	Version int   `json:"version" binding:"required"`

	Code string `json:"code"`
	Name string `json:"name" binding:"required"`

	// Requisites
	FullName string `json:"fullName"`
	INN      string `json:"inn"`
	KPP      string `json:"kpp"`
	OGRN     string `json:"ogrn"`

	// Addresses
	LegalAddress  string `json:"legalAddress"`
	ActualAddress string `json:"actualAddress"`

	// Contacts
	Phone   string `json:"phone"`
	Email   string `json:"email"`
	Website string `json:"website"`

	// Currency & default
	BaseCurrencyID id.ID `json:"baseCurrencyId"`
	IsDefault      bool  `json:"isDefault"`
	DeletionMark   bool  `json:"deletionMark"`

	// Responsible persons
	Director   string `json:"director"`
	Accountant string `json:"accountant"`
	LogoURL    string `json:"logoUrl"`

	// Accounting policy
	TaxSystem        string `json:"taxSystem"`
	VatPayer         bool   `json:"vatPayer"`
	DefaultVatRateID id.ID  `json:"defaultVatRateId"`
	InventoryMethod  string `json:"inventoryMethod"`
	FiscalYearStart  string `json:"fiscalYearStart"`
}

func (r UpdateOrganizationRequest) ApplyTo(org *organization.Organization) {
	org.Code = r.Code
	org.Name = r.Name
	org.FullName = strPtr(r.FullName)
	org.INN = strPtr(r.INN)
	org.KPP = strPtr(r.KPP)
	org.OGRN = strPtr(r.OGRN)
	org.LegalAddress = strPtr(r.LegalAddress)
	org.ActualAddress = strPtr(r.ActualAddress)
	org.Phone = strPtr(r.Phone)
	org.Email = strPtr(r.Email)
	org.Website = strPtr(r.Website)
	org.BaseCurrencyID = r.BaseCurrencyID
	org.IsDefault = r.IsDefault
	org.DeletionMark = r.DeletionMark
	org.Director = strPtr(r.Director)
	org.Accountant = strPtr(r.Accountant)
	org.LogoURL = strPtr(r.LogoURL)
	if r.TaxSystem != "" {
		org.TaxSystem = organization.TaxSystem(r.TaxSystem)
	}
	org.VatPayer = r.VatPayer
	org.DefaultVatRateID = idPtr(r.DefaultVatRateID)
	if r.InventoryMethod != "" {
		org.InventoryMethod = organization.InventoryMethod(r.InventoryMethod)
	}
	if r.FiscalYearStart != "" {
		org.FiscalYearStart = r.FiscalYearStart
	}
}

// ── Response ────────────────────────────────────────────────────────────

// OrganizationResponse is the DTO for returning organization data.
type OrganizationResponse struct {
	ID      id.ID `json:"id"`
	Version int   `json:"version"`
	Code    string `json:"code"`
	Name    string `json:"name"`

	// Requisites
	FullName string `json:"fullName"`
	INN      string `json:"inn"`
	KPP      string `json:"kpp"`
	OGRN     string `json:"ogrn"`

	// Addresses
	LegalAddress  string `json:"legalAddress"`
	ActualAddress string `json:"actualAddress"`

	// Contacts
	Phone   string `json:"phone"`
	Email   string `json:"email"`
	Website string `json:"website"`

	// Currency & default
	BaseCurrencyID id.ID `json:"baseCurrencyId"`
	IsDefault      bool  `json:"isDefault"`
	DeletionMark   bool  `json:"deletionMark"`

	// Responsible persons
	Director   string `json:"director"`
	Accountant string `json:"accountant"`
	LogoURL    string `json:"logoUrl"`

	// Accounting policy
	TaxSystem        string `json:"taxSystem"`
	VatPayer         bool   `json:"vatPayer"`
	DefaultVatRateID id.ID  `json:"defaultVatRateId"`
	InventoryMethod  string `json:"inventoryMethod"`
	FiscalYearStart  string `json:"fiscalYearStart"`
}

func FromOrganization(org *organization.Organization) OrganizationResponse {
	return OrganizationResponse{
		ID:               org.ID,
		Version:          org.Version,
		Code:             org.Code,
		Name:             org.Name,
		FullName:         derefStr(org.FullName),
		INN:              derefStr(org.INN),
		KPP:              derefStr(org.KPP),
		OGRN:             derefStr(org.OGRN),
		LegalAddress:     derefStr(org.LegalAddress),
		ActualAddress:    derefStr(org.ActualAddress),
		Phone:            derefStr(org.Phone),
		Email:            derefStr(org.Email),
		Website:          derefStr(org.Website),
		BaseCurrencyID:   org.BaseCurrencyID,
		IsDefault:        org.IsDefault,
		DeletionMark:     org.DeletionMark,
		Director:         derefStr(org.Director),
		Accountant:       derefStr(org.Accountant),
		LogoURL:          derefStr(org.LogoURL),
		TaxSystem:        string(org.TaxSystem),
		VatPayer:         org.VatPayer,
		DefaultVatRateID: derefID(org.DefaultVatRateID),
		InventoryMethod:  string(org.InventoryMethod),
		FiscalYearStart:  org.FiscalYearStart,
	}
}
