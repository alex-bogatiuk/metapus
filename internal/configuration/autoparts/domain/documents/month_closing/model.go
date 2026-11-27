// Package month_closing is the month closing (Закрытие месяца): it costs the
// month's sales at the month's average cost of each item at each warehouse
// (средняя за месяц) in the accounting currency.
//
// For an item at a warehouse the average is the cost of the stock at the
// month's start (the register of the stock's cost, which only closings
// write) plus the month's purchases, over their quantity. Posting the
// closing puts the month's purchases into the stock's cost, takes the
// month's sales out of it at the average, and writes the cost of each
// customer's sales to the sales register; selling all of the stock takes
// all of its cost.
//
// The closing is calculated when it is posted, from the registers as they
// are then: its movements are a function of the month's documents, and
// reposting it recalculates the month. Goods receipts and issues are in
// the cost sequence (inventory_cost.Sequence): changing one in a closed
// month moves the sequence back, and the closings from that month on must
// be posted again, in order.
package month_closing

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/inventory_cost"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/purchases"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/sales"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// Definition is shared by persistence, service, registration and wire contracts.
var Definition = entity.Definition{
	Key: "month_closing", Name: "MonthClosing", Route: "month-closing", Table: "doc_month_closings",
	Kind: entity.DocumentKind,
	// A document without a number gets the next one of its yearly series (ЗМ-2026-00001).
	Number:   entity.NumberRule{Prefix: "ЗМ"},
	Sequence: entity.SequenceRule{Restores: inventory_cost.Sequence},
}

// Error codes of the month closing.
const (
	// CodeOtherCurrency: the month has purchases, or the stock has cost,
	// in a currency other than the closing's accounting currency.
	CodeOtherCurrency = "MONTH_CLOSING_OTHER_CURRENCY"
	// CodeAlreadyClosed: another posted closing has costed the month.
	CodeAlreadyClosed = "MONTH_CLOSING_ALREADY_CLOSED"
	// CodeStockShort: more was sold than the stock's cost covers, because
	// an earlier month with stock is not closed.
	CodeStockShort = "MONTH_CLOSING_STOCK_SHORT"
)

// MonthClosing closes the month of its date.
type MonthClosing struct {
	entity.Document
	// CurrencyID is the accounting currency (валюта учёта) the cost is kept in.
	CurrencyID id.ID `db:"currency_id" json:"currencyId" meta:"label:Валюта учёта,ref:currency" binding:"required"`
}

func (MonthClosing) EntityDefinition() entity.Definition { return Definition }

func (e *MonthClosing) GetDocumentType() string { return Definition.Name }

// SequencePeriod is the calendar month (UTC) of the closing's date.
func (e *MonthClosing) SequencePeriod() (from, to time.Time) {
	date := e.Date.UTC()
	from = time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, time.UTC)
	return from, from.AddDate(0, 1, 0)
}

// CanPost checks the closing before posting; the calculation is the
// posting's.
func (e *MonthClosing) CanPost(ctx context.Context) error {
	if err := e.Document.CanPost(ctx); err != nil {
		return err
	}
	if id.IsNil(e.CurrencyID) {
		return apperror.NewValidation("the accounting currency is required")
	}
	return nil
}

// stockKey is an item at a warehouse.
type stockKey struct{ warehouse, item id.ID }

// costing is what the month does to an item at a warehouse.
type costing struct {
	openQuantity, receivedQuantity types.Quantity
	openCost, receivedCost         types.MinorUnits
	// sold is the quantity sold to each customer, in the order first sold.
	sold      map[id.ID]types.Quantity
	customers []id.ID
}

