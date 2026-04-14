// Package organization provides the Organization catalog.
package organization

import (
	"context"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// TaxSystem represents supported tax regimes.
type TaxSystem string

const (
	TaxSystemOSNO             TaxSystem = "osno"
	TaxSystemUSNIncome        TaxSystem = "usn_income"
	TaxSystemUSNIncomeExpense TaxSystem = "usn_income_expense"
	TaxSystemENVD             TaxSystem = "envd"
	TaxSystemPatent           TaxSystem = "patent"
)

// InventoryMethod represents inventory costing methods.
type InventoryMethod string

const (
	InventoryMethodFIFO     InventoryMethod = "fifo"
	InventoryMethodAverage  InventoryMethod = "average"
	InventoryMethodSpecific InventoryMethod = "specific"
)

// Organization represents a legal entity or business unit.
type Organization struct {
	entity.Catalog

	// ── Requisites ──────────────────────────────────────────────────────
	FullName *string `db:"full_name" json:"fullName,omitempty" meta:"label:Полное наименование"`
	INN      *string `db:"inn" json:"inn,omitempty" meta:"label:ИНН"`
	KPP      *string `db:"kpp" json:"kpp,omitempty" meta:"label:КПП"`
	OGRN     *string `db:"ogrn" json:"ogrn,omitempty" meta:"label:ОГРН"`

	// ── Addresses ──────────────────────────────────────────────────────
	LegalAddress  *string `db:"legal_address" json:"legalAddress,omitempty" meta:"label:Юридический адрес"`
	ActualAddress *string `db:"actual_address" json:"actualAddress,omitempty" meta:"label:Фактический адрес"`

	// ── Contacts ────────────────────────────────────────────────────────
	Phone   *string `db:"phone" json:"phone,omitempty" meta:"label:Телефон"`
	Email   *string `db:"email" json:"email,omitempty" meta:"label:Email"`
	Website *string `db:"website" json:"website,omitempty" meta:"label:Вебсайт"`

	// ── Currency ────────────────────────────────────────────────────────
	BaseCurrencyID id.ID `db:"base_currency_id" json:"baseCurrencyId,omitempty" meta:"label:Валюта учёта"`
	IsDefault      bool  `db:"is_default" json:"isDefault" meta:"label:Основная"`

	// ── Responsible persons ─────────────────────────────────────────────
	Director   *string `db:"director" json:"director,omitempty" meta:"label:Руководитель"`
	Accountant *string `db:"accountant" json:"accountant,omitempty" meta:"label:Главный бухгалтер"`
	LogoURL    *string `db:"logo_url" json:"logoUrl,omitempty" meta:"label:Логотип"`

	// ── Accounting policy ───────────────────────────────────────────────
	TaxSystem         TaxSystem       `db:"tax_system" json:"taxSystem" meta:"label:Система налогообложения"`
	VatPayer          bool            `db:"vat_payer" json:"vatPayer" meta:"label:Плательщик НДС"`
	DefaultVatRateID  *id.ID          `db:"default_vat_rate_id" json:"defaultVatRateId,omitempty" meta:"label:Ставка НДС по умолчанию"`
	InventoryMethod   InventoryMethod `db:"inventory_method" json:"inventoryMethod" meta:"label:Метод учёта запасов"`
	FiscalYearStart   string          `db:"fiscal_year_start" json:"fiscalYearStart" meta:"label:Начало фискального года"`
}

// NewOrganization creates a new Organization with required fields.
func NewOrganization(code, name string, baseCurrencyID id.ID) *Organization {
	return &Organization{
		Catalog:         entity.NewCatalog(code, name),
		BaseCurrencyID:  baseCurrencyID,
		TaxSystem:       TaxSystemOSNO,
		InventoryMethod: InventoryMethodFIFO,
		FiscalYearStart: "01-01",
	}
}

// Validate implements entity.Validatable interface.
func (o *Organization) Validate(ctx context.Context) error {
	return o.Catalog.Validate(ctx)
}
