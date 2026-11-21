package aml

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
)

func testPolicy() Policy {
	return Policy{Version: "test-v1", LowMax: 3000, MediumMax: 7000, MaxReportAge: time.Hour, DecisionTTL: 10 * time.Minute,
		AcceptedSignals: []string{"wallet", "sanctions"}, CriticalSignals: []string{"sanctions"}}
}

func testReport(now time.Time, score ScoreBP) Report {
	return Report{ID: id.New(), Provider: "getblock_net", ProviderAccountID: id.New(), CheckHash: "vendor-hash",
		RequestDigest: "request-digest", ContractVersion: "v1", AdapterVersion: "v1", MappingVersion: "v1",
		Score: &score, Signals: []Signal{{Name: "wallet", Value: "1"}}, CoverageAccepted: true, CompletedAt: now, ReceivedAt: now}
}

func TestPolicyScoreBoundaries(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		give ScoreBP
		want Class
	}{{0, Low}, {3000, Low}, {3001, Medium}, {7000, Medium}, {7001, High}, {10000, High}}
	for _, tt := range tests {
		t.Run(string(tt.want)+"/"+time.Duration(tt.give).String(), func(t *testing.T) {
			result, err := testPolicy().Assess(testReport(now, tt.give), now)
			require.NoError(t, err)
			require.Equal(t, tt.want, result.Class)
			require.Equal(t, now.Add(10*time.Minute), result.ExpiresAt)
		})
	}
}

func TestPolicyIncompleteEvidenceAndCriticalSignals(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name string
		give func(*Report)
		want Class
	}{
		{"missing score", func(r *Report) { r.Score = nil }, Unknown},
		{"invalid score", func(r *Report) { value := ScoreBP(10001); r.Score = &value }, Unknown},
		{"coverage", func(r *Report) { r.CoverageAccepted = false }, Unknown},
		{"missing mapping", func(r *Report) { r.MappingVersion = "" }, Unknown},
		{"expired at boundary", func(r *Report) { r.CompletedAt = now.Add(-time.Hour) }, Unknown},
		{"future completion", func(r *Report) { r.CompletedAt = now.Add(time.Second) }, Unknown},
		{"unknown category", func(r *Report) { r.Signals = []Signal{{Name: "new_category", Value: "0"}} }, Unknown},
		{"known high is not hidden by unknown category", func(r *Report) {
			score := ScoreBP(9000)
			r.Score = &score
			r.Signals = []Signal{{Name: "new_category", Value: "0"}}
		}, High},
		{"critical", func(r *Report) { r.Signals = []Signal{{Name: "sanctions", Value: "0.00001"}} }, High},
		{"critical zero", func(r *Report) { r.Signals = []Signal{{Name: "sanctions", Value: "0.000"}} }, Low},
		{"malformed signal", func(r *Report) { r.Signals = []Signal{{Name: "wallet", Value: "bad"}} }, Unknown},
		{"invalid signal range", func(r *Report) { r.Signals = []Signal{{Name: "wallet", Value: "1.1"}} }, Unknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := testReport(now, 0)
			tt.give(&report)
			result, err := testPolicy().Assess(report, now)
			require.NoError(t, err)
			require.Equal(t, tt.want, result.Class)
		})
	}
}

func TestDecisionDoesNotExtendReportFreshness(t *testing.T) {
	now := time.Now().UTC()
	report := testReport(now.Add(-59*time.Minute), 100)
	report.ReceivedAt = now // repolling does not refresh vendor completion
	result, err := testPolicy().Assess(report, now)
	require.NoError(t, err)
	require.Equal(t, now.Add(time.Minute), result.ExpiresAt)
}

func TestPoolClassNeverDilutesRisk(t *testing.T) {
	require.Equal(t, High, PoolClass(Low, Low, High))
	require.Equal(t, High, PoolClass(Unknown, High))
	require.Equal(t, Unknown, PoolClass(Low, Unknown, Medium))
	require.Equal(t, Medium, PoolClass(Low, Medium))
	require.Equal(t, Unknown, PoolClass())
}

func TestReportCloneDoesNotAliasEvidence(t *testing.T) {
	now := time.Now().UTC()
	report := testReport(now, 100)
	report.RawResponse = []byte("evidence")
	report.DatasetAsOf = &now
	copyReport := report.Clone()
	*copyReport.Score = 9000
	copyReport.Signals[0].Name = "changed"
	copyReport.RawResponse[0] = 'X'
	*copyReport.DatasetAsOf = now.Add(time.Hour)
	require.Equal(t, ScoreBP(100), *report.Score)
	require.Equal(t, "wallet", report.Signals[0].Name)
	require.Equal(t, "evidence", string(report.RawResponse))
	require.Equal(t, now, *report.DatasetAsOf)
}
