// cmd/updater/config.go
package main

import (
	"fmt"
	"os"
	"time"
)

// Config holds the updater agent configuration, loaded from environment variables.
type Config struct {
	// Port is the HTTP server port for the updater API.
	Port string

	// ServerURL is the base URL of the Metapus server (internal network).
	ServerURL string

	// TenantID is the tenant UUID for single-tenant mode.
	TenantID string

	// RegistryImage is the full image name without tag (e.g., ghcr.io/alex-bogatiuk/metapus).
	RegistryImage string

	// RegistryToken is the auth token for pulling images from GHCR.
	RegistryToken string

	// DockerNetwork is the Docker network name for container communication.
	DockerNetwork string

	// ContainerName is the name/alias of the main Metapus container.
	ContainerName string

	// StateFilePath is the path to the WAL state file.
	StateFilePath string

	// HealthTimeout is the max time to wait for the new container health check.
	HealthTimeout time.Duration

	// DrainTimeout is the grace period for draining old container connections.
	DrainTimeout time.Duration

	// CheckInterval is howoften to poll the registry for new tags (0 = disabled).
	CheckInterval time.Duration

	// LogLevel controls logging verbosity.
	LogLevel string
}

// LoadConfig reads configuration from environment variables with sensible defaults.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Port:          envOrDefault("UPDATER_PORT", "9090"),
		ServerURL:     envOrDefault("SERVER_URL", "http://metapus-app:8080"),
		TenantID:      os.Getenv("TENANT_ID"),
		RegistryImage: envOrDefault("REGISTRY_IMAGE", "ghcr.io/alex-bogatiuk/metapus"),
		RegistryToken: os.Getenv("REGISTRY_TOKEN"),
		DockerNetwork: envOrDefault("DOCKER_NETWORK", "metapus-net"),
		ContainerName: envOrDefault("CONTAINER_NAME", "metapus-app"),
		StateFilePath: envOrDefault("STATE_FILE", "/data/state.json"),
		HealthTimeout: envDurationOrDefault("HEALTH_TIMEOUT", 60*time.Second),
		DrainTimeout:  envDurationOrDefault("DRAIN_TIMEOUT", 30*time.Second),
		CheckInterval: envDurationOrDefault("CHECK_INTERVAL", 30*time.Minute),
		LogLevel:      envOrDefault("LOG_LEVEL", "info"),
	}

	if cfg.TenantID == "" {
		return nil, fmt.Errorf("TENANT_ID is required")
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return fallback
}
