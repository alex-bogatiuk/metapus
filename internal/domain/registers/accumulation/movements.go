package accumulation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/posting"
)

// Source is implemented by a document that moves declarative registers.
// RegisterMovements adds the document's movements to out with each
// register's Receipt, Expense or Turnover. It is a pure function of the
// document: the same document gives the same movements.
//
//	func (d *GoodsReceipt) RegisterMovements(_ context.Context, out *accumulation.Movements) error {
//		for _, line := range d.Goods {
//			warehouse_stock.Register.Receipt(out, warehouse_stock.Movement{WarehouseID: d.WarehouseID, ItemID: line.ItemID, Quantity: line.Quantity})
//		}
//		return nil
//	}
type Source interface {
	RegisterMovements(ctx context.Context, out *Movements) error
}

// Movements collects a document's movements in all declarative registers.
// The registers fill each movement's recorder, version and record type; the
// period is the document's date unless the movement sets its own.
type Movements struct {
	recorderID      id.ID
	recorderType    string
	recorderVersion int
	period          time.Time
	byKey           map[string]any
	keys            []string
	errs            []error
}

// NewMovements starts the movements of the posting of recorderType
// recorderID as its version recorderVersion, dated period.
func NewMovements(recorderID id.ID, recorderType string, recorderVersion int, period time.Time) *Movements {
	return &Movements{recorderID: recorderID, recorderType: recorderType, recorderVersion: recorderVersion, period: period, byKey: map[string]any{}}
}

func (m *Movements) fail(err error) { m.errs = append(m.errs, err) }

// Err is the first mistake made while adding movements.
func (m *Movements) Err() error { return errors.Join(m.errs...) }

// collected marks a movement set whose declarative movements are in.
const collected = "accumulation.collected"

// Visitor collects the movements of a document that implements Source into
// the posting's movement set, each register's under its key. Installing it
// twice collects them once.
type Visitor struct{}

func (Visitor) Name() string { return "accumulation" }

func (Visitor) CollectMovements(ctx context.Context, doc posting.Postable, set *posting.MovementSet) error {
	source, ok := doc.(Source)
	if !ok {
		return nil
	}
	if _, done := set.GetExtension(collected); done {
		return nil
	}
	var period time.Time
	if dated, ok := doc.(interface{ GetDate() time.Time }); ok {
		period = dated.GetDate()
	}
	out := NewMovements(doc.GetID(), doc.GetDocumentType(), doc.GetPostedVersion()+1, period)
	if err := source.RegisterMovements(ctx, out); err != nil {
		return fmt.Errorf("generate register movements: %w", err)
	}
	if err := out.Err(); err != nil {
		return fmt.Errorf("generate register movements: %w", err)
	}
	for _, key := range out.keys {
		set.SetExtension(key, out.byKey[key])
	}
	set.SetExtension(collected, true)
	return nil
}

// Install adds registers to engine: the visitor that collects their
// movements and a recorder per register. It returns the registers as the
// providers of the document movements view.
func Install(engine *posting.Engine, registers ...Declared) []entity.MovementProvider {
	if len(registers) == 0 {
		return nil
	}
	engine.AddVisitor(Visitor{})
	providers := make([]entity.MovementProvider, 0, len(registers))
	for _, r := range registers {
		engine.AddRecorder(r)
		providers = append(providers, r)
	}
	return providers
}

var _ posting.RegisterVisitor = Visitor{}
