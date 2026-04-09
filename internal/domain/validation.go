package domain

import (
	"context"

	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// ValidatableDocLine provides access to common line fields for reusable validation.
// Document line types implement this to benefit from ValidateDocumentLines.
type ValidatableDocLine interface {
	GetProductID() id.ID
	GetUnitID() id.ID
	GetCoefficient() decimal.Decimal
	GetQuantity() types.Quantity
	GetVATRateID() id.ID
}

// ValidationRule is a reusable validation strategy for documents.
// Multiple rules can be composed to build a validation pipeline.
type ValidationRule[T any] func(ctx context.Context, doc T) error

// RunValidationRules executes a sequence of validation rules (fail-fast).
func RunValidationRules[T any](ctx context.Context, doc T, rules ...ValidationRule[T]) error {
	for _, rule := range rules {
		if err := rule(ctx, doc); err != nil {
			return err
		}
	}
	return nil
}

// ValidateDocumentLines validates common fields across all document line types.
// Checks: non-empty lines, product, unit, coefficient > 0, quantity > 0, VAT rate.
func ValidateDocumentLines[L ValidatableDocLine](lines []L) error {
	if len(lines) == 0 {
		return apperror.NewValidation("at least one line is required").
			WithDetail("field", "lines")
	}

	for i, line := range lines {
		lineNo := i + 1

		if id.IsNil(line.GetProductID()) {
			return apperror.NewValidation("product is required").
				WithDetail("field", "lines").
				WithDetail("lineNo", lineNo)
		}
		if id.IsNil(line.GetUnitID()) {
			return apperror.NewValidation("unit is required").
				WithDetail("field", "lines").
				WithDetail("lineNo", lineNo)
		}
		if line.GetCoefficient().LessThanOrEqual(decimal.Zero) {
			return apperror.NewValidation("coefficient must be positive").
				WithDetail("field", "lines").
				WithDetail("lineNo", lineNo)
		}
		if line.GetQuantity() <= 0 {
			return apperror.NewValidation("quantity must be positive").
				WithDetail("field", "lines").
				WithDetail("lineNo", lineNo)
		}
		if id.IsNil(line.GetVATRateID()) {
			return apperror.NewValidation("VAT rate is required").
				WithDetail("field", "lines").
				WithDetail("lineNo", lineNo)
		}
	}

	return nil
}
