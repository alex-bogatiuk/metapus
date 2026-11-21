package worker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateProductionEnvironmentRequiresMetricExport(t *testing.T) {
	env := map[string]string{"APP_ENV": "production"}
	require.ErrorContains(t, validateProductionEnvironment(func(key string) string { return env[key] }), "OTEL_EXPORTER_OTLP")
	env["OTEL_EXPORTER_OTLP_ENDPOINT"] = "https://otel-collector:4317"
	require.NoError(t, validateProductionEnvironment(func(key string) string { return env[key] }))
	require.NoError(t, validateProductionEnvironment(func(string) string { return "" }), "development needs no export")
}
