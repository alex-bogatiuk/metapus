package accumulation

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/domain/posting"
)

// Store keeps the movements and balances of any declarative register. The
// platform's PostgreSQL store implements it; it holds no state of its own
// and takes the transaction from the context. dst is a pointer to a slice of
// the register's movement type.
type Store interface {
	CreateMovements(ctx context.Context, schema *Schema, movements []Movement) error
	DeleteMovements(ctx context.Context, schema *Schema, recorderID id.ID, beforeVersion int) error
	SelectMovements(ctx context.Context, schema *Schema, recorderID id.ID, dst any) error
	SelectBalances(ctx context.Context, schema *Schema, filter Filter, dst any) error
	SelectBalancesAt(ctx context.Context, schema *Schema, at time.Time, filter Filter, dst any) error
	SelectTurnovers(ctx context.Context, schema *Schema, from, to time.Time, filter Filter, dst any) error
	// SelectRecorderTotals sums the document's movements per combination of
	// dimensions, receipts less expenses.
	SelectRecorderTotals(ctx context.Context, schema *Schema, recorderID id.ID, dst any) error
	// SelectBalancesOf reads the balances rows of the given combinations,
	// each the dimension values in schema order; a combination without a row
	// is left out.
	SelectBalancesOf(ctx context.Context, schema *Schema, combinations [][]any, dst any) error
	// LockCombinations takes a transaction lock on each key in the order
	// given; the posting sorts the keys of all registers it moves.
	LockCombinations(ctx context.Context, keys []int64) error
}

// Filter selects combinations of dimensions: column → value, or a slice of
// values for any of them. An empty filter selects all.
type Filter map[string]any

// Declared is a register as the platform sees it, whatever its movement
// type: it binds to the store, records postings and shows a document's
// movements. Only New makes one.
type Declared interface {
	Schema() *Schema
	Bind(store Store)
	BindNames(names domain.RefResolver)
	posting.RegisterRecorder
	entity.MovementProvider
	planControl(ctx context.Context, doc posting.Postable, unposting bool) (*controlPlan, error)
}

// Register is an accumulation register whose movements have type M.
// Declare it once as a package variable with New and register it with the
// configuration's registry (RegisterAccumulation), which binds its storage.
type Register[M any] struct {
	schema *Schema
	store  atomic.Pointer[storeBox]
	names  atomic.Pointer[namesBox]
}

type storeBox struct{ Store }

type namesBox struct{ domain.RefResolver }

// BindNames lets the register name the references in its messages, such as
// the warehouse and item of a shortage, as the user may read them. Without
// it the messages show their IDs.
func (r *Register[M]) BindNames(names domain.RefResolver) {
	r.names.Store(&namesBox{names})
}

func (r *Register[M]) resolver() domain.RefResolver {
	if box := r.names.Load(); box != nil {
		return box.RefResolver
	}
	return nil
}

// New declares the register def with movements of type M. It panics when
// the description is wrong, as a mistake in a declaration found at start-up.
func New[M any](def Definition) *Register[M] {
	if _, ok := any(new(M)).(Movement); !ok {
		panic(fmt.Sprintf("accumulation register %q: *%s has no Header method; embed entity.MovementBase", def.Key, reflect.TypeFor[M]()))
	}
	schema, err := NewSchema(def, reflect.TypeFor[M]())
	if err != nil {
		panic(err.Error())
	}
	return &Register[M]{schema: schema}
}

// Schema describes the register's tables and fields.
func (r *Register[M]) Schema() *Schema { return r.schema }

// Key is the register's key.
func (r *Register[M]) Key() string { return r.schema.Key }

// Bind gives the register its storage. The registry binds every register it
// is given; binding again replaces the store.
func (r *Register[M]) Bind(store Store) { r.store.Store(&storeBox{store}) }

func (r *Register[M]) storage() (Store, error) {
	box := r.store.Load()
	if box == nil {
		return nil, apperror.NewInternal(fmt.Errorf("accumulation register %s is not bound to storage", r.schema.Key))
	}
	return box.Store, nil
}

// Receipt adds m to the register's balance in out: the document's movements.
// The recorder and period come from out, unless m sets its own period.
func (r *Register[M]) Receipt(out *Movements, m M) { r.add(out, entity.RecordTypeReceipt, m) }

// Expense takes m away from the register's balance in out. A Turnovers
// register has no expense; use Turnover.
func (r *Register[M]) Expense(out *Movements, m M) {
	if r.schema.Kind != Balances {
		out.fail(fmt.Errorf("turnovers register %s has no expenses", r.schema.Key))
		return
	}
	r.add(out, entity.RecordTypeExpense, m)
}

// Turnover adds m to a Turnovers register in out.
func (r *Register[M]) Turnover(out *Movements, m M) { r.add(out, entity.RecordTypeReceipt, m) }

func (r *Register[M]) add(out *Movements, recordType entity.RecordType, m M) {
	h := any(&m).(Movement).Header()
	period := h.Period
	if period.IsZero() {
		period = out.period
	}
	*h = entity.NewMovementBase(out.recorderID, out.recorderType, out.recorderVersion, period, recordType)
	list, _ := out.byKey[r.schema.Key].([]M)
	if list == nil {
		out.keys = append(out.keys, r.schema.Key)
	}
	out.byKey[r.schema.Key] = append(list, m)
}

// Name is the register's key: its place in a posting's movement set.
func (r *Register[M]) Name() string { return r.schema.Key }

