// Package counterparty provides the Counterparty catalog (Справочник "Контрагенты").
// Counterparties represent business partners: customers, suppliers, etc.
package counterparty

import (
	"context"
	"regexp"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
)

// Pre-compiled regex patterns for validation (performance optimization)
var (
	whitespaceRE = regexp.MustCompile(`\s`)
	digitsOnlyRE = regexp.MustCompile(`^\d+$`)
	kppRE        = regexp.MustCompile(`^\d{9}$`)
	emailRE      = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
)

// CounterpartyType defines the type of counterparty.
type CounterpartyType string

const (
	TypeCustomer CounterpartyType = "customer" // Покупатель
	TypeSupplier CounterpartyType = "supplier" // Поставщик
	TypeBoth     CounterpartyType = "both"     // Покупатель и Поставщик
	TypeOther    CounterpartyType = "other"    // Прочие
)

// LegalForm defines the legal form of counterparty.
type LegalForm string

const (
	LegalIndividual LegalForm = "individual"  // Физлицо
	LegalSoleTrader LegalForm = "sole_trader" // ИП
	LegalCompany    LegalForm = "company"     // Юрлицо
	LegalGovernment LegalForm = "government"  // Гос. орган
)

// Counterparty represents a business partner (customer, supplier, etc.).
type Counterparty struct {
	entity.Catalog

	// Type defines whether this is a customer, supplier, or both
	Type CounterpartyType `db:"type" json:"type"`

	// LegalForm defines the legal status
	LegalForm LegalForm `db:"legal_form" json:"legalForm"`

	// FullName is the official registered name
	FullName *string `db:"full_name" json:"fullName"`

	// INN (ИНН) - Tax Identification Number
	INN *string `db:"inn" json:"inn"`

	// KPP (КПП) - Tax Registration Reason Code (for companies)
	KPP *string `db:"kpp" json:"kpp,omitempty"`

	// OGRN (ОГРН) - Primary State Registration Number
	OGRN *string `db:"ogrn" json:"ogrn,omitempty"`

	// LegalAddress is the registered address
	LegalAddress *string `db:"legal_address" json:"legalAddress,omitempty"`

	// ActualAddress is the actual/physical address
	ActualAddress *string `db:"actual_address" json:"actualAddress,omitempty"`

	// Phone is the primary contact phone
	Phone *string `db:"phone" json:"phone,omitempty"`

	// Email is the primary contact email
	Email *string `db:"email" json:"email,omitempty"`

	// ContactPerson is the primary contact name
	ContactPerson *string `db:"contact_person" json:"contactPerson,omitempty"`

	// Comment is a free-form note
	Comment *string `db:"comment" json:"comment,omitempty"`
}

// NewCounterparty creates a new Counterparty with required fields.
func NewCounterparty(code, name string, cpType CounterpartyType, legalForm LegalForm) *Counterparty {
	return &Counterparty{
		Catalog:   entity.NewCatalog(code, name),
		Type:      cpType,
		LegalForm: legalForm,
	}
}

// Validate implements entity.Validatable interface.
func (c *Counterparty) Validate(ctx context.Context) error {
	// Base catalog validation
	if err := c.Catalog.Validate(ctx); err != nil {
		return err
	}

	// Type validation
	if !isValidCounterpartyType(c.Type) {
		return apperror.NewValidation("invalid counterparty type").
			WithDetail("field", "type").
			WithDetail("value", string(c.Type))
	}

	// Legal form validation
	if !isValidLegalForm(c.LegalForm) {
		return apperror.NewValidation("invalid legal form").
			WithDetail("field", "legalForm").
			WithDetail("value", string(c.LegalForm))
	}

	// INN validation (if provided)
	if c.INN != nil && *c.INN != "" {
		if err := validateINN(*c.INN, c.LegalForm); err != nil {
			return err
		}
	}

	// KPP validation (required for companies)
	if c.LegalForm == LegalCompany && c.KPP != nil && *c.KPP != "" {
		if !isValidKPP(*c.KPP) {
			return apperror.NewValidation("invalid KPP format (must be 9 digits)").
				WithDetail("field", "kpp")
		}
	}

	// Email validation (if provided)
	if c.Email != nil && *c.Email != "" && !isValidEmail(*c.Email) {
		return apperror.NewValidation("invalid email format").
			WithDetail("field", "email")
	}

	return nil
}

// IsCustomer returns true if counterparty is a customer.
func (c *Counterparty) IsCustomer() bool {
	return c.Type == TypeCustomer || c.Type == TypeBoth
}

// IsSupplier returns true if counterparty is a supplier.
func (c *Counterparty) IsSupplier() bool {
	return c.Type == TypeSupplier || c.Type == TypeBoth
}

// --- Validation Helpers ---

func isValidCounterpartyType(t CounterpartyType) bool {
	switch t {
	case TypeCustomer, TypeSupplier, TypeBoth, TypeOther:
		return true
	}
	return false
}

func isValidLegalForm(f LegalForm) bool {
	switch f {
	case LegalIndividual, LegalSoleTrader, LegalCompany, LegalGovernment:
		return true
	}
	return false
}

func validateINN(inn string, form LegalForm) error {
	// Remove spaces
	cleaned := whitespaceRE.ReplaceAllString(inn, "")

	// Check length based on legal form
	switch form {
	case LegalIndividual, LegalSoleTrader:
		// Individual INN: 12 digits
		if len(cleaned) != 12 {
			return apperror.NewValidation("individual INN must be 12 digits").
				WithDetail("field", "inn")
		}
	case LegalCompany, LegalGovernment:
		// Company INN: 10 digits
		if len(cleaned) != 10 {
			return apperror.NewValidation("company INN must be 10 digits").
				WithDetail("field", "inn")
		}
	}

	// Check that all characters are digits
	if !digitsOnlyRE.MatchString(cleaned) {
		return apperror.NewValidation("INN must contain only digits").
			WithDetail("field", "inn")
	}

	return nil
}

func isValidKPP(kpp string) bool {
	return kppRE.MatchString(kpp)
}

func isValidEmail(email string) bool {
	return emailRE.MatchString(email)
}
