package automationreport

import (
	"context"
	"errors"
	"github.com/alex-bogatiuk/metapus/internal/application/automation"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/automations"
	"github.com/alex-bogatiuk/metapus/internal/domain/reports/schema"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/reporting/compiler"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type rejectedReportPrincipal struct{ got id.ID }

func (p *rejectedReportPrincipal) ContextFor(_ context.Context, uid id.ID) (context.Context, error) {
	p.got = uid
	return nil, errors.New("permission lookup failed")
}

func TestReportGeneratorRequiresConfiguredExecutorBeforeQuery(t *testing.T) {
	principal := &rejectedReportPrincipal{}
	g := NewReportGenerator(nil, nil, nil, nil, principal)
	uid := id.New()
	_, err := g.GenerateAndStore(context.Background(), &automations.Rule{ReportConfig: &automations.ReportActionConfig{ExecutorUserID: uid}}, time.Now())
	require.ErrorContains(t, err, "permission lookup failed")
	require.Equal(t, uid, principal.got)
}

func TestScheduledReportPeriodUsesDeclaredParametersAndFullCalendarDays(t *testing.T) {
	c := compiler.NewCompiler(metadata.NewRegistry(), []*schema.Dataset{
		{Key: "range", Filters: []schema.FilterDef{{Key: "from_date"}, {Key: "to_date"}}},
		{Key: "balance", Filters: []schema.FilterDef{{Key: "as_of_date"}}},
	}, nil)
	g := NewReportGenerator(c, nil, nil, nil, nil)
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	day := time.Date(2026, 3, 29, 0, 0, 0, 0, loc) // DST day is 23 hours.
	period := automation.ResolvedPeriod{From: day, To: day}
	req, _, err := g.buildQueryRequest(context.Background(), &automations.ReportActionConfig{DatasetKey: "range", PeriodType: automations.PeriodToday}, period)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"from_date": "2026-03-29T00:00:00+01:00", "to_date": "2026-03-30T00:00:00+02:00"}, req.Filters)
	req, _, err = g.buildQueryRequest(context.Background(), &automations.ReportActionConfig{DatasetKey: "balance", PeriodType: automations.PeriodToday}, period)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"as_of_date": "2026-03-29T23:59:59.999999999+02:00"}, req.Filters)
}
