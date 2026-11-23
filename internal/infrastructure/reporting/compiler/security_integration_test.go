//go:build integration

package compiler_test

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/readpolicy"
	"net/http"
	"os"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/domain/filter"
	reportcontract "github.com/alex-bogatiuk/metapus/internal/domain/reports/contract"
	"github.com/alex-bogatiuk/metapus/internal/domain/reports/schema"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/reporting/compiler"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestReportPoliciesApplyBeforeUnionAndAggregation(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn, "explicit disposable database is required")
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.MaxConns = 1 // TEMP fixtures and compiler share the same isolated connection.
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(context.Background(), `
        CREATE TEMP TABLE report_policy_a(id uuid, number text, organization_id uuid, currency_id uuid, amount bigint);
        CREATE TEMP TABLE report_policy_b(LIKE report_policy_a);
        CREATE TEMP TABLE report_policy_currency(id uuid, code text, decimal_places int);
    `)
	require.NoError(t, err)
	orgA, orgB, usd, eur := id.New(), id.New(), id.New(), id.New()
	_, err = pool.Exec(context.Background(), `INSERT INTO report_policy_currency VALUES($1,'USD',2),($2,'EUR',2)`, usd, eur)
	require.NoError(t, err)
	for _, table := range []string{"report_policy_a", "report_policy_b"} {
		_, err = pool.Exec(context.Background(), "INSERT INTO "+table+" VALUES($1,'PUBLIC',$2,$4,15),($1,'SECRET',$2,$4,1000),($1,'OTHER_SCOPE',$3,$4,2000),($1,'NULL_SCOPE',NULL,$4,4000),($1,'PRIVATE_CURRENCY',$2,$5,8000)", id.New(), orgA, orgB, usd, eur)
		require.NoError(t, err)
	}
	registry, ds := securityFixture()
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	defer engine.Stop()
	c := compiler.NewCompiler(registry, []*schema.Dataset{&ds}, postgres.NewAuthorizedReadService(registry, engine), map[string]readpolicy.DatasetExecutor{"security-union": securityUnionExecutor{}})
	principal := appctx.UserContext{UserID: id.New().String(), Permissions: []string{ds.Permission, "a:read", "b:read", "currency:read"}}
	actor := appctx.WithUser(context.Background(), &principal)
	ctx := tenant.WithTxManager(actor, postgres.NewTxManagerFromRawPool(pool))
	ctx = security.WithDataScope(ctx, &security.DataScope{Dimensions: map[string][]string{"organization": {orgA.String()}}})
	ctx = security.WithPolicyRules(ctx, []security.PolicyRule{
		readRule("a", "doc.number == 'SECRET'", "deny"), readRule("b", "doc.number == 'SECRET'", "deny"), readRule("currency", "doc.code == 'EUR'", "deny"),
	})
	query := compiler.QueryRequest{Dataset: ds.Key, Select: []string{"amount"}, GroupBy: []string{"currency_id"}}
	result, err := c.Execute(ctx, query)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "30", result.Items[0]["amount"])
	require.Equal(t, usd.String(), result.Items[0]["currency_id"])
	require.Equal(t, "USD", result.Items[0]["currency_code"])

	rows, err := c.Execute(ctx, compiler.QueryRequest{Dataset: ds.Key, Select: []string{"number", "amount"}, Limit: 1})
	require.NoError(t, err)
	require.Len(t, rows.Items, 1)
	require.Equal(t, "15", rows.Items[0]["amount"])
	empty := security.WithDataScope(ctx, &security.DataScope{Dimensions: map[string][]string{"organization": {}}})
	result, err = c.Execute(empty, query)
	require.NoError(t, err)
	require.Empty(t, result.Items, "NULL organization must not bypass an empty scope")

	hidden := security.WithFieldPolicies(ctx, map[string]*security.FieldPolicy{"a:read": {AllowedFields: []string{"*", "-amount"}}})
	for _, request := range []compiler.QueryRequest{
		query,
		{Dataset: ds.Key, Select: []string{"number"}, OrderBy: "amount"},
		{Dataset: ds.Key, Select: []string{"number"}, ExportColumns: []string{"amount"}},
		{Dataset: ds.Key, Select: []string{"number"}, AdvancedFilters: []filter.Item{{Field: "amount", Operator: filter.Greater, Value: "0"}}},
	} {
		_, err = c.Execute(hidden, request)
		require.Equal(t, http.StatusForbidden, apperror.GetHTTPStatus(err))
	}
	// No transaction manager is attached: unsupported CEL must fail before SQL.
	unsupported := security.WithPolicyRules(actor, []security.PolicyRule{readRule("a", "doc.number.startsWith('S')", "deny")})
	_, err = c.Execute(unsupported, query)
	var app *apperror.AppError
	require.ErrorAs(t, err, &app)
	require.Equal(t, "REPORT_POLICY_UNSUPPORTED", app.Code)

	// A raw-amount source (NUMERIC) has a wider range than BIGINT. Filters, SQL
	// SUM, scanning and the returned string must all keep the full raw amount.
	_, err = pool.Exec(context.Background(), `ALTER TABLE report_policy_a ALTER COLUMN amount TYPE NUMERIC(78,0); UPDATE report_policy_a SET amount=100000000000000000000000000001 WHERE number='PUBLIC'`)
	require.NoError(t, err)
	ds.FindField("amount").Money.AmountType = reportcontract.AmountTypeRaw
	result, err = c.Execute(ctx, query)
	require.NoError(t, err)
	require.Equal(t, "100000000000000000000000000016", result.Items[0]["amount"])
	filtered := query
	filtered.AdvancedFilters = []filter.Item{{Field: "amount", Operator: filter.Greater, Value: "9223372036854775808"}}
	result, err = c.Execute(ctx, filtered)
	require.NoError(t, err)
	require.Equal(t, "100000000000000000000000000001", result.Items[0]["amount"])
}
