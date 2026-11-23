//go:build integration

package postgres_test

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/readpolicy"
	"os"
	"testing"

	"github.com/Masterminds/squirrel"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/domain/security_profile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestCELSQLMatchesGoPolicyEngineBeforeAggregation(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	transaction, err := pool.Begin(context.Background())
	require.NoError(t, err)
	defer func() { require.NoError(t, transaction.Rollback(context.Background())) }()
	_, err = transaction.Exec(context.Background(), `CREATE TEMP TABLE policy_rows(ordinal int PRIMARY KEY, name text, amount bigint, enabled boolean)`)
	require.NoError(t, err)
	type row struct {
		Name    *string `db:"name" json:"name"`
		Amount  *int64  `db:"amount" json:"amount"`
		Enabled *bool   `db:"enabled" json:"enabled"`
	}
	ptr := func(value string) *string { return &value }
	integer := func(value int64) *int64 { return &value }
	boolean := func(value bool) *bool { return &value }
	records := []row{
		{}, {ptr(""), integer(0), boolean(false)}, {ptr("abc"), integer(2), boolean(true)},
		{ptr("z"), integer(10), nil}, {ptr("я"), integer(-1), boolean(false)},
		{ptr("é"), integer(9223372036854775807), boolean(true)}, {ptr("admin"), integer(-9223372036854775808), nil},
		{ptr("x' OR TRUE; --"), nil, boolean(true)}, {nil, integer(1), nil},
	}
	for i, value := range records {
		_, err = transaction.Exec(context.Background(), `INSERT INTO policy_rows VALUES($1,$2,$3,$4)`, i, value.Name, value.Amount, value.Enabled)
		require.NoError(t, err)
	}
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	defer engine.Stop()
	ctx := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: id.New().String(), Email: "abc", Roles: []string{"admin", "reader"}})
	fields := map[string]readpolicy.SQLPolicyField{
		"name":    {SQL: "r.name", Kind: readpolicy.SQLString, Nullable: true},
		"amount":  {SQL: "r.amount", Kind: readpolicy.SQLInteger, Nullable: true},
		"enabled": {SQL: "r.enabled", Kind: readpolicy.SQLBoolean, Nullable: true},
	}
	rule := func(expression, effect string, priority int) security.PolicyRule {
		return &security_profile.PolicyRule{ID: id.New(), EntityName: "entry", Actions: []string{"read"}, Expression: expression, Effect: effect, Priority: priority, Enabled: true}
	}
	cases := [][]security.PolicyRule{}
	for _, expression := range []string{
		"true", "false", "2 < 10", "10 < 2", "doc.amount > 0", "doc.amount <= 2", "doc.amount == null", "doc.amount != null",
		"doc.name == user.email", "doc.name < 'z'", "doc.name >= 'é'", "doc['name'] != 'abc'", "doc.name == \"x' OR TRUE; --\"",
		"doc.enabled", "!doc.enabled", "doc.enabled == null", "doc.enabled != true",
		"doc.enabled || doc.amount > 0", "doc.enabled && doc.amount > 0", "true || doc.amount > 0", "false && doc.amount > 0",
		"doc.name in ['abc','é',null]", "doc.amount in [0,2,10,null]", "doc.name in []", "'admin' in user.roles", "action == 'read'",
		"doc.amount > null", "!(doc.amount > 0)", "doc.amount != 2 && doc.name != null",
	} {
		cases = append(cases, []security.PolicyRule{rule(expression, "deny", 0)})
	}
	cases = append(cases,
		[]security.PolicyRule{rule("doc.enabled", "allow", 10), rule("true", "deny", 0)},
		[]security.PolicyRule{rule("doc.amount > 2", "deny", 0), rule("doc.name == 'abc'", "allow", 10)},
		[]security.PolicyRule{rule("doc.amount > 0", "deny", 10), rule("true", "allow", 0)},
	)
	for _, rules := range cases {
		t.Run(rules[0].GetExpression(), func(t *testing.T) {
			predicate, err := readpolicy.CompileReadSQL(ctx, engine, rules, fields)
			require.NoError(t, err)
			query, args, err := squirrel.Select("ordinal").From("policy_rows r").Where(predicate).OrderBy("ordinal").PlaceholderFormat(squirrel.Dollar).ToSql()
			require.NoError(t, err)
			rows, err := transaction.Query(ctx, query, args...)
			require.NoError(t, err)
			got, err := pgx.CollectRows(rows, pgx.RowTo[int])
			require.NoError(t, err)
			expected := []int{}
			for i := range records {
				if engine.Evaluate(ctx, rules, "read", &records[i]) == nil {
					expected = append(expected, i)
				}
			}
			require.Equal(t, expected, got, "SQL and Go must authorize exactly the same source rows")
			// An aggregate consumes the authorized rows, not an already computed total.
			aggregate, args, err := squirrel.Select("COUNT(*)").From("policy_rows r").Where(predicate).PlaceholderFormat(squirrel.Dollar).ToSql()
			require.NoError(t, err)
			var count int
			require.NoError(t, transaction.QueryRow(ctx, aggregate, args...).Scan(&count))
			require.Equal(t, len(expected), count)
		})
	}
}
