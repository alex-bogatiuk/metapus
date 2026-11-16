package main

import (
	"context"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestIntegrationRunnerRequiresExplicitCluster(t *testing.T) {
	t.Setenv("METAPUS_TEST_ADMIN_DSN", "")
	t.Setenv("TENANT_POSTGRES_TEST_URL", "")
	require.ErrorContains(t, run(context.Background()), "must identify")
	t.Setenv("METAPUS_TEST_ADMIN_DSN", "not a PostgreSQL URL")
	require.ErrorContains(t, run(context.Background()), "must be a postgres")
}

func TestIntegrationEnvironmentCannotReuseInputDatabase(t *testing.T) {
	want := map[string]string{
		"DATABASE_URL":                    "postgres://admin/generated_database",
		"METAPUS_TEST_DATABASE_DSN":       "postgres://admin/generated_database",
		"METAPUS_TEST_MIGRATION_DSN":      "postgres://admin/generated_database",
		"METAPUS_TEST_MARKERS_DSN":        "postgres://admin/generated_database",
		"METAPUS_TEST_EXCHANGE_RATES_DSN": "postgres://admin/generated_database",
		"TENANT_POSTGRES_TEST_URL":        "postgres://admin/test_cluster",
		"METAPUS_TEST_INTEGRATIONS_DSN":   "postgres://admin/test_cluster",
		"PATH":                            "tools",
	}
	original := []string{"PATH=tools"}
	for key := range want {
		if key != "PATH" {
			original = append(original, key+"=do-not-touch")
		}
	}
	env := integrationEnv(original, "postgres://admin/test_cluster", "postgres://admin/generated_database")
	counts := map[string]int{}
	got := map[string]string{}
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		counts[key]++
		got[key] = value
	}
	require.Equal(t, want, got)
	for _, count := range counts {
		require.Equal(t, 1, count)
	}
}
