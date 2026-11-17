package formula

import (
	"testing"

	"github.com/shopspring/decimal"
)

func evalWith(t *testing.T, source string, values map[string]string) string {
	t.Helper()
	expr, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse(%q): %v", source, err)
	}
	return expr.Eval(func(name string) decimal.Decimal {
		value, ok := values[name]
		if !ok {
			t.Fatalf("%q read unknown name %q", source, name)
		}
		return decimal.RequireFromString(value)
	}).String()
}

func TestFormulasComputeExactlyWithPrecedence(t *testing.T) {
	values := map[string]string{"quantity": "1.5", "price": "1999", "discount": "10", "vatRate": "20"}
	cases := map[string]string{
		"quantity*price":                   "2998.5",
		"quantity * price - discount":      "2988.5",
		"quantity*(price-discount)":        "2983.5",
		"-quantity + 2":                    "0.5",
		"price*vatRate/100":                "399.8",
		"0.1+0.2":                          "0.3",
		"price/0":                          "0",
		"price/3":                          "666.3333333333333333",
		"(quantity*price)*(1+vatRate/100)": "3598.2",
	}
	for source, want := range cases {
		if got := evalWith(t, source, values); got != want {
			t.Errorf("%s = %s, want %s", source, got, want)
		}
	}
}

func TestFormulaVarsAreTheNamesItReads(t *testing.T) {
	expr, err := Parse("quantity*price + quantity")
	if err != nil {
		t.Fatal(err)
	}
	if got := expr.Vars(); len(got) != 2 || got[0] != "quantity" || got[1] != "price" {
		t.Fatalf("Vars() = %v", got)
	}
}

func TestBadFormulasAreRejected(t *testing.T) {
	for _, source := range []string{"", "quantity*", "(price", "price)", "price $ 2", "1..2"} {
		if _, err := Parse(source); err == nil {
			t.Errorf("Parse(%q) accepted a bad formula", source)
		}
	}
}