// RegisterMovements calculates the month: its purchases go into the stock's
// cost, its sales come out at the average cost and their cost goes to the
// sales register, all at the month's last moment.
func (e *MonthClosing) RegisterMovements(ctx context.Context, out *accumulation.Movements) error {
	from, to := e.SequencePeriod()
	at := to.Add(-time.Microsecond)
	month := map[stockKey]*costing{}
	var order []stockKey
	of := func(warehouse, item id.ID) *costing {
		key := stockKey{warehouse, item}
		c, ok := month[key]
		if !ok {
			c = &costing{sold: map[id.ID]types.Quantity{}}
			month[key] = c
			order = append(order, key)
		}
		return c
	}

	opening, err := inventory_cost.Register.BalancesAt(ctx, from, nil)
	if err != nil {
		return err
	}
	for _, b := range opening {
		if b.CurrencyID != e.CurrencyID {
			return apperror.NewBusinessRule(CodeOtherCurrency, "Себестоимость запасов на начало месяца ведётся в другой валюте учёта")
		}
		c := of(b.WarehouseID, b.ItemID)
		c.openQuantity += b.Quantity
		c.openCost += b.Amount
	}
	if err := e.refuseClosedMonth(ctx, from, to); err != nil {
		return err
	}
	bought, err := purchases.Register.Turnovers(ctx, from, to, nil)
	if err != nil {
		return err
	}
	for _, p := range bought {
		if p.CurrencyID != e.CurrencyID {
			return apperror.NewBusinessRule(CodeOtherCurrency, "В месяце есть поступления в другой валюте: себестоимость считается в одной валюте учёта")
		}
		c := of(p.WarehouseID, p.ItemID)
		c.receivedQuantity += p.Quantity
		c.receivedCost += p.Amount
	}
	sold, err := sales.Register.Turnovers(ctx, from, to, nil)
	if err != nil {
		return err
	}
	for _, s := range sold {
		if !s.Quantity.IsPositive() {
			continue
		}
		c := of(s.WarehouseID, s.ItemID)
		if _, ok := c.sold[s.CustomerID]; !ok {
			c.customers = append(c.customers, s.CustomerID)
		}
		c.sold[s.CustomerID] += s.Quantity
	}

	for _, key := range order {
		c := month[key]
		if c.receivedQuantity != 0 || c.receivedCost != 0 {
			inventory_cost.Register.Receipt(out, inventory_cost.Movement{
				MovementBase: entity.MovementBase{Period: at},
				WarehouseID:  key.warehouse, ItemID: key.item, CurrencyID: e.CurrencyID,
				Quantity: c.receivedQuantity, Amount: c.receivedCost,
			})
		}
		var soldTotal types.Quantity
		for _, q := range c.sold {
			soldTotal += q
		}
		if soldTotal == 0 {
			continue
		}
		available := c.openQuantity + c.receivedQuantity
		if soldTotal > available {
			return apperror.NewBusinessRule(CodeStockShort,
				"Продано больше, чем учтено в себестоимости запасов: закройте предыдущие месяцы по порядку").
				WithDetail("warehouseId", key.warehouse).WithDetail("itemId", key.item)
		}
		cost := share(c.openCost+c.receivedCost, soldTotal, available)
		inventory_cost.Register.Expense(out, inventory_cost.Movement{
			MovementBase: entity.MovementBase{Period: at},
			WarehouseID:  key.warehouse, ItemID: key.item, CurrencyID: e.CurrencyID,
			Quantity: soldTotal, Amount: cost,
		})
		// Each customer's sales take their share of the cost; the last
		// takes what is left, so the shares add up to it.
		left, leftQuantity := cost, soldTotal
		for _, customer := range c.customers {
			part := share(left, c.sold[customer], leftQuantity)
			left, leftQuantity = left-part, leftQuantity-c.sold[customer]
			if part == 0 {
				continue
			}
			sales.Register.Turnover(out, sales.Movement{
				MovementBase: entity.MovementBase{Period: at},
				WarehouseID:  key.warehouse, ItemID: key.item, CustomerID: customer, CurrencyID: e.CurrencyID,
				Cost: part,
			})
		}
	}
	return nil
}

// refuseClosedMonth refuses a month another posted closing has costed: the
// month has cost movements besides the closing's own. The posting reads
// the movements before it reverses the closing's earlier ones, so those are
// left out.
func (e *MonthClosing) refuseClosedMonth(ctx context.Context, from, to time.Time) error {
	closed := apperror.NewBusinessRule(CodeAlreadyClosed, fmt.Sprintf("Месяц %s уже закрыт другим документом", from.Format("01.2006")))
	costed, err := sales.Register.Turnovers(ctx, from, to, nil)
	if err != nil {
		return err
	}
	ownSales, err := sales.Register.Movements(ctx, e.ID)
	if err != nil {
		return err
	}
	var cost types.MinorUnits
	for _, s := range costed {
		cost += s.Cost
	}
	for _, s := range ownSales {
		cost -= s.Cost
	}
	if cost != 0 {
		return closed
	}
	stocked, err := inventory_cost.Register.Turnovers(ctx, from, to, nil)
	if err != nil {
		return err
	}
	ownStock, err := inventory_cost.Register.Movements(ctx, e.ID)
	if err != nil {
		return err
	}
	var quantity types.Quantity
	var amount types.MinorUnits
	for _, s := range stocked {
		quantity, amount = quantity+s.Quantity, amount+s.Amount
	}
	for _, s := range ownStock {
		if s.RecordType == entity.RecordTypeExpense {
			quantity, amount = quantity+s.Quantity, amount+s.Amount
		} else {
			quantity, amount = quantity-s.Quantity, amount-s.Amount
		}
	}
	if quantity != 0 || amount != 0 {
		return closed
	}
	return nil
}

// share is amount × part / whole, rounded half away from zero; the whole
// amount when part is the whole.
func share(amount types.MinorUnits, part, whole types.Quantity) types.MinorUnits {
	if part == whole {
		return amount
	}
	product := new(big.Int).Mul(big.NewInt(int64(amount)), big.NewInt(int64(part)))
	quotient, remainder := new(big.Int).QuoRem(product, big.NewInt(int64(whole)), new(big.Int))
	if new(big.Int).Abs(new(big.Int).Mul(remainder, big.NewInt(2))).Cmp(big.NewInt(int64(whole))) >= 0 {
		if product.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	return types.MinorUnits(quotient.Int64())
}

// GetLineCount sizes the movement set.
func (e *MonthClosing) GetLineCount() int { return 0 }

var (
	_ accumulation.Source   = (*MonthClosing)(nil)
	_ entity.SequencePeriod = (*MonthClosing)(nil)
)
