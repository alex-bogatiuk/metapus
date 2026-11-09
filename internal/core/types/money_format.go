package types

import (
	"fmt"
	"strconv"
	"strings"
)

// FormatMinorUnits returns an exact major-unit decimal, including trailing
// currency digits. Formatting MinInt64 never negates its signed value.
func FormatMinorUnits(value int64, decimalPlaces int) (string, error) {
	if decimalPlaces < 0 || decimalPlaces > 18 {
		return "", fmt.Errorf("invalid currency scale: %d", decimalPlaces)
	}
	text := strconv.FormatInt(value, 10)
	sign := ""
	if strings.HasPrefix(text, "-") {
		sign, text = "-", text[1:]
	}
	if decimalPlaces == 0 {
		return sign + text, nil
	}
	if len(text) <= decimalPlaces {
		text = strings.Repeat("0", decimalPlaces+1-len(text)) + text
	}
	split := len(text) - decimalPlaces
	return sign + text[:split] + "." + text[split:], nil
}

func (m MinorUnits) MinorUnitsValue() int64 { return int64(m) }
