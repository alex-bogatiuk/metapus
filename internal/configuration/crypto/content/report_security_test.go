package content_test

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/appctx"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/configuration"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/filter"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/v1"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/reports/compiler"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/reports/schema"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/security"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/security_profile"
	"github.com/stretchr/testify/require"
)

func reportCompiler(t *testing.T) *compiler.Compiler {
	t.Helper()
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	t.Cleanup(engine.Stop)
	factories := configuration.Compose(content.Crypto(content.Services{}))
	registry := v1.BuildReadRegistry(factories)
	datasets := []*schema.Dataset{}
	for _, original := range factories.Datasets() {
		copy := *original
		copy.Fields = append([]schema.Field(nil), original.Fields...)
		datasets = append(datasets, &copy)
	}
	return compiler.NewCompiler(registry, datasets, postgres.NewAuthorizedReadService(registry, engine), factories.DatasetExecutors())
}

func reportPrincipal() appctx.UserContext {
	return appctx.UserContext{UserID: id.New().String(), Permissions: []string{
		"report:document-journal:read",
		"document:crypto_invoice:read", "document:crypto_payment:read",
		"document:crypto_withdrawal:read", "document:treasury_withdrawal:read",
		"catalog:token:read", "catalog:merchant:read",
	}}
}

func readRule(entity, expression, effect string) security.PolicyRule {
	return &security_profile.PolicyRule{ID: id.New(), EntityName: entity, Expression: expression, Effect: effect, Actions: []string{"read"}, Enabled: true}
}

func TestReportAuthorizationRejectsBeforeDatabaseAccess(t *testing.T) {
	c := reportCompiler(t)
	principal := reportPrincipal()
	base := appctx.WithUser(context.Background(), &principal) // Deliberately no database dependency.
	t.Run("missing principal", func(t *testing.T) {
		_, err := c.Execute(context.Background(), compiler.QueryRequest{Dataset: "document-journal"})
		require.Equal(t, http.StatusUnauthorized, apperror.GetHTTPStatus(err))
	})
	t.Run("real dataset permission", func(t *testing.T) {
		user := principal
		user.Permissions = []string{"report:other:read"}
		_, err := c.Execute(appctx.WithUser(context.Background(), &user), compiler.QueryRequest{Dataset: "document-journal"})
		require.Equal(t, http.StatusForbidden, apperror.GetHTTPStatus(err))
	})
	for _, expression := range []string{"doc.date < now", "doc.number.startsWith('A')", "doc.totalAmount + 1 > 0"} {
		t.Run(expression, func(t *testing.T) {
			ctx := security.WithPolicyRules(base, []security.PolicyRule{readRule("crypto_invoice", expression, "deny")})
			_, err := c.Execute(ctx, compiler.QueryRequest{Dataset: "document-journal"})
			var app *apperror.AppError
			require.ErrorAs(t, err, &app)
			require.Equal(t, "REPORT_POLICY_UNSUPPORTED", app.Code)
			require.Equal(t, http.StatusForbidden, apperror.GetHTTPStatus(app))
		})
	}
	t.Run("unsupported joined policy", func(t *testing.T) {
		ctx := security.WithPolicyRules(base, []security.PolicyRule{readRule("merchant", "doc.name.startsWith('A')", "deny")})
		_, err := c.Execute(ctx, compiler.QueryRequest{Dataset: "document-journal"})
		require.Equal(t, http.StatusForbidden, apperror.GetHTTPStatus(err))
	})
}

func TestReportFieldPoliciesCoverEveryQueryClause(t *testing.T) {
	c := reportCompiler(t)
	principal := reportPrincipal()
	base := appctx.WithUser(context.Background(), &principal)
	for _, tc := range []struct {
		name    string
		request compiler.QueryRequest
	}{
		{"select", compiler.QueryRequest{Select: []string{"total_amount"}}},
		{"filter", compiler.QueryRequest{Select: []string{"number"}, AdvancedFilters: []filter.Item{{Field: "total_amount", Operator: filter.Greater, Value: 1}}}},
		{"search", compiler.QueryRequest{Select: []string{"number"}, AdvancedFilters: []filter.Item{{Field: "total_amount", Operator: filter.Contains, Value: "secret"}}}},
		{"order", compiler.QueryRequest{Select: []string{"number"}, OrderBy: "total_amount"}},
		{"group", compiler.QueryRequest{Select: []string{"number"}, GroupBy: []string{"total_amount"}}},
		{"export", compiler.QueryRequest{Select: []string{"number"}, ExportColumns: []string{"total_amount"}}},
		{"export group", compiler.QueryRequest{Select: []string{"number"}, ExportGroupBy: []string{"total_amount"}}},
		{"display group", compiler.QueryRequest{Select: []string{"number"}, DisplayFields: []string{"total_amount"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.request.Dataset = "document-journal"
			for _, hidden := range []string{"expected_amount"} {
				ctx := security.WithFieldPolicies(base, map[string]*security.FieldPolicy{"crypto_invoice:read": {AllowedFields: []string{"*", "-" + hidden}}})
				_, err := c.Execute(ctx, tc.request)
				require.Equal(t, http.StatusForbidden, apperror.GetHTTPStatus(err), "%s: %v", hidden, err)
			}
		})
	}
	t.Run("related fields and parameters", func(t *testing.T) {
		ctx := security.WithFieldPolicies(base, map[string]*security.FieldPolicy{"merchant:read": {AllowedFields: []string{"*", "-name"}}})
		_, err := c.Execute(ctx, compiler.QueryRequest{Dataset: "document-journal", Select: []string{"counterparty_name"}})
		require.Equal(t, http.StatusForbidden, apperror.GetHTTPStatus(err))
		ctx = security.WithFieldPolicies(base, map[string]*security.FieldPolicy{"crypto_invoice:read": {AllowedFields: []string{"*", "-posted"}}})
		_, err = c.Execute(ctx, compiler.QueryRequest{Dataset: "document-journal", Select: []string{"number"}, Filters: map[string]any{"posted": false}})
		require.Equal(t, http.StatusForbidden, apperror.GetHTTPStatus(err))
	})
}

func TestReportComposesAllParametersAndLateJoins(t *testing.T) {
	c := reportCompiler(t)
	principal := reportPrincipal()
	ctx := appctx.WithUser(context.Background(), &principal)
	ctx = security.WithPolicyRules(ctx, []security.PolicyRule{
		readRule("merchant", "doc.code == 'PRIVATE'", "deny"),
		readRule("crypto_invoice", "doc.number == 'PRIVATE'", "deny"),
	})
	req := compiler.QueryRequest{
		Dataset:         "document-journal",
		Filters:         map[string]any{"from_date": "2026-01-01", "to_date": "2026-02-01", "posted": true},
		AdvancedFilters: []filter.Item{{Field: "number", Operator: filter.Contains, Value: "PUBLIC"}},
	}
	q, err := c.BuildQuery(ctx, req)
	require.NoError(t, err)
	sql, args, err := q.ToSql()
	require.NoError(t, err)
	placeholders := regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(sql, -1)
	require.Len(t, placeholders, len(args), sql)
	for i, placeholder := range placeholders {
		require.Equal(t, strconv.Itoa(i+1), placeholder[1], sql)
	}
}
