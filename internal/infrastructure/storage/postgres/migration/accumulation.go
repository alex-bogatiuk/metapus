package migration

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/domain/registers/accumulation"
)

// WithAccumulationRegisters makes Run check, after the migrations, that the
// tenant database holds the tables of the given declarative registers as
// they are declared (CheckAccumulationRegisters). Pass the registry's
// registers from the composition root.
func WithAccumulationRegisters(schemas []*accumulation.Schema) Option {
	return func(o *options) { o.registers = append(o.registers, schemas...) }
}

// CheckAccumulationRegisters compares each register's declaration with the
// database: the movements table and, for a Balances register, the balances
// table with their columns and types, the balances' primary key, and the
// triggers that keep the balances and refuse updates of movements. A
// mismatch names every difference and shows the SQL the declaration expects
// (AccumulationDDL), so a migration that drifted from its
// register fails the tenant migration instead of the first posting.
func CheckAccumulationRegisters(ctx context.Context, db *sql.DB, schemas []*accumulation.Schema) error {
	var errs []error
	for _, schema := range schemas {
		problems, err := accumulationProblems(ctx, db, schema)
		if err != nil {
			return fmt.Errorf("check register %s: %w", schema.Key, err)
		}
		if len(problems) > 0 {
			errs = append(errs, fmt.Errorf("register %s does not match its tables:\n  - %s\nexpected:\n%s",
				schema.Key, strings.Join(problems, "\n  - "), AccumulationDDL(schema)))
		}
	}
	return errors.Join(errs...)
}

type column struct{ name, dataType string }

func accumulationProblems(ctx context.Context, db *sql.DB, schema *accumulation.Schema) ([]string, error) {
	var problems []string
	movements := []column{
		{"line_id", "uuid"}, {"recorder_id", "uuid"}, {"recorder_type", "text"}, {"recorder_version", "integer"},
		{"period", "timestamp with time zone"}, {"record_type", "text"}, {"created_at", "timestamp with time zone"},
	}
	for _, f := range schema.Dimensions {
		movements = append(movements, column{f.Column, f.Type.SQLType()})
	}
	for _, f := range schema.Resources {
		movements = append(movements, column{f.Column, "bigint"})
	}
	p, err := tableProblems(ctx, db, schema.MovementsTable(), movements)
	if err != nil {
		return nil, err
	}
	problems = append(problems, p...)

	triggers, err := tableTriggers(ctx, db, schema.MovementsTable())
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(triggers, func(t trigger) bool {
		return t.function == "sys_accumulation_movements_immutable" && t.on(triggerUpdate) && t.before()
	}) {
		problems = append(problems, fmt.Sprintf("%s has no BEFORE UPDATE trigger calling sys_accumulation_movements_immutable()", schema.MovementsTable()))
	}
	if schema.Kind != accumulation.Balances {
		return problems, nil
	}

	balances := make([]column, 0, len(schema.Dimensions)+len(schema.Resources)+1)
	for _, f := range schema.Dimensions {
		balances = append(balances, column{f.Column, f.Type.SQLType()})
	}
	for _, f := range schema.Resources {
		balances = append(balances, column{f.Column, "bigint"})
	}
	balances = append(balances, column{"updated_at", "timestamp with time zone"})
	p, err = tableProblems(ctx, db, schema.BalancesTable(), balances)
	if err != nil {
		return nil, err
	}
	problems = append(problems, p...)
	key, err := primaryKey(ctx, db, schema.BalancesTable())
	if err != nil {
		return nil, err
	}
	if want := slices.Sorted(slices.Values(schema.DimensionColumns())); !slices.Equal(key, want) {
		problems = append(problems, fmt.Sprintf("the primary key of %s is (%s), not the dimensions (%s)",
			schema.BalancesTable(), strings.Join(key, ", "), strings.Join(want, ", ")))
	}
	args := TriggerArguments(schema)
	for _, event := range []struct {
		name string
		bit  int
	}{{"INSERT", triggerInsert}, {"DELETE", triggerDelete}} {
		if !slices.ContainsFunc(triggers, func(t trigger) bool {
			return t.function == "sys_accumulation_balances" && t.on(event.bit) && !t.before() && !t.row() && slices.Equal(t.args, args[:])
		}) {
			problems = append(problems, fmt.Sprintf("%s has no AFTER %s statement trigger calling sys_accumulation_balances('%s', '%s', '%s')",
				schema.MovementsTable(), event.name, args[0], args[1], args[2]))
		}
	}
	return problems, nil
}

// tableProblems compares the table's columns with want: each wanted column
// exists, NOT NULL, with its type, and the table has no other column. TEXT
// and VARCHAR are alike.
func tableProblems(ctx context.Context, db *sql.DB, table string, want []column) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT column_name, data_type, is_nullable = 'YES'
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1`, table)
	if err != nil {
		return nil, err
	}
	type actual struct {
		dataType string
		nullable bool
	}
	have := map[string]actual{}
	for rows.Next() {
		var name string
		var a actual
		if err := rows.Scan(&name, &a.dataType, &a.nullable); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if a.dataType == "character varying" {
			a.dataType = "text"
		}
		have[name] = a
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	if len(have) == 0 {
		return []string{fmt.Sprintf("table %s does not exist", table)}, nil
	}
	var problems []string
	for _, c := range want {
		a, ok := have[c.name]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%s has no column %s", table, c.name))
		case a.dataType != c.dataType:
			problems = append(problems, fmt.Sprintf("%s.%s is %s, not %s", table, c.name, a.dataType, c.dataType))
		case a.nullable:
			problems = append(problems, fmt.Sprintf("%s.%s is nullable", table, c.name))
		}
		delete(have, c.name)
	}
	for _, name := range slices.Sorted(maps.Keys(have)) {
		problems = append(problems, fmt.Sprintf("%s has column %s the register does not declare", table, name))
	}
	return problems, nil
}

func primaryKey(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY (i.indkey)
		WHERE i.indrelid = to_regclass($1) AND i.indisprimary
		ORDER BY a.attname`, table)
	if err != nil {
		return nil, err
	}
	var key []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return nil, err
		}
		key = append(key, name)
	}
	return key, errors.Join(rows.Err(), rows.Close())
}

// Bits of pg_trigger.tgtype.
const (
	triggerRow    = 1 << 0
	triggerBefore = 1 << 1
	triggerInsert = 1 << 2
	triggerDelete = 1 << 3
	triggerUpdate = 1 << 4
)

type trigger struct {
	function string
	kind     int
	args     []string
}

func (t trigger) on(event int) bool { return t.kind&event != 0 }
func (t trigger) before() bool      { return t.kind&triggerBefore != 0 }
func (t trigger) row() bool         { return t.kind&triggerRow != 0 }

func tableTriggers(ctx context.Context, db *sql.DB, table string) ([]trigger, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT p.proname, t.tgtype, t.tgargs
		FROM pg_trigger t
		JOIN pg_proc p ON p.oid = t.tgfoid
		WHERE t.tgrelid = to_regclass($1) AND NOT t.tgisinternal AND t.tgenabled <> 'D'`, table)
	if err != nil {
		return nil, err
	}
	var result []trigger
	for rows.Next() {
		var t trigger
		var raw []byte
		if err := rows.Scan(&t.function, &t.kind, &raw); err != nil {
			_ = rows.Close()
			return nil, err
		}
		for _, arg := range bytes.Split(bytes.TrimSuffix(raw, []byte{0}), []byte{0}) {
			if len(raw) > 0 {
				t.args = append(t.args, string(arg))
			}
		}
		result = append(result, t)
	}
	return result, errors.Join(rows.Err(), rows.Close())
}
