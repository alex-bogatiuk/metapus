package currency_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/domain/catalogs/currency"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

func TestCurrencyNumericCodeValidation(t *testing.T) {
	tests := []struct {
		name  string
		code  *string
		valid bool
	}{
		{name: "omitted", valid: true},
		{name: "empty", code: new(""), valid: true},
		{name: "numeric ISO code", code: new("840"), valid: true},
		{name: "leading zeros", code: new("008"), valid: true},
		{name: "ticker from UI", code: new("PYUSD")},
		{name: "three letters", code: new("USD")},
		{name: "short code", code: new("84")},
		{name: "long code", code: new("0840")},
		{name: "non-ASCII digits", code: new("８４０")},
		{name: "whitespace", code: new("   ")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			curr := currency.NewCurrency("PYUSD", "PYUSD", new("PYUSD"), new("PYUSD"))
			curr.DecimalPlaces = 6
			curr.ISONumericCode = tt.code

			curr.Normalize()
			err := curr.Validate(context.Background())
			if tt.valid {
				if err != nil {
					t.Fatalf("valid currency rejected: %v", err)
				}
				if curr.MinorMultiplier != 1_000_000 {
					t.Fatalf("minor multiplier = %d, want 1000000", curr.MinorMultiplier)
				}
				return
			}
			appErr, ok := apperror.AsAppError(err)
			if !ok {
				t.Fatalf("expected validation error, got %v", err)
			}
			if appErr.Code != apperror.CodeValidation || appErr.HTTPStatus != http.StatusBadRequest || appErr.Details["field"] != "isoNumericCode" {
				t.Fatalf("unexpected validation response: %+v", appErr)
			}
		})
	}
}

func TestCurrencyFormExcludesCalculatedMultiplier(t *testing.T) {
	def := metadata.Inspect(currency.Currency{}, "Currency", metadata.TypeCatalog)
	for _, field := range def.Fields {
		if field.Name == "minorMultiplier" {
			t.Fatal("calculated minor multiplier must not be exposed as a form field")
		}
	}
}
