package automation

import "math"

// SanitizePayloadNumbers recursively walks a map[string]any (typically produced
// by json.Unmarshal) and converts float64 values that represent whole numbers
// back to int64.
//
// Why: Go's json.Unmarshal into map[string]any always decodes JSON numbers as
// float64. This causes large integers (e.g., CryptoAmount 10000000) to render
// as "1e+07" in Go templates (fmt's default for large float64).
//
// By converting float64 → int64 at the JSON↔engine boundary:
//   - Go templates render "10000000" (not "1e+07")
//   - CEL evaluation works correctly (int64 is a native CEL type)
//   - No template changes needed (no custom {{num .field}} function)
//   - Works for ALL entity types, not just crypto
//
// Safety: JSON numbers up to 2^53 are exact in float64.
// All our amounts (MinorUnits, CryptoAmount, Quantity) fit within this range,
// so the float64 → int64 conversion is lossless for integer values.
func SanitizePayloadNumbers(m map[string]any) map[string]any {
	for k, v := range m {
		m[k] = sanitizeValue(v)
	}
	return m
}

func sanitizeValue(v any) any {
	switch val := v.(type) {
	case float64:
		// Convert whole-number float64 to int64.
		// math.Trunc check ensures we don't lose fractional parts
		// (e.g., currency amounts like 1500.50 stay as float64).
		if val == math.Trunc(val) && !math.IsInf(val, 0) && !math.IsNaN(val) {
			return int64(val)
		}
		return val

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
