// Package tabular applies report units and aggregates independently of transports.
package tabular

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
	reportcontract "github.com/alex-bogatiuk/metapus/internal/domain/reports/contract"
	"github.com/shopspring/decimal"
)

type MoneyUnit struct {
	CurrencyID    string `json:"currencyId"`
	CurrencyCode  string `json:"currencyCode"`
	DecimalPlaces int    `json:"decimalPlaces"`
}

// A report total may exceed one stored int64 amount; its exact integer is still
// carried as text. Every currency has its own total, including identical scales.
type MoneyTotal struct {
	MoneyUnit
	Amount string `json:"amount"`
}

func ReadMoneyUnit(row map[string]any, sources *reportcontract.MoneySources) (MoneyUnit, error) {
	if sources == nil {
		return MoneyUnit{}, fmt.Errorf("monetary units are not declared")
	}
	uid, okID := row[sources.CurrencyID].(string)
	code, okCode := row[sources.CurrencyCode].(string)
	scale, err := Numeric(row[sources.DecimalPlaces])
	if !okID || !okCode || uid == "" || code == "" || err != nil || !scale.IsInteger() || scale.IsNegative() || scale.GreaterThan(decimal.NewFromInt(18)) {
		return MoneyUnit{}, fmt.Errorf("invalid monetary unit projection")
	}
	return MoneyUnit{CurrencyID: uid, CurrencyCode: code, DecimalPlaces: int(scale.IntPart())}, nil
}

func Amount(value any) (decimal.Decimal, error) {
	text, ok := value.(string)
	if !ok {
		return decimal.Zero, fmt.Errorf("amount must be a minor-unit string")
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || strconv.FormatInt(n, 10) != text {
		return decimal.Zero, fmt.Errorf("amount must be a canonical int64 string")
	}
	return decimal.NewFromInt(n), nil
}

// AmountFor validates the registered unit contract, never a client type hint.
func AmountFor(value any, sources *reportcontract.MoneySources) (decimal.Decimal, error) {
	if sources == nil || sources.AmountType == "" {
		return Amount(value)
	}
	if sources.AmountType != reportcontract.AmountTypeRaw {
		return decimal.Zero, fmt.Errorf("unsupported report amount type")
	}
	text, ok := value.(string)
	if !ok {
		return decimal.Zero, fmt.Errorf("raw amount must be a raw-unit string")
	}
	amount, err := types.NewRawAmountFromString(text)
	if err != nil {
		return decimal.Zero, err
	}
	return amount.ToDecimal(0), nil
}

func FormatMoneyFor(value any, unit MoneyUnit, sources *reportcontract.MoneySources) (string, error) {
	amount, err := AmountFor(value, sources)
	if err != nil {
		return "", err
	}
	return amount.Shift(-int32(unit.DecimalPlaces)).StringFixed(int32(unit.DecimalPlaces)), nil
}

// Numeric is for report measures/counts; monetary inputs use Amount instead.
func Numeric(value any) (decimal.Decimal, error) {
	switch v := value.(type) {
	case int:
		return decimal.NewFromInt(int64(v)), nil
	case int32:
		return decimal.NewFromInt(int64(v)), nil
	case int64:
		return decimal.NewFromInt(v), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return decimal.Zero, fmt.Errorf("non-finite report number")
		}
		return decimal.NewFromFloat(v), nil
	case json.Number:
		return decimal.NewFromString(v.String())
	case string:
		return decimal.NewFromString(v)
	default:
		return decimal.Zero, fmt.Errorf("invalid report number %T", value)
	}
}

func FormatMoney(value any, unit MoneyUnit) (string, error) {
	amount, err := Amount(value)
	if err != nil {
		return "", err
	}
	return types.FormatMinorUnits(amount.IntPart(), unit.DecimalPlaces)
}

func FormatTotal(total MoneyTotal) string {
	value, err := decimal.NewFromString(total.Amount)
	if err != nil {
		return ""
	}
	return value.Shift(-int32(total.DecimalPlaces)).StringFixed(int32(total.DecimalPlaces)) + " " + total.CurrencyCode
}

func FormatCell(row map[string]any, col reportcontract.ReportColumn) (string, error) {
	value := row[col.Key]
	if value == nil {
		return "", nil
	}
	if col.Type == "money" {
		unit, err := ReadMoneyUnit(row, col.Money)
		if err != nil {
			return "", err
		}
		return FormatMoneyFor(value, unit, col.Money)
	}
	if flag, ok := value.(bool); ok {
		if flag {
			return "Да", nil
		}
		return "Нет", nil
	}
	return fmt.Sprint(value), nil
}

// ExcelDecimal keeps values with more than 15 significant decimal digits as
// text. Otherwise an explicit numeric cell carries the scale without losing a
// decimal digit when Excel loads/saves the workbook.
func ExcelDecimal(text string) (any, error) {
	value, err := decimal.NewFromString(text)
	if err != nil {
		return nil, err
	}
	significant := strings.TrimLeft(strings.TrimPrefix(value.String(), "-"), "0.")
	significant = strings.ReplaceAll(significant, ".", "")
	if len(significant) > 15 {
		return text, nil
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil, err
	}
	return number, nil
}
