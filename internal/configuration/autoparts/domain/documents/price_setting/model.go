// Package price_setting is the price setting document (Установка цен
// номенклатуры): the selling prices of items from the document's date on.
// Posting records them in the item prices register; reposting replaces
// them, unposting removes them.
package price_setting

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/item_prices"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// Definition is shared by persistence, service, registration and wire contracts.
var Definition = entity.Definition{
	Key: "price_setting", Name: "PriceSetting", Route: "price-setting", Table: "doc_price_settings",
	Kind: entity.DocumentKind,
	// A document without a number gets the next one of its yearly series (УЦ-2026-00001).
	Number: entity.NumberRule{Prefix: "УЦ"},
	Parts:  []entity.Part{{Key: "prices", Field: "prices"}},
}

// PriceSetting sets item prices in one currency.
type PriceSetting struct {
	entity.Document
	CurrencyID id.ID       `db:"currency_id" json:"currencyId" meta:"label:Валюта,ref:currency" binding:"required"`
	Prices     []PriceLine `db:"-" json:"prices" meta:"label:Цены"`
}

// PriceLine is a row of the prices table part (doc_price_settings__prices).
type PriceLine struct {
	LineID id.ID            `db:"line_id" json:"lineId"`
	LineNo int              `db:"line_no" json:"lineNo" meta:"label:№"`
	ItemID id.ID            `db:"item_id" json:"itemId" meta:"label:Номенклатура,ref:item" binding:"required"`
	Price  types.MinorUnits `db:"price" json:"price" meta:"label:Цена"`
}

func (PriceSetting) EntityDefinition() entity.Definition { return Definition }

func (e *PriceSetting) GetDocumentType() string { return Definition.Name }

func (e *PriceSetting) Validate(ctx context.Context) error {
	if err := e.Document.Validate(ctx); err != nil {
		return err
	}
	seen := map[id.ID]int{}
	for _, line := range e.Prices {
		if !line.Price.IsPositive() {
			return apperror.NewValidation(fmt.Sprintf("line %d: the price must be positive", line.LineNo))
		}
		if first, ok := seen[line.ItemID]; ok {
			return apperror.NewValidation(fmt.Sprintf("line %d: the item already has a price in line %d", line.LineNo, first))
		}
		seen[line.ItemID] = line.LineNo
	}
	return nil
}

// CanPost checks the document before posting.
func (e *PriceSetting) CanPost(ctx context.Context) error {
	if err := e.Document.CanPost(ctx); err != nil {
		return err
	}
	if len(e.Prices) == 0 {
		return apperror.NewValidation("a price setting needs at least one line")
	}
	return e.Validate(ctx)
}

// RegisterMovements records every line's price from the document's date.
func (e *PriceSetting) RegisterMovements(_ context.Context, out *accumulation.Movements) error {
	for _, line := range e.Prices {
		item_prices.Register.Record(out, item_prices.Record{ItemID: line.ItemID, Price: line.Price, CurrencyID: e.CurrencyID})
	}
	return nil
}

// GetLineCount sizes the movement set.
func (e *PriceSetting) GetLineCount() int { return len(e.Prices) }

var _ accumulation.Source = (*PriceSetting)(nil)
