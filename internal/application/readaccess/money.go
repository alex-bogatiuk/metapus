package readaccess

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
)

// CurrencyUnit contains only independently authorized unit fields. A missing
// field is different from a valid zero scale and never implies scale two.
type CurrencyUnit struct {
	ID            id.ID
	Code          string
	DecimalPlaces int
}

func CurrencyUnitFromProjection(row Projection) (CurrencyUnit, bool) {
	code, hasCode := row.Fields["isoCode"].(string)
	scale, hasScale := row.Fields["decimalPlaces"].(int)
	if !hasCode || code == "" || !hasScale || scale < 0 || scale > 18 {
		return CurrencyUnit{}, false
	}
	return CurrencyUnit{row.ID, code, scale}, true
}

// MoneyFields is a server-declared mapping of an authorized source projection.
type MoneyFields struct{ Amount, CurrencyID, CurrencyEntity string }
type MoneyProjection struct {
	Amount types.MinorUnits
	Unit   CurrencyUnit
}

// ReadMoney expands units in one batch, using each currency's RBAC/RLS/FLS/CEL.
// Hidden source fields or inaccessible units omit the amount altogether.
func (s *Service) ReadMoney(ctx context.Context, rows []Projection, fields MoneyFields) (map[id.ID]MoneyProjection, error) {
	type candidate struct {
		source, currency id.ID
		amount           types.MinorUnits
	}
	var candidates []candidate
	var ids []id.ID
	result := make(map[id.ID]MoneyProjection)
	for _, row := range rows {
		value := row.Fields[fields.Amount]
		currency, ok := row.Fields[fields.CurrencyID].(id.ID)
		if value == nil || !ok || id.IsNil(currency) {
			continue
		}
		amount, ok := value.(types.MinorUnits)
		if !ok {
			return nil, fmt.Errorf("invalid monetary projection %s: %T", fields.Amount, value)
		}
		candidates = append(candidates, candidate{row.ID, currency, amount})
		ids = append(ids, currency)
	}
	if len(ids) == 0 {
		return result, nil
	}
	if _, err := s.Require(ctx, fields.CurrencyEntity); err != nil {
		if app, ok := apperror.AsAppError(err); ok && app.Code == apperror.CodeForbidden {
			return result, nil
		}
		return nil, err
	}
	currencies, err := s.ReadIDs(ctx, fields.CurrencyEntity, ids)
	if err != nil {
		return nil, err
	}
	units := make(map[id.ID]CurrencyUnit, len(currencies))
	for _, row := range currencies {
		if unit, ok := CurrencyUnitFromProjection(row); ok {
			units[row.ID] = unit
		}
	}
	for _, item := range candidates {
		if unit, ok := units[item.currency]; ok {
			result[item.source] = MoneyProjection{item.amount, unit}
		}
	}
	return result, nil
}