// RecordFromSet writes the movements a posting collected for the register.
func (r *Register[M]) RecordFromSet(ctx context.Context, set *posting.MovementSet) error {
	raw, ok := set.GetExtension(r.schema.Key)
	if !ok {
		return nil
	}
	plan, ok := raw.([]M)
	if !ok {
		return fmt.Errorf("register %s movements have type %T", r.schema.Key, raw)
	}
	if len(plan) == 0 {
		return nil
	}
	store, err := r.storage()
	if err != nil {
		return err
	}
	// The plan stays as generated; the store stamps copies.
	rows := make([]M, len(plan))
	copy(rows, plan)
	movements := make([]Movement, len(rows))
	for i := range rows {
		movements[i] = any(&rows[i]).(Movement)
	}
	if err := store.CreateMovements(ctx, r.schema, movements); err != nil {
		return fmt.Errorf("record %s movements: %w", r.schema.Key, err)
	}
	return nil
}

// ReverseMovements removes the movements of the document's earlier
// postings; the balances follow them.
func (r *Register[M]) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	store, err := r.storage()
	if err != nil {
		return err
	}
	if err := store.DeleteMovements(ctx, r.schema, recorderID, beforeVersion); err != nil {
		return fmt.Errorf("reverse %s movements: %w", r.schema.Key, err)
	}
	return nil
}

// Movements returns the movements of the document recorderID.
func (r *Register[M]) Movements(ctx context.Context, recorderID id.ID) ([]M, error) {
	store, err := r.storage()
	if err != nil {
		return nil, err
	}
	var result []M
	if err := store.SelectMovements(ctx, r.schema, recorderID, &result); err != nil {
		return nil, fmt.Errorf("select %s movements: %w", r.schema.Key, err)
	}
	return result, nil
}

// Balances returns the current balances of the combinations filter selects,
// one movement-shaped row per combination with its dimensions and resources;
// combinations whose resources are all zero are left out.
func (r *Register[M]) Balances(ctx context.Context, filter Filter) ([]M, error) {
	if r.schema.Kind != Balances {
		return nil, apperror.NewValidation(fmt.Sprintf("turnovers register %s has no balances", r.schema.Key))
	}
	return r.query(ctx, filter, func(store Store, dst *[]M) error {
		return store.SelectBalances(ctx, r.schema, filter, dst)
	})
}

// BalancesAt returns the balances at the moment at: the movements before it.
func (r *Register[M]) BalancesAt(ctx context.Context, at time.Time, filter Filter) ([]M, error) {
	if r.schema.Kind != Balances {
		return nil, apperror.NewValidation(fmt.Sprintf("turnovers register %s has no balances", r.schema.Key))
	}
	return r.query(ctx, filter, func(store Store, dst *[]M) error {
		return store.SelectBalancesAt(ctx, r.schema, at, filter, dst)
	})
}

// Turnovers returns the change of each combination over [from, to): the
// receipts less the expenses, or the sum of a Turnovers register.
func (r *Register[M]) Turnovers(ctx context.Context, from, to time.Time, filter Filter) ([]M, error) {
	if !from.Before(to) {
		return nil, apperror.NewValidation("the period start must precede its end")
	}
	return r.query(ctx, filter, func(store Store, dst *[]M) error {
		return store.SelectTurnovers(ctx, r.schema, from, to, filter, dst)
	})
}

func (r *Register[M]) query(ctx context.Context, filter Filter, run func(Store, *[]M) error) ([]M, error) {
	for column := range filter {
		if !r.schema.IsDimension(column) {
			return nil, apperror.NewValidation(fmt.Sprintf("register %s has no dimension %s", r.schema.Key, column))
		}
	}
	store, err := r.storage()
	if err != nil {
		return nil, err
	}
	var result []M
	if err := run(store, &result); err != nil {
		return nil, fmt.Errorf("query %s: %w", r.schema.Key, err)
	}
	return result, nil
}

// RegisterName is the register's name for people.
func (r *Register[M]) RegisterName() string { return r.schema.Label }

// GetDocumentMovements shows the document's movements in the register.
func (r *Register[M]) GetDocumentMovements(ctx context.Context, recorderID id.ID) ([]entity.DocumentMovement, error) {
	movements, err := r.Movements(ctx, recorderID)
	if err != nil {
		return nil, err
	}
	columns := make([]entity.MovementColumnDef, 0, len(r.schema.Dimensions)+len(r.schema.Resources))
	for _, f := range append(append([]Field(nil), r.schema.Dimensions...), r.schema.Resources...) {
		columns = append(columns, entity.MovementColumnDef{
			Key: f.Column, PolicyField: f.Column, Label: f.Label, Type: string(f.Type), RefEntity: f.Ref, CurrencyRef: f.Currency,
		})
	}
	result := make([]entity.DocumentMovement, 0, len(movements))
	for i := range movements {
		v := reflect.ValueOf(&movements[i]).Elem()
		h := any(&movements[i]).(Movement).Header()
		data := make(map[string]any, len(columns))
		for _, f := range r.schema.Dimensions {
			value := v.FieldByIndex(f.index).Interface()
			if f.Type == FieldRef {
				ref := value.(id.ID).String()
				value = entity.MovementRefValue{ID: ref, Name: ref}
			}
			data[f.Column] = value
		}
		for _, f := range r.schema.Resources {
			data[f.Column] = v.FieldByIndex(f.index).Interface()
		}
		result = append(result, entity.DocumentMovement{
			PolicyEntity: r.schema.Key,
			RegisterName: r.schema.Label,
			RecordType:   string(h.RecordType),
			Period:       h.Period,
			Columns:      columns,
			Data:         data,
		})
	}
	return result, nil
}

var (
	_ posting.RegisterRecorder = (*Register[entity.MovementBase])(nil)
	_ entity.MovementProvider  = (*Register[entity.MovementBase])(nil)
	_ Declared                 = (*Register[entity.MovementBase])(nil)
)
