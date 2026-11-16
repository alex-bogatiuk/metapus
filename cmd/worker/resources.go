package main

import (
	"fmt"
	"time"

	"metapus/internal/platform/envconfig"
)

// resourceSettings tune TRON resource rental for every tenant's crypto
// processor. They are read once at startup so a bad value stops the worker
// instead of being replaced by a default inside a tenant goroutine.
type resourceSettings struct {
	LeaseTargetEnergy int64
	SafetyMarginPct   int64
	ReservationCutoff time.Duration
}

func defaultResourceSettings() resourceSettings {
	return resourceSettings{LeaseTargetEnergy: 65000, SafetyMarginPct: 10, ReservationCutoff: 120 * time.Second}
}

func loadResourceSettings() (resourceSettings, error) {
	defaults := defaultResourceSettings()
	env := envconfig.New()
	settings := resourceSettings{
		LeaseTargetEnergy: nonNegativeInt64(env, "CRYPTO_RESOURCE_LEASE_TARGET_ENERGY", defaults.LeaseTargetEnergy),
		SafetyMarginPct:   nonNegativeInt64(env, "CRYPTO_RESOURCE_SAFETY_MARGIN_PCT", defaults.SafetyMarginPct),
		ReservationCutoff: time.Duration(nonNegativeInt64(env, "CRYPTO_RESOURCE_RESERVATION_CUTOFF_SECONDS", int64(defaults.ReservationCutoff/time.Second))) * time.Second,
	}
	if err := env.Err(); err != nil {
		return resourceSettings{}, err
	}
	return settings, nil
}

func nonNegativeInt64(env *envconfig.Reader, key string, fallback int64) int64 {
	value := env.Int64(key, fallback)
	if value < 0 {
		env.Fail(key, fmt.Errorf("must not be negative, got %d", value))
		return fallback
	}
	return value
}
