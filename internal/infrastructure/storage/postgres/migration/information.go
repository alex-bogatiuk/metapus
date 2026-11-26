package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/domain/registers/information"
)

// WithInformationRegisters makes Run check, after the migrations, that the
// tenant database holds the tables of the given information registers as
// they are declared (CheckInformationRegisters).
func WithInformationRegisters(schemas []*information.Schema) Option {
	return func(o *options) { o.information = append(o.information, schemas...) }
}

// InformationDDL is the SQL that creates an information register's table in
// a configuration's migration; the tenant migration checks the table against
// the same schema. Foreign keys of references are left for the author.
func InformationDDL(schema *information.Schema) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE %s (\n", schema.Table)
	for _, c := range informationColumns(schema) {
		fmt.Fprintf(&b, " %s %s NOT NULL,\n", c.name, strings.ToUpper(sqlName(c.dataType)))
	}
	fmt.Fprintf(&b, " PRIMARY KEY (%s)\n);\n", strings.Join(schema.KeyColumns(), ", "))
	fmt.Fprintf(&b, "COMMENT ON TABLE %s IS '%s';\n", schema.Table, escapeLiteral(schema.Label))
	if schema.Mode == information.Recorder {
		fmt.Fprintf(&b, "CREATE INDEX idx_%s_recorder ON %s (recorder_id);\n", schema.Table, schema.Table)
	}
	return b.String()
}

// sqlName is the DDL spelling of an information_schema data type.
func sqlName(dataType string) string {
	if dataType == "timestamp with time zone" {
		return "timestamptz"
	}
	return dataType
}

func informationColumns(schema *information.Schema) []column {
	var columns []column
	if schema.Periodic() {
		columns = append(columns, column{"period", "timestamp with time zone"})
	}
	if schema.Mode == information.Recorder {
		columns = append(columns, column{"recorder_id", "uuid"}, column{"recorder_type", "text"})
	}
	for _, f := range append(append([]information.Field(nil), schema.Dimensions...), schema.Resources...) {
		columns = append(columns, column{f.Column, f.Type.SQLType()})
	}
	return columns
}

// CheckInformationRegisters compares each register's declaration with its
// table: columns, types, NOT NULL, no other column, and the primary key of
// the period and dimensions. A mismatch names every difference and shows the
// SQL the declaration expects.
func CheckInformationRegisters(ctx context.Context, db *sql.DB, schemas []*information.Schema) error {
	var errs []error
	for _, schema := range schemas {
		problems, err := tableProblems(ctx, db, schema.Table, informationColumns(schema))
		if err != nil {
			return fmt.Errorf("check register %s: %w", schema.Key, err)
		}
		key, err := primaryKey(ctx, db, schema.Table)
		if err != nil {
			return fmt.Errorf("check register %s: %w", schema.Key, err)
		}
		if want := slices.Sorted(slices.Values(schema.KeyColumns())); len(problems) == 0 && !slices.Equal(key, want) {
			problems = append(problems, fmt.Sprintf("the primary key of %s is (%s), not the period and dimensions (%s)",
				schema.Table, strings.Join(key, ", "), strings.Join(want, ", ")))
		}
		if len(problems) > 0 {
			errs = append(errs, fmt.Errorf("register %s does not match its table:\n  - %s\nexpected:\n%s",
				schema.Key, strings.Join(problems, "\n  - "), InformationDDL(schema)))
		}
	}
	return errors.Join(errs...)
}
