package worker

import (
	"context"
	"testing"

	"github.com/Masterminds/squirrel"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration"
	"metapus/internal/configuration/none"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain/reports/schema"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/readpolicy"
	"metapus/internal/infrastructure/reporting/compiler"
	"metapus/internal/metadata"
)

type reportProbeItem struct{ entity.Catalog }

func (reportProbeItem) EntityDefinition() entity.Definition { return probeCatalog }

var probeCatalog = entity.Definition{
	Kind: entity.CatalogKind, Key: "report_probe_item", Name: "ReportProbeItem",
	Route: "report-probe-items", Table: "cat_report_probe_items", Code: entity.CodeRule{Prefix: "RP"},
}

var probeDataset = &schema.Dataset{
	Key:         "extension-probe",
	Name:        "Extension probe",
	Permission:  "report:extension-probe:read",
	Sources:     []schema.Source{{Key: "items", Entity: probeCatalog.Key, Alias: "c"}},
	Fields:      []schema.Field{{Name: "code", Access: []schema.FieldAccess{{Entity: probeCatalog.Key, Fields: []string{"code"}}}, Label: "Code", Kind: schema.FieldAttribute, Type: schema.TypeString}},
	ExecutorKey: "extension-probe",
}

type probeExecutor struct{}

func (probeExecutor) BuildQuery(_ context.Context, _ map[string]any, access readpolicy.ReadConstraints) (squirrel.SelectBuilder, error) {
	q, err := access.Source("items")
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	return squirrel.Select().PrefixExpr(squirrel.Expr("WITH base AS (?)", q.Columns("c.code AS code", "'extension-probe' AS probe_marker"))).From("base"), nil
}

// merchantScopedProbe scopes probe items by their merchant, as the crypto
// configuration scopes its documents, so security profiles restrict them.
type merchantScopedProbe struct {
	*v1.AutoCatalog[reportProbeItem, *reportProbeItem]
}

func (merchantScopedProbe) RLSDimensions() map[string]string {
	return map[string]string{"merchant": "merchant_id"}
}

// reportingConfiguration is a configuration with one report of its own.
type reportingConfiguration struct{ configuration.Configuration }

var configurationDataset = &schema.Dataset{Key: "configuration-probe", Name: "Configuration probe", Permission: "report:configuration-probe:read"}

func (reportingConfiguration) Register(reg *v1.FactoryRegistry) {
	reg.RegisterDataset(configurationDataset)
}

func registerProbe(reg *v1.FactoryRegistry) {
	reg.RegisterCatalog(merchantScopedProbe{v1.Catalog[reportProbeItem](metadata.Presentation{Singular: "Probe item", Plural: "Probe items"})})
	reg.RegisterDataset(probeDataset)
	reg.RegisterDatasetExecutor(probeDataset.ExecutorKey, probeExecutor{})
}

// Scheduled reports run against the server's composition: a dataset an
// extension registers is found and reaches its executor, as over HTTP.
func TestScheduledReportsRunExtensionDatasets(t *testing.T) {
	policies, err := security.NewPolicyEngine()
	require.NoError(t, err)
	t.Cleanup(policies.Stop)
	worker := &MultiTenantWorker{factories: configuration.Compose(reportingConfiguration{none.New()}, registerProbe)}
	reports := worker.reportCompiler(policies)

	require.Same(t, probeDataset, reports.GetDataset(probeDataset.Key))
	require.Same(t, configurationDataset, reports.GetDataset(configurationDataset.Key), "the configuration's reports stay")

	user := appctx.UserContext{UserID: id.New().String(), Permissions: []string{probeDataset.Permission}}
	request := compiler.QueryRequest{Dataset: probeDataset.Key}
	ctx := appctx.WithUser(t.Context(), &user)
	_, err = reports.BuildQuery(ctx, request)
	require.True(t, apperror.IsForbidden(err), "expected a source permission error, got %v", err)
	denied, _ := apperror.AsAppError(err)
	sourcePermission := probeCatalog.Permission() + ":read"
	require.Equal(t, sourcePermission, denied.Details["required_permission"])

	user.Permissions = append(user.Permissions, sourcePermission)
	query, err := reports.BuildQuery(ctx, request)
	require.NoError(t, err)
	sql, _, err := query.ToSql()
	require.NoError(t, err)
	require.Contains(t, sql, probeCatalog.Table)
	require.Contains(t, sql, "'extension-probe' AS probe_marker", "the extension's executor builds the query")
}
