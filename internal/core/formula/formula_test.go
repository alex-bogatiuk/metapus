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
	return expr.Eval(Env{
		Value: func(name string) decimal.Decimal {
			value, ok := values[name]
			if !ok {
				t.Fatalf("%q read unknown name %q", source, name)
			}
			return decimal.RequireFromString(value)
		},
		Sum: func(ref SumRef) decimal.Decimal {
			value, ok := values[ref.Part+"."+ref.Field]
			if !ok {
				t.Fatalf("%q totals unknown column %v", source, ref)
			}
			return decimal.RequireFromString(value)
		},
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
	for _, source := range []string{"", "quantity*", "(price", "price)", "price $ 2", "1..2",
		"avg(items.amount)", "sum(items)", "sum(items.amount", "sum(items.amount*2)", "sum(.amount)"} {
		if _, err := Parse(source); err == nil {
			t.Errorf("Parse(%q) accepted a bad formula", source)
		}
	}
}

func TestHeaderFormulasTotalTablePartColumns(t *testing.T) {
	values := map[string]string{"items.amount": "5997", "services.amount": "1500", "discount": "97"}
	if got := evalWith(t, "sum(items.amount) + sum( services . amount ) - discount", values); got != "7400" {
		t.Fatalf("total = %s, want 7400", got)
	}
	expr, err := Parse("sum(items.amount) + sum(items.amount) + sum(services.amount)")
	if err != nil {
		t.Fatal(err)
	}
	if got := expr.Sums(); len(got) != 2 || got[0] != (SumRef{"items", "amount"}) || got[1] != (SumRef{"services", "amount"}) {
		t.Fatalf("Sums() = %v", got)
	}
	if len(expr.Vars()) != 0 {
		t.Fatalf("a total reads no field of the header: %v", expr.Vars())
	}
	if got := evalWith(t, "sum * 2", map[string]string{"sum": "4"}); got != "8" {
		t.Fatalf("a field named sum still reads as a field: %s", got)
	}
}
