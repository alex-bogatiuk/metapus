package types

import (
	"fmt"
	"strconv"
)

// ParseMinorUnits parses an exact canonical integer without changing any JSON codec.
func ParseMinorUnits(text string) (MinorUnits, error) {
	if len(text) == 0 || len(text) > 20 {
		return 0, fmt.Errorf("money must be an int64 minor-unit string")
	}
	digits := text
	if digits[0] == '-' {
		digits = digits[1:]
	}
	if len(digits) == 0 || (len(digits) > 1 && digits[0] == '0') || text == "-0" {
		return 0, fmt.Errorf("money must be a canonical integer")
	}
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("money must be an integer")
		}
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("money is outside int64 range: %w", err)
	}
	return MinorUnits(value), nil
}
