package information

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/posting"
	"github.com/alex-bogatiuk/metapus/internal/domain/registers/accumulation"
)

// Store keeps the records of any information register in the table its
// schema names; the platform's PostgreSQL store implements it. dst is a
// pointer to a slice of the register's record type.
type Store interface {
	// Upsert writes record, replacing the one with its key.
	Upsert(ctx context.Context, schema *Schema, record Record) error
	// DeleteKey removes the record with the key of record.
	DeleteKey(ctx context.Context, schema *Schema, record Record) error
	// InsertRecords writes a document's records; a key another record holds
	// is refused.
	InsertRecords(ctx context.Context, schema *Schema, records []Record) error
	// DeleteRecorder removes the records of a document.
	DeleteRecorder(ctx context.Context, schema *Schema, recorderID id.ID) error
	// SelectSliceLast reads, per combination of dimensions the filter
	// selects, the latest record at or before at (every record, for a
	// non-periodic register).
	SelectSliceLast(ctx context.Context, schema *Schema, at time.Time, filter Filter, dst any) error
	// SelectRecords reads the records the filter selects in [from, to] by
	// period and dimensions; zero times leave that end open.
	SelectRecords(ctx context.Context, schema *Schema, filter Filter, from, to time.Time, dst any) error
	// SelectRecorder reads a document's records.
	SelectRecorder(ctx context.Context, schema *Schema, recorderID id.ID, dst any) error
}

// Filter selects combinations of dimensions: column → value or values.
type Filter = accumulation.Filter

// Declared is an information register as the platform sees it.
type Declared interface {
	Schema() *Schema
	Bind(store Store)
	posting.RegisterRecorder
	entity.MovementProvider
}

// Register is an information register whose records have type R. Declare it
// once as a package variable with New and register it with the
// configuration's registry (RegisterInformation), which binds its storage.
type Register[R any] struct {
	schema *Schema
	store  atomic.Pointer[storeBox]
}

type storeBox struct{ Store }

// New declares the register def with records of type R; it panics when the
// description is wrong.
func New[R any](def Definition) *Register[R] {
	if _, ok := any(new(R)).(Record); !ok {
		panic(fmt.Sprintf("information register %q: *%s has no Header method; embed information.RecordBase", def.Key, reflect.TypeFor[R]()))
	}
	schema, err := NewSchema(def, reflect.TypeFor[R]())
	if err != nil {
		panic(err.Error())
	}
	return &Register[R]{schema: schema}
}

// Schema describes the register's table and fields.
func (r *Register[R]) Schema() *Schema { return r.schema }

// Key is the register's key.
func (r *Register[R]) Key() string { return r.schema.Key }

// Bind gives the register its storage.
func (r *Register[R]) Bind(store Store) { r.store.Store(&storeBox{store}) }

func (r *Register[R]) storage() (Store, error) {
	box := r.store.Load()
	if box == nil {
		return nil, apperror.NewInternal(fmt.Errorf("information register %s is not bound to storage", r.schema.Key))
	}
	return box.Store, nil
}

// prepare checks a record's period and keeps it as the register keeps it.
func (r *Register[R]) prepare(record *R) error {
	h := any(record).(Record).Header()
	if !r.schema.Periodic() {
		h.Period = time.Time{}
		return nil
	}
	if h.Period.IsZero() {
		return apperror.NewValidation(fmt.Sprintf("a record of %s needs its period", r.schema.Label))
	}
	h.Period = r.schema.PeriodOf(h.Period)
	return nil
}

// Set writes record of an Independent register, replacing the record with
// the same period and dimensions.
func (r *Register[R]) Set(ctx context.Context, record R) error {
	if r.schema.Mode != Independent {
		return apperror.NewInternal(fmt.Errorf("register %s is written by documents, not Set", r.schema.Key))
	}
	if err := r.prepare(&record); err != nil {
		return err
	}
	store, err := r.storage()
	if err != nil {
		return err
	}
	if err := store.Upsert(ctx, r.schema, any(&record).(Record)); err != nil {
		return fmt.Errorf("set %s: %w", r.schema.Key, err)
	}
	return nil
}

// Delete removes the record of an Independent register with the period and
// dimensions of key.
func (r *Register[R]) Delete(ctx context.Context, key R) error {
	if r.schema.Mode != Independent {
		return apperror.NewInternal(fmt.Errorf("register %s is written by documents, not Delete", r.schema.Key))
	}
	if err := r.prepare(&key); err != nil {
		return err
	}
	store, err := r.storage()
	if err != nil {
		return err
	}
	return store.DeleteKey(ctx, r.schema, any(&key).(Record))
}

// Record adds record of a Recorder register to the document's records in
// out. The register fills the recorder; the period is the document's date
// unless record sets its own.
func (r *Register[R]) Record(out *accumulation.Movements, record R) {
	if r.schema.Mode != Recorder {
		out.Fail(fmt.Errorf("register %s is written with Set, not by documents", r.schema.Key))
		return
	}
	recorderID, recorderType, _, period := out.Recorder()
	h := any(&record).(Record).Header()
	h.RecorderID, h.RecorderType = recorderID, recorderType
	if h.Period.IsZero() {
		h.Period = period
	}
	accumulation.Collect(out, r.schema.Key, record)
}

// Name is the register's key: its place in a posting's movement set.
func (r *Register[R]) Name() string { return r.schema.Key }

