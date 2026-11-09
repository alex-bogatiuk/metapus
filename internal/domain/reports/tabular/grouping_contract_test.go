package tabular_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/json"
	reportcontract "metapus/internal/domain/reports/contract"
	"metapus/internal/domain/reports/tabular"
)

func TestGroupingWireContractMatchesBrowserFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/grouping.json")
	require.NoError(t, err)
	var fixture struct {
		Items   []map[string]any             `json:"items"`
		GroupBy []string                     `json:"groupBy"`
		Totals  []reportcontract.ReportTotal `json:"totals"`
		Rows    json.RawMessage              `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	rows, err := tabular.BuildDisplayRows(fixture.Items, fixture.GroupBy, fixture.Totals)
	require.NoError(t, err)
	wire, err := json.Marshal(rows)
	require.NoError(t, err)
	require.JSONEq(t, string(fixture.Rows), string(wire), "the same fixture is consumed by frontend grouping and HTTP response schemas")
}
