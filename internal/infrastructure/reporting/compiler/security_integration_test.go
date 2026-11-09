//go:build integration

package compiler_test

import (
	"context"
	"metapus/internal/infrastructure/readpolicy"
	"net/http"
	"os"
	"testing"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/internal/content"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/filter"
	reportcontract "metapus/internal/domain/reports/contract"
	"metapus/internal/domain/reports/schema"
	"metapus/internal/infrastructure/reporting/compiler"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/metadata"
)

// Independent sources exercise the compiler's security contract without tying
// policy coverage to the lifecycle of any particular business document.
type reportSecurityRow struct {
	ID             id.ID  `db:"id" json:"id"`
	Number         string `db:"number" json:"number"`
	OrganizationID *id.ID `db:"organization_id" json:"organizationId"`
	CurrencyID     id.ID  `db:"currency_id" json:"currencyId"`
	Amount         int64  `db:"amount" json:"amount"`
}
type reportSecurityCurrency struct {
	ID            id.ID  `db:"id" json:"id"`
	Code          string `db:"code" json:"code"`
	DecimalPlaces int    `db:"decimal_places" json:"decimalPlaces"`
}
type securityUnionExecutor struct{}

func (securityUnionExecutor) BuildQuery(_ context.Context, _ map[string]any, constraints readpolicy.ReadConstraints) (squirrel.SelectBuilder, error) {
	a, err := constraints.Source("a")
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	b, err := constraints.Source("b")
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	columns := []string{"d.number", "d.amount", "c.id AS currency_id", "c.code AS currency_code", "c.decimal_places"}
	union := squirrel.Expr("(?) UNION ALL (?)", a.Columns(columns...), b.Columns(columns...))
	return squirrel.Select().PrefixExpr(squirrel.Expr("WITH base AS (?)", union)).From("base"), nil
}

func TestReportPoliciesApplyBeforeUnionAndAggregation(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
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
	registry := metadata.NewRegistry()
	for _, key := range []string{"a", "b"} {
		def := metadata.Inspect(reportSecurityRow{}, key, metadata.TypeDocument)
		def.Key, def.TableName, def.ReadPermission = key, "report_policy_"+key, key+":read"
		def.RLSDimensions = map[string]string{"organization": "organization_id"}
		registry.Register(def)
	}
	currency := metadata.Inspect(reportSecurityCurrency{}, "currency", metadata.TypeCatalog)
	currency.Key, currency.TableName, currency.ReadPermission = "currency", "report_policy_currency", "currency:read"
	registry.Register(currency)
	amountAccess := []schema.FieldAccess{{Entity: "a", Fields: []string{"amount"}}, {Entity: "b", Fields: []string{"amount"}}}
	ds := schema.Dataset{Key: "security-union", Permission: "report:security-union:read", ExecutorKey: "security-union",
		Sources: []schema.Source{
			{Key: "a", Entity: "a", Alias: "d", Relations: []schema.SourceRelation{{Entity: "currency", Alias: "c", Column: "currency_id", Required: true}}},
			{Key: "b", Entity: "b", Alias: "d", Relations: []schema.SourceRelation{{Entity: "currency", Alias: "c", Column: "currency_id", Required: true}}},
		},
		Fields: []schema.Field{
			{Name: "number", Type: schema.TypeString, Kind: schema.FieldDimension, Access: []schema.FieldAccess{{Entity: "a", Fields: []string{"number"}}, {Entity: "b", Fields: []string{"number"}}}},
			{Name: "amount", Type: schema.TypeMoney, Kind: schema.FieldMeasure, Agg: schema.AggSum, Access: amountAccess, Money: &reportcontract.MoneySources{CurrencyID: "currency_id", CurrencyCode: "currency_code", DecimalPlaces: "decimal_places"}},
			{Name: "currency_id", Type: schema.TypeString, Kind: schema.FieldDimension, Access: []schema.FieldAccess{{Entity: "currency", Fields: []string{"id"}}}},
			{Name: "currency_code", Type: schema.TypeString, Kind: schema.FieldDimension, Access: []schema.FieldAccess{{Entity: "currency", Fields: []string{"code"}}}},
			{Name: "decimal_places", Type: schema.TypeInteger, Kind: schema.FieldDimension, Access: []schema.FieldAccess{{Entity: "currency", Fields: []string{"decimal_places"}}}},
		},
	}
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	defer engine.Stop()
	c := compiler.NewCompiler(registry, []*schema.Dataset{&ds}, postgres.NewAuthorizedReadService(registry, engine), map[string]readpolicy.DatasetExecutor{"security-union": securityUnionExecutor{}}, content.DatasetExecutors())
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

	// A crypto source has a different range from ERP BIGINT. Filters, SQL SUM,
	// scanning and the returned string must all keep the full raw amount.
	_, err = pool.Exec(context.Background(), `ALTER TABLE report_policy_a ALTER COLUMN amount TYPE NUMERIC(78,0); UPDATE report_policy_a SET amount=100000000000000000000000000001 WHERE number='PUBLIC'`)
	require.NoError(t, err)
	ds.FindField("amount").Money.AmountType = "crypto"
	result, err = c.Execute(ctx, query)
	require.NoError(t, err)
	require.Equal(t, "100000000000000000000000000016", result.Items[0]["amount"])
	filtered := query
	filtered.AdvancedFilters = []filter.Item{{Field: "amount", Operator: filter.Greater, Value: "9223372036854775808"}}
	result, err = c.Execute(ctx, filtered)
	require.NoError(t, err)
	require.Equal(t, "100000000000000000000000000001", result.Items[0]["amount"])
}

func TestBuiltInJournalUsesFreshCryptoSchema(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	ctx := tenant.WithTxManager(appctx.WithUser(context.Background(), &appctx.UserContext{UserID: id.New().String(), IsAdmin: true}), postgres.NewTxManagerFromRawPool(pool))
	for _, dataset := range content.AllDatasets() {
		_, err := reportCompiler(t).Execute(ctx, compiler.QueryRequest{Dataset: dataset.Key, Limit: 1})
		require.NoError(t, err, dataset.Key)
	}
}
