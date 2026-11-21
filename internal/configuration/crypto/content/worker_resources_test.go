package content

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadResourceSettingsDefaultsAndOverrides(t *testing.T) {
	settings, err := loadResourceSettings()
	require.NoError(t, err)
	require.Equal(t, defaultResourceSettings(), settings)

	t.Setenv("CRYPTO_RESOURCE_LEASE_TARGET_ENERGY", "80000")
	t.Setenv("CRYPTO_RESOURCE_SAFETY_MARGIN_PCT", "0")
	t.Setenv("CRYPTO_RESOURCE_RESERVATION_CUTOFF_SECONDS", "90")
	settings, err = loadResourceSettings()
	require.NoError(t, err)
	require.Equal(t, resourceSettings{LeaseTargetEnergy: 80000, SafetyMarginPct: 0, ReservationCutoff: 90 * time.Second}, settings)
}

func TestLoadResourceSettingsRejectsInvalidValues(t *testing.T) {
	t.Setenv("CRYPTO_RESOURCE_LEASE_TARGET_ENERGY", "65k")
	t.Setenv("CRYPTO_RESOURCE_SAFETY_MARGIN_PCT", "-5")
	t.Setenv("CRYPTO_RESOURCE_RESERVATION_CUTOFF_SECONDS", "2m")

	_, err := loadResourceSettings()

	require.ErrorContains(t, err, "CRYPTO_RESOURCE_LEASE_TARGET_ENERGY")
	require.ErrorContains(t, err, "CRYPTO_RESOURCE_SAFETY_MARGIN_PCT")
	require.ErrorContains(t, err, "CRYPTO_RESOURCE_RESERVATION_CUTOFF_SECONDS")
}
