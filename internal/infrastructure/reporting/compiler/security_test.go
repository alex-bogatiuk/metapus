package compiler_test

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain/filter"
	"metapus/internal/domain/reports/schema"
	"metapus/internal/infrastructure/readpolicy"
	"metapus/internal/infrastructure/reporting/compiler"
	"metapus/internal/infrastructure/storage/postgres"
)

// These cases need no database: authorization and policy checks run before
// any SQL. The configuration's own datasets are covered in internal/content.
func fixtureCompiler(t *testing.T) (*compiler.Compiler, string) {
	t.Helper()
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	t.Cleanup(engine.Stop)
	registry, ds := securityFixture()
	return compiler.NewCompiler(registry, []*schema.Dataset{&ds}, postgres.NewAuthorizedReadService(registry, engine),
		map[string]readpolicy.DatasetExecutor{"security-union": securityUnionExecutor{}}), ds.Key
}

func fixturePrincipal(dataset string) context.Context {
	principal := appctx.UserContext{UserID: id.New().String(), Permissions: []string{"report:" + dataset + ":read", "a:read", "b:read", "currency:read"}}
	return appctx.WithUser(context.Background(), &principal)
}

func TestReportAuthorizationRejectsBeforeDatabaseAccess(t *testing.T) {
	c, dataset := fixtureCompiler(t)
	base := fixturePrincipal(dataset) // Deliberately no database dependency.
	t.Run("missing principal", func(t *testing.T) {
		_, err := c.Execute(context.Background(), compiler.QueryRequest{Dataset: dataset})
		require.Equal(t, http.StatusUnauthorized, apperror.GetHTTPStatus(err))
	})
	t.Run("dataset permission", func(t *testing.T) {
		user := appctx.UserContext{UserID: id.New().String(), Permissions: []string{"report:other:read", "a:read", "b:read", "currency:read"}}
		_, err := c.Execute(appctx.WithUser(context.Background(), &user), compiler.QueryRequest{Dataset: dataset})
		require.Equal(t, http.StatusForbidden, apperror.GetHTTPStatus(err))
	})
	for _, expression := range []string{"doc.number.startsWith('A')", "doc.amount + 1 > 0"} {
		t.Run(expression, func(t *testing.T) {
			ctx := security.WithPolicyRules(base, []security.PolicyRule{readRule("a", expression, "deny")})
			_, err := c.Execute(ctx, compiler.QueryRequest{Dataset: dataset, Select: []string{"number"}})
			var app *apperror.AppError
			require.ErrorAs(t, err, &app)
			require.Equal(t, "REPORT_POLICY_UNSUPPORTED", app.Code)
			require.Equal(t, http.StatusForbidden, apperror.GetHTTPStatus(app))
		})
	}
	t.Run("supported policy", func(t *testing.T) {
		ctx := security.WithPolicyRules(base, []security.PolicyRule{readRule("a", "doc.number == 'A'", "deny")})
		_, err := c.BuildQuery(ctx, compiler.QueryRequest{Dataset: dataset, Select: []string{"number"}})
		require.NoError(t, err)
	})
	t.Run("unsupported joined policy", func(t *testing.T) {
		ctx := security.WithPolicyRules(base, []security.PolicyRule{readRule("currency", "doc.code.startsWith('A')", "deny")})
		_, err := c.Execute(ctx, compiler.QueryRequest{Dataset: dataset, Select: []string{"number"}})
		require.Equal(t, http.StatusForbidden, apperror.GetHTTPStatus(err))
	})
}

func TestReportFieldPoliciesCoverEveryQueryClause(t *testing.T) {
	c, dataset := fixtureCompiler(t)
	base := fixturePrincipal(dataset)
	for _, tc := range []struct {
		name, hidden string
		request      compiler.QueryRequest
	}{
		{"select", "amount", compiler.QueryRequest{Select: []string{"amount"}}},
		{"filter", "amount", compiler.QueryRequest{Select: []string{"number"}, AdvancedFilters: []filter.Item{{Field: "amount", Operator: filter.Greater, Value: "1"}}}},
		{"search", "number", compiler.QueryRequest{Select: []string{"currency_code"}, AdvancedFilters: []filter.Item{{Field: "number", Operator: filter.Contains, Value: "secret"}}}},
		{"order", "amount", compiler.QueryRequest{Select: []string{"number"}, OrderBy: "amount"}},
		{"group", "number", compiler.QueryRequest{Select: []string{"amount"}, GroupBy: []string{"number"}}},
		{"export", "amount", compiler.QueryRequest{Select: []string{"number"}, ExportColumns: []string{"amount"}}},
		{"export group", "amount", compiler.QueryRequest{Select: []string{"number"}, ExportGroupBy: []string{"amount"}}},
		{"display group", "amount", compiler.QueryRequest{Select: []string{"number"}, DisplayFields: []string{"amount"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.request.Dataset = dataset
			_, err := c.BuildQuery(base, tc.request)
			require.NoError(t, err, "the request is valid without the field policy")
			// Hiding the field on one source of the union is enough to deny it.
			ctx := security.WithFieldPolicies(base, map[string]*security.FieldPolicy{"a:read": {AllowedFields: []string{"*", "-" + tc.hidden}}})
			_, err = c.Execute(ctx, tc.request)
			require.Equal(t, http.StatusForbidden, apperror.GetHTTPStatus(err), "%v", err)
		})
	}
	t.Run("related fields", func(t *testing.T) {
		request := compiler.QueryRequest{Dataset: dataset, Select: []string{"currency_code"}}
		_, err := c.BuildQuery(base, request)
		require.NoError(t, err)
		ctx := security.WithFieldPolicies(base, map[string]*security.FieldPolicy{"currency:read": {AllowedFields: []string{"*", "-code"}}})
		_, err = c.Execute(ctx, request)
		require.Equal(t, http.StatusForbidden, apperror.GetHTTPStatus(err))
	})
}

func TestReportComposesAllParametersAndLateJoins(t *testing.T) {
	c, dataset := fixtureCompiler(t)
	ctx := security.WithDataScope(fixturePrincipal(dataset), &security.DataScope{Dimensions: map[string][]string{"organization": {id.New().String()}}})
	ctx = security.WithPolicyRules(ctx, []security.PolicyRule{
		readRule("currency", "doc.code == 'PRIVATE'", "deny"),
		readRule("a", "doc.number == 'PRIVATE'", "deny"),
		readRule("b", "doc.number == 'PRIVATE'", "deny"),
	})
	req := compiler.QueryRequest{
		Dataset:         dataset,
		Select:          []string{"number", "amount"},
		AdvancedFilters: []filter.Item{{Field: "number", Operator: filter.Contains, Value: "PUBLIC"}},
	}
	q, err := c.BuildQuery(ctx, req)
	require.NoError(t, err)
	sql, args, err := q.ToSql()
	require.NoError(t, err)
	placeholders := regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(sql, -1)
	require.Len(t, placeholders, len(args), sql)
	require.NotEmpty(t, args, sql)
	for i, placeholder := range placeholders {
		require.Equal(t, strconv.Itoa(i+1), placeholder[1], sql)
	}
}