// RecordFromSet writes the records a posting collected for the register.
func (r *Register[R]) RecordFromSet(ctx context.Context, set *posting.MovementSet) error {
	raw, ok := set.GetExtension(r.schema.Key)
	if !ok {
		return nil
	}
	plan, ok := raw.([]R)
	if !ok {
		return fmt.Errorf("register %s records have type %T", r.schema.Key, raw)
	}
	if len(plan) == 0 {
		return nil
	}
	store, err := r.storage()
	if err != nil {
		return err
	}
	rows := make([]R, len(plan))
	copy(rows, plan)
	records := make([]Record, len(rows))
	for i := range rows {
		if err := r.prepare(&rows[i]); err != nil {
			return err
		}
		records[i] = any(&rows[i]).(Record)
	}
	if err := store.InsertRecords(ctx, r.schema, records); err != nil {
		return fmt.Errorf("record %s: %w", r.schema.Key, err)
	}
	return nil
}

// ReverseMovements removes the document's records: reposting writes them
// anew, unposting leaves none.
func (r *Register[R]) ReverseMovements(ctx context.Context, recorderID id.ID, _ int) error {
	if r.schema.Mode != Recorder {
		return nil
	}
	store, err := r.storage()
	if err != nil {
		return err
	}
	return store.DeleteRecorder(ctx, r.schema, recorderID)
}

// SliceLast returns, per combination of dimensions the filter selects, the
// record valid at the moment at: the latest one at or before it (срез
// последних). A non-periodic register returns its records.
func (r *Register[R]) SliceLast(ctx context.Context, at time.Time, filter Filter) ([]R, error) {
	return r.query(ctx, filter, func(store Store, dst *[]R) error {
		return store.SelectSliceLast(ctx, r.schema, at, filter, dst)
	})
}

// Records returns the records the filter selects with periods in
// [from, to]; a zero time leaves that end open.
func (r *Register[R]) Records(ctx context.Context, filter Filter, from, to time.Time) ([]R, error) {
	return r.query(ctx, filter, func(store Store, dst *[]R) error {
		return store.SelectRecords(ctx, r.schema, filter, from, to, dst)
	})
}

// RecordsOf returns the records a document wrote.
func (r *Register[R]) RecordsOf(ctx context.Context, recorderID id.ID) ([]R, error) {
	store, err := r.storage()
	if err != nil {
		return nil, err
	}
	var result []R
	if err := store.SelectRecorder(ctx, r.schema, recorderID, &result); err != nil {
		return nil, fmt.Errorf("select %s records: %w", r.schema.Key, err)
	}
	return result, nil
}

func (r *Register[R]) query(ctx context.Context, filter Filter, run func(Store, *[]R) error) ([]R, error) {
	for column := range filter {
		if !r.schema.IsDimension(column) {
			return nil, apperror.NewValidation(fmt.Sprintf("register %s has no dimension %s", r.schema.Key, column))
		}
	}
	store, err := r.storage()
	if err != nil {
		return nil, err
	}
	var result []R
	if err := run(store, &result); err != nil {
		return nil, fmt.Errorf("query %s: %w", r.schema.Key, err)
	}
	return result, nil
}

// RegisterName is the register's name for people.
func (r *Register[R]) RegisterName() string { return r.schema.Label }

// GetDocumentMovements shows the records the document wrote, in the
// document's movements view; an Independent register shows none.
func (r *Register[R]) GetDocumentMovements(ctx context.Context, recorderID id.ID) ([]entity.DocumentMovement, error) {
	if r.schema.Mode != Recorder {
		return nil, nil
	}
	records, err := r.RecordsOf(ctx, recorderID)
	if err != nil {
		return nil, err
	}
	fields := append(append([]Field(nil), r.schema.Dimensions...), r.schema.Resources...)
	columns := make([]entity.MovementColumnDef, 0, len(fields))
	for _, f := range fields {
		columns = append(columns, entity.MovementColumnDef{
			Key: f.Column, PolicyField: f.Column, Label: f.Label, Type: string(f.Type), RefEntity: f.Ref, CurrencyRef: f.Currency,
		})
	}
	result := make([]entity.DocumentMovement, 0, len(records))
	for i := range records {
		v := reflect.ValueOf(&records[i]).Elem()
		data := make(map[string]any, len(fields))
		for _, f := range fields {
			value := v.FieldByIndex(f.index).Interface()
			if f.Type == FieldRef {
				ref := value.(id.ID).String()
				value = entity.MovementRefValue{ID: ref, Name: ref}
			}
			data[f.Column] = value
		}
		result = append(result, entity.DocumentMovement{
			PolicyEntity: r.schema.Key,
			RegisterName: r.schema.Label,
			Period:       any(&records[i]).(Record).Header().Period,
			Columns:      columns,
			Data:         data,
		})
	}
	return result, nil
}

// Install adds the Recorder registers among registers to engine: the
// visitor that collects documents' records (shared with accumulation
// registers) and a recorder per register. It returns them as providers of
// the document movements view.
func Install(engine *posting.Engine, registers ...Declared) []entity.MovementProvider {
	var providers []entity.MovementProvider
	for _, r := range registers {
		if r.Schema().Mode != Recorder {
			continue
		}
		if providers == nil {
			engine.AddVisitor(accumulation.Visitor{})
		}
		engine.AddRecorder(r)
		providers = append(providers, r)
	}
	return providers
}

var (
	_ posting.RegisterRecorder = (*Register[RecordBase])(nil)
	_ entity.MovementProvider  = (*Register[RecordBase])(nil)
	_ Declared                 = (*Register[RecordBase])(nil)
)
