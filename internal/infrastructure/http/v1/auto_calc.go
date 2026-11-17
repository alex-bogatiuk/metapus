package v1

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/formula"
	"metapus/internal/core/types"
)

// Calculated fields of declared documents (meta:"calc:quantity*price"), as
// 1C computes a line's amount when its quantity or price changes.
//
// A calculated field is read-only on the wire: requests never carry it, and
// the server computes it from the sibling fields of the same struct (the
// document header or a table-part line) every time a document is created
// or updated. The frontend recomputes it while the user types
// (lib/formula.ts); the stored value is the server's.
//
// Values take part as the user sees them in their own units: money as its
// minor units (amount = quantity × price stays in the price's currency),
// a Quantity as its decimal value, integers and decimals as they are. The
// result is rounded half away from zero to the target's scale: minor units
// and integers to whole numbers, Quantity and decimal to four places.

type calcKind int

const (
	calcInteger calcKind = iota
	calcMinorUnits
	calcQuantity
	calcDecimal
)

// scale is the number of decimal places a result is rounded to (and the
// metadata scale the frontend rounds to: lib/formula.ts formulaScale).
func (k calcKind) scale() int32 {
	switch k {
	case calcQuantity, calcDecimal:
		return 4
	default:
		return 0
	}
}

var (
	calcMinorUnitsType = reflect.TypeFor[types.MinorUnits]()
	calcQuantityType   = reflect.TypeFor[types.Quantity]()
	calcDecimalType    = reflect.TypeFor[decimal.Decimal]()
)

func calcKindOf(t reflect.Type) (calcKind, bool) {
	switch t {
	case calcMinorUnitsType:
		return calcMinorUnits, true
	case calcQuantityType:
		return calcQuantity, true
	case calcDecimalType:
		return calcDecimal, true
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return calcInteger, true
	}
	return 0, false
}

type calcOperand struct {
	index   []int
	kind    calcKind
	pointer bool
}

type autoCalc struct {
	label    string
	index    []int
	kind     calcKind
	expr     *formula.Expr
	operands map[string]calcOperand
}

// autoCalcs are the calculated fields of one struct in the order they are
// computed: a field another one reads comes first.
type autoCalcs []autoCalc

func calcTag(f reflect.StructField) string {
	for part := range strings.SplitSeq(f.Tag.Get("meta"), ",") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(part), "calc:"); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func calcLabel(f reflect.StructField, json string) string {
	for part := range strings.SplitSeq(f.Tag.Get("meta"), ",") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(part), "label:"); ok {
			return strings.TrimSpace(value)
		}
	}
	return json
}

// deriveAutoCalcs reads the calculated fields of a struct and checks them:
// each formula parses, names numeric fields of the same struct, and no
// formula depends on itself through others.
func deriveAutoCalcs(t reflect.Type, kind entity.Kind) (autoCalcs, error) {
	byJSON := map[string]reflect.StructField{}
	var declared []reflect.StructField
	for _, f := range reflect.VisibleFields(t) {
		if !f.IsExported() || f.Anonymous {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		byJSON[name] = f
		if calcTag(f) != "" {
			declared = append(declared, f)
		}
	}
	if len(declared) == 0 {
		return nil, nil
	}
	if kind != entity.DocumentKind {
		return nil, fmt.Errorf("calculated fields are supported on documents and their table parts")
	}
	pending := map[string]autoCalc{}
	deps := map[string][]string{}
	var order []string
	for _, f := range declared {
		json, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		target, ok := calcKindOf(f.Type)
		if !ok {
			return nil, fmt.Errorf("calculated field %s must be an integer, MinorUnits, Quantity or decimal, not %v", f.Name, f.Type)
		}
		expr, err := formula.Parse(calcTag(f))
		if err != nil {
			return nil, fmt.Errorf("calculated field %s: %w", f.Name, err)
		}
		c := autoCalc{label: calcLabel(f, json), index: f.Index, kind: target, expr: expr, operands: map[string]calcOperand{}}
		for _, name := range expr.Vars() {
			operand, ok := byJSON[name]
			if !ok {
				return nil, fmt.Errorf("calculated field %s reads %q, which is not a field of %v", f.Name, name, t)
			}
			fieldType, pointer := operand.Type, false
			if fieldType.Kind() == reflect.Pointer {
				fieldType, pointer = fieldType.Elem(), true
			}
			operandKind, ok := calcKindOf(fieldType)
			if !ok {
				return nil, fmt.Errorf("calculated field %s reads %q, which is not a number", f.Name, name)
			}
			c.operands[name] = calcOperand{index: operand.Index, kind: operandKind, pointer: pointer}
			if calcTag(operand) != "" {
				deps[json] = append(deps[json], name)
			}
		}
		pending[json] = c
		order = append(order, json)
	}
	// Dependencies first; a cycle is a declaration error.
	var sorted autoCalcs
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		switch state[name] {
		case 1:
			return fmt.Errorf("calculated fields depend on each other in a cycle: %s", strings.Join(append(path, name), " → "))
		case 2:
			return nil
		}
		state[name] = 1
		for _, dep := range deps[name] {
			if err := visit(dep, append(path, name)); err != nil {
				return err
			}
		}
		state[name] = 2
		sorted = append(sorted, pending[name])
		return nil
	}
	for _, name := range order {
		if err := visit(name, nil); err != nil {
			return nil, err
		}
	}
	return sorted, nil
}

func readCalc(v reflect.Value, operand calcOperand) decimal.Decimal {
	if operand.pointer {
		if v.IsNil() {
			return decimal.Zero
		}
		v = v.Elem()
	}
	switch operand.kind {
	case calcQuantity:
		return decimal.NewFromInt(v.Int()).Shift(-4)
	case calcDecimal:
		return v.Interface().(decimal.Decimal)
	default:
		return decimal.NewFromInt(v.Int())
	}
}

// apply computes the struct's calculated fields in place. A result outside
// the target's range is a validation error naming the field.
func (calcs autoCalcs) apply(v reflect.Value) error {
	for _, c := range calcs {
		result := c.expr.Eval(func(name string) decimal.Decimal {
			operand := c.operands[name]
			return readCalc(v.FieldByIndex(operand.index), operand)
		}).Round(c.kind.scale())
		target := v.FieldByIndex(c.index)
		switch c.kind {
		case calcDecimal:
			target.Set(reflect.ValueOf(result))
			continue
		case calcQuantity:
			result = result.Shift(4)
		}
		n, err := types.CheckedInt64FromDecimal(result)
		if err != nil || target.OverflowInt(n) {
			return apperror.NewValidation(fmt.Sprintf("«%s»: результат расчёта вне допустимого диапазона", c.label)).
				WithDetail("field", c.label)
		}
		target.SetInt(n)
	}
	return nil
}
