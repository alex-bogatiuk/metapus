package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigureMetricsWithoutEndpointIsExplicitNoop(t *testing.T) {
	shutdown, err := ConfigureMetrics(context.Background(), "worker", "test", func(string) string { return "" })
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.NoError(t, shutdown(context.Background()))
}

func TestConfigureMetricsRequiresEnvironmentReader(t *testing.T) {
	_, err := ConfigureMetrics(context.Background(), "worker", "test", nil)
	require.ErrorContains(t, err, "environment reader")
}
