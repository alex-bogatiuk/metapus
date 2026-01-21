// Package entity provides base types for all domain entities.
package entity

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"
)

// Attributes represents JSONB custom fields with type-safe accessors.
// Implements sql.Scanner and driver.Valuer for PostgreSQL JSONB mapping.
//
// CRITICAL: Uses json.Number to preserve numeric precision.
// Default Go JSON decoder converts numbers to float64, losing precision for decimals.
type Attributes map[string]any

// Scan implements sql.Scanner for reading from PostgreSQL JSONB.
// Uses custom decoder with UseNumber() to preserve numeric precision.
func (a *Attributes) Scan(src any) error {
	if src == nil {
		*a = nil
		return nil
	}

	var source []byte
	switch v := src.(type) {
	case []byte:
		source = v
	case string:
		source = []byte(v)
	default:
		return fmt.Errorf("unsupported type for Attributes: %T", src)
	}

	if len(source) == 0 {
		*a = nil
		return nil
	}

	// CRITICAL: UseNumber() preserves numeric precision
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber()

	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		return fmt.Errorf("failed to decode Attributes: %w", err)
	}

	*a = result
	return nil
}

// Value implements driver.Valuer for writing to PostgreSQL JSONB.
func (a Attributes) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	return json.Marshal(a)
}

// --- Type-safe getters ---

// GetString returns string value or empty string if not found/wrong type.
func (a Attributes) GetString(key string) string {
	if a == nil {
		return ""
	}
	if v, ok := a[key].(string); ok {
		return v
	}
	return ""
}

// GetStringOr returns string value or default if not found/wrong type.
func (a Attributes) GetStringOr(key, defaultVal string) string {
	if v := a.GetString(key); v != "" {
		return v
	}
	return defaultVal
}

// GetInt returns int64 value, handling json.Number correctly.
func (a Attributes) GetInt(key string) int64 {
	if a == nil {
		return 0
	}
	switch v := a[key].(type) {
	case json.Number:
		i, _ := v.Int64()
		return i
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

// GetFloat returns float64 value, handling json.Number correctly.
func (a Attributes) GetFloat(key string) float64 {
	if a == nil {
		return 0
	}
	switch v := a[key].(type) {
	case json.Number:
		f, _ := v.Float64()
		return f
	case float64:
		return v
	case int64:
		return float64(v)
	case int:
		return float64(v)
	}
	return 0
}

// GetDecimal returns decimal.Decimal value with full precision.
// This is the preferred method for monetary values.
func (a Attributes) GetDecimal(key string) decimal.Decimal {
	if a == nil {
		return decimal.Zero
	}
	switch v := a[key].(type) {
	case json.Number:
		d, err := decimal.NewFromString(v.String())
		if err != nil {
			return decimal.Zero
		}
		return d
	case string:
		d, err := decimal.NewFromString(v)
		if err != nil {
			return decimal.Zero
		}
		return d
	case float64:
		return decimal.NewFromFloat(v)
	}
	return decimal.Zero
}

// GetBool returns boolean value.
func (a Attributes) GetBool(key string) bool {
	if a == nil {
		return false
	}
	if v, ok := a[key].(bool); ok {
		return v
	}
	return false
}

// GetMap returns nested map.
func (a Attributes) GetMap(key string) Attributes {
	if a == nil {
		return nil
	}
	if v, ok := a[key].(map[string]any); ok {
		return Attributes(v)
	}
	return nil
}

// Has checks if key exists (including nil values).
func (a Attributes) Has(key string) bool {
	if a == nil {
		return false
	}
	_, ok := a[key]
	return ok
}

// Set adds or updates a value. Returns self for chaining.
func (a *Attributes) Set(key string, value any) Attributes {
	if *a == nil {
		*a = make(Attributes)
	}
	(*a)[key] = value
	return *a
}

// Delete removes a key. Returns self for chaining.
func (a Attributes) Delete(key string) Attributes {
	delete(a, key)
	return a
}

// Clone creates a shallow copy.
func (a Attributes) Clone() Attributes {
	if a == nil {
		return nil
	}
	result := make(Attributes, len(a))
	for k, v := range a {
		result[k] = v
	}
	return result
}
