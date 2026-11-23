package tabular

import (
	"fmt"
	reportcontract "github.com/alex-bogatiuk/metapus/internal/domain/reports/contract"
	"github.com/shopspring/decimal"
)

type accumulator struct {
	sum   decimal.Decimal
	min   decimal.Decimal
	max   decimal.Decimal
	count int64
}

func (a *accumulator) add(value decimal.Decimal) {
	if a.count == 0 || value.LessThan(a.min) {
		a.min = value
	}
	if a.count == 0 || value.GreaterThan(a.max) {
		a.max = value
	}
	a.sum = a.sum.Add(value)
	a.count++
}
func (a *accumulator) value(fn string) (decimal.Decimal, error) {
	switch fn {
	case "sum":
		return a.sum, nil
	case "count":
		return decimal.NewFromInt(a.count), nil
	case "avg":
		if a.count == 0 {
			return decimal.Zero, nil
		}
		return a.sum.Div(decimal.NewFromInt(a.count)), nil
	case "min":
		return a.min, nil
	case "max":
		return a.max, nil
	default:
		return decimal.Zero, fmt.Errorf("unsupported report aggregation %q", fn)
	}
}

func Aggregate(items []map[string]any, defs []reportcontract.ReportTotal) (map[string]any, error) {
	result := make(map[string]any, len(defs))
	for _, def := range defs {
		var ordinary accumulator
		order := []string{}
		units := map[string]MoneyUnit{}
		sums := map[string]*accumulator{}
		for _, row := range items {
			if row[def.Column] == nil {
				continue
			}
			if def.Money == nil {
				value, err := Numeric(row[def.Column])
				if err != nil {
					return nil, err
				}
				ordinary.add(value)
				continue
			}
			value, err := AmountFor(row[def.Column], def.Money)
			if err != nil {
				return nil, err
			}
			ordinary.count++
			unit, err := ReadMoneyUnit(row, def.Money)
			if err != nil {
				return nil, err
			}
			if existing, exists := units[unit.CurrencyID]; exists && existing != unit {
				return nil, fmt.Errorf("inconsistent unit for currency %s", unit.CurrencyID)
			}
			if sums[unit.CurrencyID] == nil {
				sums[unit.CurrencyID] = &accumulator{}
				units[unit.CurrencyID] = unit
				order = append(order, unit.CurrencyID)
			}
			sums[unit.CurrencyID].add(value)
		}
		if def.Money == nil || def.Func == "count" {
			value, err := ordinary.value(def.Func)
			if err != nil {
				return nil, err
			}
			result[def.Column], _ = value.Float64()
			continue
		}
		totals := make([]MoneyTotal, 0, len(order))
		for _, uid := range order {
			value, err := sums[uid].value(def.Func)
			if err != nil {
				return nil, err
			}
			totals = append(totals, MoneyTotal{MoneyUnit: units[uid], Amount: value.Round(0).StringFixed(0)})
		}
		result[def.Column] = totals
	}
	return result, nil
}
