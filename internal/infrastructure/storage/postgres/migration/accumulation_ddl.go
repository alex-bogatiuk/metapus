package migration

import (
	"fmt"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/domain/registers/accumulation"
)

// AccumulationDDL is the SQL that creates a declarative register's tables in
// a configuration's migration: the movements table, its indexes and the
// triggers that keep the balances table, for a Balances register. The tenant
// migration checks the tables against the same schema
// (CheckAccumulationRegisters), so a migration that starts from
// this text passes the check. Foreign keys of reference dimensions are left
// for the author to add, since only they know the referenced table.
func AccumulationDDL(schema *accumulation.Schema) string {
	var b strings.Builder
	movements := schema.MovementsTable()
	fmt.Fprintf(&b, "CREATE TABLE %s (\n", movements)
	fmt.Fprintf(&b, " line_id          UUID        PRIMARY KEY,\n")
	fmt.Fprintf(&b, " recorder_id      UUID        NOT NULL,\n")
	fmt.Fprintf(&b, " recorder_type    TEXT        NOT NULL,\n")
	fmt.Fprintf(&b, " recorder_version INT         NOT NULL,\n")
	fmt.Fprintf(&b, " period           TIMESTAMPTZ NOT NULL,\n")
	fmt.Fprintf(&b, " record_type      TEXT        NOT NULL CHECK (record_type IN ('receipt', 'expense')),\n")
	fmt.Fprintf(&b, " created_at       TIMESTAMPTZ NOT NULL,\n")
	for _, f := range schema.Dimensions {
		fmt.Fprintf(&b, " %s %s NOT NULL,\n", f.Column, strings.ToUpper(f.Type.SQLType()))
	}
	for i, f := range schema.Resources {
		sep := ","
		if i == len(schema.Resources)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, " %s BIGINT NOT NULL%s\n", f.Column, sep)
	}
	fmt.Fprintf(&b, ");\n")
	fmt.Fprintf(&b, "COMMENT ON TABLE %s IS '%s — движения';\n", movements, escapeLiteral(schema.Label))
	fmt.Fprintf(&b, "CREATE INDEX idx_%s_recorder ON %s (recorder_id, recorder_version);\n", movements, movements)
	fmt.Fprintf(&b, "CREATE INDEX idx_%s_dimensions ON %s (%s, period);\n", movements, movements, strings.Join(schema.DimensionColumns(), ", "))
	fmt.Fprintf(&b, "CREATE TRIGGER trg_%s_immutable BEFORE UPDATE ON %s\n    FOR EACH ROW EXECUTE FUNCTION sys_accumulation_movements_immutable();\n", schema.Table, movements)
	if schema.Kind != accumulation.Balances {
		return b.String()
	}
	balances := schema.BalancesTable()
	fmt.Fprintf(&b, "\nCREATE TABLE %s (\n", balances)
	for _, f := range schema.Dimensions {
		fmt.Fprintf(&b, " %s %s NOT NULL,\n", f.Column, strings.ToUpper(f.Type.SQLType()))
	}
	for _, f := range schema.Resources {
		fmt.Fprintf(&b, " %s BIGINT NOT NULL DEFAULT 0,\n", f.Column)
	}
	fmt.Fprintf(&b, " updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),\n")
	fmt.Fprintf(&b, " PRIMARY KEY (%s)\n);\n", strings.Join(schema.DimensionColumns(), ", "))
	fmt.Fprintf(&b, "COMMENT ON TABLE %s IS '%s — остатки, ведутся триггерами движений';\n", balances, escapeLiteral(schema.Label))
	args := TriggerArguments(schema)
	for _, op := range []struct{ name, event, transition string }{{"insert", "INSERT", "NEW TABLE AS new_rows"}, {"delete", "DELETE", "OLD TABLE AS old_rows"}} {
		fmt.Fprintf(&b, "CREATE TRIGGER trg_%s_balances_%s\n    AFTER %s ON %s\n    REFERENCING %s\n    FOR EACH STATEMENT EXECUTE FUNCTION sys_accumulation_balances('%s', '%s', '%s');\n",
			schema.Table, op.name, op.event, movements, op.transition, args[0], args[1], args[2])
	}
	return b.String()
}

// TriggerArguments are the arguments of the register's balance triggers:
// the balances table, the dimension columns and the resource columns.
func TriggerArguments(schema *accumulation.Schema) [3]string {
	return [3]string{schema.BalancesTable(), strings.Join(schema.DimensionColumns(), ","), strings.Join(schema.ResourceColumns(), ",")}
}

func escapeLiteral(s string) string { return strings.ReplaceAll(s, "'", "''") }
