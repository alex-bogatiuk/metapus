package main

import (
	"context"
	"testing"

	"github.com/Masterminds/squirrel"
	"github.com/stretchr/testify/require"

	"metapus/internal/content"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain/reports/schema"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/readpolicy"
	"metapus/internal/infrastructure/reporting/compiler"
)

// probeDataset is a client extension's report over a platform catalog.
var probeDataset = &schema.Dataset{
	Key:         "extension-probe",
	Name:        "Extension probe",
	Permission:  "report:extension-probe:read",
	Sources:     []schema.Source{{Key: "currency", Entity: "currency", Alias: "c"}},
	Fields:      []schema.Field{{Name: "code", Access: []schema.FieldAccess{{Entity: "currency", Fields: []string{"code"}}}, Label: "Code", Kind: schema.FieldAttribute, Type: schema.TypeString}},
	ExecutorKey: "extension-probe",
}

type probeExecutor struct{}

func (probeExecutor) BuildQuery(_ context.Context, _ map[string]any, access readpolicy.ReadConstraints) (squirrel.SelectBuilder, error) {
	q, err := access.Source("currency")
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	return squirrel.Select().PrefixExpr(squirrel.Expr("WITH base AS (?)", q.Columns("c.code AS code", "'extension-probe' AS probe_marker"))).From("base"), nil
}

// registerProbe stands in for extensions.Register with one report module.
func registerProbe(reg *v1.FactoryRegistry) {
	reg.RegisterDataset(probeDataset)
	reg.RegisterDatasetExecutor(probeDataset.ExecutorKey, probeExecutor{})
}

// Scheduled reports run against the server's composition: a dataset an
// extension registers is found and reaches its executor, as over HTTP.
func TestScheduledReportsRunExtensionDatasets(t *testing.T) {
	policies, err := security.NewPolicyEngine()
	require.NoError(t, err)
	t.Cleanup(policies.Stop)
	worker := &MultiTenantWorker{factories: content.Compose(content.Services{}, registerProbe)}
	reports := worker.reportCompiler(policies)

	require.Same(t, probeDataset, reports.GetDataset(probeDataset.Key))
	require.NotNil(t, reports.GetDataset("document-journal"), "the built-in reports stay")

	user := appctx.UserContext{UserID: id.New().String(), Permissions: []string{probeDataset.Permission, "catalog:currency:read"}}
	query, err := reports.BuildQuery(appctx.WithUser(context.Background(), &user), compiler.QueryRequest{Dataset: probeDataset.Key})
	require.NoError(t, err)
	sql, _, err := query.ToSql()
	require.NoError(t, err)
	require.Contains(t, sql, "'extension-probe' AS probe_marker", "the extension's executor builds the query")
}
