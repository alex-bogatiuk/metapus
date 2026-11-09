package automation

import (
	"math"
	"math/big"

	"metapus/internal/core/json"
)

// SanitizePayloadNumbers recursively walks a map[string]any (typically produced
// by json.UnmarshalPreserveNumbers) and converts whole numbers to int64.
//
// Why: unmarshalling into map[string]any would otherwise leave JSON numbers as
// float64 or json.Number. Large integers then render as "1e+07" in Go templates.
//
// By converting whole numbers → int64 at the JSON↔engine boundary:
//   - Go templates render "10000000" (not "1e+07")
//   - CEL evaluation works correctly (int64 is a native CEL type)
//   - No template changes needed (no custom {{num .field}} function)
//   - Works for ALL entity types, not just crypto
func SanitizePayloadNumbers(m map[string]any) map[string]any {
	for k, v := range m {
		m[k] = sanitizeValue(v)
	}
	return m
}

func sanitizeValue(v any) any {
	switch val := v.(type) {
	case float64:
		if val == math.Trunc(val) && val >= math.MinInt64 && val < -math.MinInt64 {
			return int64(val)
		}
		return val

	case json.Number:
		if i, err := val.Int64(); err == nil {
			return i
		}
		if exact, ok := new(big.Rat).SetString(val.String()); ok && exact.IsInt() {
			if exact.Num().IsInt64() {
				return exact.Num().Int64()
			}
			return val
		}
		f, err := val.Float64()
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return val
		}
		return f

	case map[string]any:
		return SanitizePayloadNumbers(val)

	case []any:
		for i, elem := range val {
			val[i] = sanitizeValue(elem)
		}
		return val

	default:
		return v
	}
}
