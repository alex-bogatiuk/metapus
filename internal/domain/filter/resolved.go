package filter

import (
	"fmt"
	"math"
	"reflect"
	"strconv"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
)

// ValueKind is declared by the server's model or dataset registration.
type ValueKind uint8

const (
	UnknownValue ValueKind = iota // Unset storage semantics; rejected by Resolve.
	NativeValue
	DateValue
	QuantityValue
	MoneyValue
	RawAmountValue
)

// FieldSpec describes storage semantics independently of the HTTP filter hints.
// Kind must be explicitly declared, including NativeValue for unconverted values.
type FieldSpec struct{ Kind ValueKind }

// ResolvedItem can only be constructed through Resolve. SQL adapters cannot
// accidentally use a client's fieldType or scale as trusted storage metadata.
type ResolvedItem struct {
	field    string
	operator ComparisonType
	value    any
	kind     ValueKind
}

func (r ResolvedItem) Field() string            { return r.field }
func (r ResolvedItem) Operator() ComparisonType { return r.operator }
func (r ResolvedItem) Value() any               { return r.value }
func (r ResolvedItem) Kind() ValueKind          { return r.kind }
func (r ResolvedItem) WithOperator(op ComparisonType) ResolvedItem {
	r.operator = op
	return r
}

func Resolve(item Item, spec FieldSpec) (ResolvedItem, error) {
	if spec.Kind == UnknownValue || spec.Kind > RawAmountValue {
		return ResolvedItem{}, fmt.Errorf("unsupported filter field kind")
	}
	if err := ValidateItems([]Item{item}); err != nil {
		return ResolvedItem{}, err
	}
	resolved := ResolvedItem{field: item.Field, operator: item.Operator, kind: spec.Kind}
	if item.Operator == IsNull || item.Operator == IsNotNull {
		return resolved, nil
	}
	if spec.Kind == MoneyValue || spec.Kind == QuantityValue || spec.Kind == RawAmountValue {
		switch item.Operator {
		case Equal, NotEqual, Less, LessOrEqual, Greater, GreaterOrEqual, InList, NotInList:
		default:
			return ResolvedItem{}, fmt.Errorf("operator %q is not supported for an exact numeric field", item.Operator)
		}
	}
	list := item.Operator == InList || item.Operator == NotInList
	value := reflect.ValueOf(item.Value)
	if list {
		if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
			return ResolvedItem{}, fmt.Errorf("list operator requires an array")
		}
		items := make([]any, value.Len())
		for i := range items {
			var err error
			items[i], err = resolveValue(value.Index(i).Interface(), spec.Kind)
			if err != nil {
				return ResolvedItem{}, fmt.Errorf("filter list value[%d]: %w", i, err)
			}
		}
		resolved.value = items
	} else {
		_, uuid := item.Value.(id.ID)
		if value.Kind() == reflect.Slice || (value.Kind() == reflect.Array && !uuid) || value.Kind() == reflect.Map {
			return ResolvedItem{}, fmt.Errorf("scalar operator requires a scalar value")
		}
		var err error
		resolved.value, err = resolveValue(item.Value, spec.Kind)
		if err != nil {
			return ResolvedItem{}, err
		}
	}
	return resolved, nil
}

func resolveValue(value any, kind ValueKind) (any, error) {
	switch kind {
	case RawAmountValue:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("raw amount filter requires a raw-unit string")
		}
		amount, err := types.NewRawAmountFromString(text)
		return amount.String(), err
	case MoneyValue:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("money filter requires a string of minor units")
		}
		amount, err := types.ParseMinorUnits(text)
		return int64(amount), err
	case QuantityValue:
		var text string
		switch number := value.(type) {
		case string:
			text = number
		case json.Number:
			text = string(number)
		case int:
			text = strconv.Itoa(number)
		case int64:
			text = strconv.FormatInt(number, 10)
		case float64:
			if math.IsNaN(number) || math.IsInf(number, 0) {
				return nil, fmt.Errorf("quantity must be finite")
			}
			text = strconv.FormatFloat(number, 'f', -1, 64)
		default:
			return nil, fmt.Errorf("quantity filter requires a decimal number or string")
		}
		quantity, err := types.ParseQuantity(text)
		return int64(quantity), err
	default:
		// PostgreSQL can parse exact decimal text using the column's native type.
		if number, ok := value.(json.Number); ok {
			return string(number), nil
		}
		return value, nil
	}
}
