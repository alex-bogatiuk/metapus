// cmd/updater/docker.go
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// DockerClient abstracts Docker Engine API for testability.
type DockerClient interface {
	// PullImage downloads an image from registry. progressFn is called with (current, total) bytes.
	PullImage(ctx context.Context, imageRef string, authToken string, progressFn func(current, total int64)) error

	// StartContainer creates and starts a new container from the given image.
	// Returns the container ID. Container joins the specified network without alias initially.
	// oldInfo is used to inherit port bindings and healthcheck from the previous container.
	StartContainer(ctx context.Context, imageRef string, env []string, networkName string, oldInfo *ContainerInfo) (containerID string, err error)

	// WaitHealthy polls the container's health status until healthy or timeout.
	WaitHealthy(ctx context.Context, containerID string, timeout time.Duration) error

	// NetworkConnect adds a container to a network with the specified alias.
	NetworkConnect(ctx context.Context, networkName, containerID, alias string) error

	// NetworkDisconnect removes a container from a network.
	NetworkDisconnect(ctx context.Context, networkName, containerID string) error

	// StopContainer gracefully stops a container with the given timeout.
	StopContainer(ctx context.Context, containerID string, timeout time.Duration) error

	// RemoveContainer removes a stopped container.
	RemoveContainer(ctx context.Context, containerID string) error

	// InspectContainer returns basic info about a container.
	InspectContainer(ctx context.Context, nameOrID string) (*ContainerInfo, error)

	// Close releases Docker client resources.
	Close() error
}

// ContainerInfo holds basic container metadata.
type ContainerInfo struct {
	ID           string
	Name         string
	Image        string
	Running      bool
	PortBindings nat.PortMap
	ExposedPorts nat.PortSet
	Healthcheck  *container.HealthConfig
}

// --- Implementation ---

// dockerClient wraps the official Docker SDK client.
type dockerClient struct {
	cli *client.Client
}

// NewDockerClient creates a Docker client from the default environment
// (DOCKER_HOST or /var/run/docker.sock).
func NewDockerClient() (DockerClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}
	return &dockerClient{cli: cli}, nil
}

func (d *dockerClient) PullImage(ctx context.Context, imageRef string, authToken string, progressFn func(current, total int64)) error {
	opts := image.PullOptions{}
	if authToken != "" {
		authConfig := registry.AuthConfig{
			Username: "oauth2",
			Password: authToken,
		}
		authJSON, err := json.Marshal(authConfig)
		if err != nil {
			return fmt.Errorf("marshal auth: %w", err)
		}
		opts.RegistryAuth = base64.StdEncoding.EncodeToString(authJSON)
	}

	reader, err := d.cli.ImagePull(ctx, imageRef, opts)
	if err != nil {
		return fmt.Errorf("pull image %s: %w", imageRef, err)
	}
	defer func() { _ = reader.Close() }()

	// Parse Docker pull progress stream (JSON lines)
	decoder := json.NewDecoder(reader)
	var totalBytes, currentBytes int64
	for {
		var event struct {
			Status         string `json:"status"`
			ProgressDetail struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
			Error string `json:"error"`
		}
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("decode pull progress: %w", err)
		}
		if event.Error != "" {
			return fmt.Errorf("pull error: %s", event.Error)
		}
		if event.ProgressDetail.Total > 0 {
			currentBytes = event.ProgressDetail.Current
			totalBytes = event.ProgressDetail.Total
			if progressFn != nil {
				progressFn(currentBytes, totalBytes)
			}
		}
	}
	return nil
}

func (d *dockerClient) StartContainer(ctx context.Context, imageRef string, env []string, networkName string, oldInfo *ContainerInfo) (string, error) {
	cfg := &container.Config{
		Image: imageRef,
		Env:   env,
	}
	hostCfg := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
	}

	// Inherit port bindings and healthcheck from the old container.
	if oldInfo != nil {
		if len(oldInfo.PortBindings) > 0 {
			hostCfg.PortBindings = oldInfo.PortBindings
		}
		if len(oldInfo.ExposedPorts) > 0 {
			cfg.ExposedPorts = oldInfo.ExposedPorts
		}
		if oldInfo.Healthcheck != nil {
			cfg.Healthcheck = oldInfo.Healthcheck
		}
	}

	resp, err := d.cli.ContainerCreate(ctx,
		cfg,
		hostCfg,
		&network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				networkName: {},
			},
		},
		nil, // platform
		"",  // auto-generated name
	)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}

	if err := d.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		// Cleanup on failed start
		_ = d.cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return "", fmt.Errorf("start container: %w", err)
	}

	return resp.ID, nil
}

func (d *dockerClient) WaitHealthy(ctx context.Context, containerID string, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("health check timeout after %s for container %s", timeout, containerID[:12])
		case <-ticker.C:
			info, err := d.cli.ContainerInspect(ctx, containerID)
			if err != nil {
				continue
			}
			if info.State == nil {
				continue
			}
			// If container has a HEALTHCHECK defined
			if info.State.Health != nil {
				if info.State.Health.Status == "healthy" {
					return nil
				}
				continue
			}
			// No HEALTHCHECK — just check if running
			if info.State.Running {
				return nil
			}
		}
	}
}

func (d *dockerClient) NetworkConnect(ctx context.Context, networkName, containerID, alias string) error {
	return d.cli.NetworkConnect(ctx, networkName, containerID, &network.EndpointSettings{
		Aliases: []string{alias},
	})
}

func (d *dockerClient) NetworkDisconnect(ctx context.Context, networkName, containerID string) error {
	return d.cli.NetworkDisconnect(ctx, networkName, containerID, false)
}

func (d *dockerClient) StopContainer(ctx context.Context, containerID string, timeout time.Duration) error {
	timeoutSec := int(timeout.Seconds())
	return d.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeoutSec})
}

func (d *dockerClient) RemoveContainer(ctx context.Context, containerID string) error {
	return d.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{
		RemoveVolumes: false,
		Force:         true,
	})
}

func (d *dockerClient) InspectContainer(ctx context.Context, nameOrID string) (*ContainerInfo, error) {
	info, err := d.cli.ContainerInspect(ctx, nameOrID)
	if err != nil {
		// Fallback 1: try finding by docker-compose service name label.
		containerID, findErr := d.findContainerByService(ctx, nameOrID)
		if findErr != nil {
			// Fallback 2: try finding by running image prefix (for updater-created containers).
			containerID, findErr = d.findContainerByImage(ctx, nameOrID)
			if findErr != nil {
				return nil, fmt.Errorf("inspect container %s: %w", nameOrID, err)
			}
		}
		info, err = d.cli.ContainerInspect(ctx, containerID)
		if err != nil {
			return nil, fmt.Errorf("inspect container %s (by ID %s): %w", nameOrID, containerID[:12], err)
		}
	}

	name := strings.TrimPrefix(info.Name, "/")
	running := info.State != nil && info.State.Running

	ci := &ContainerInfo{
		ID:      info.ID,
		Name:    name,
		Image:   info.Config.Image,
		Running: running,
	}

	// Extract port bindings from host config.
	if info.HostConfig != nil && len(info.HostConfig.PortBindings) > 0 {
		ci.PortBindings = info.HostConfig.PortBindings
	}
	// Extract exposed ports from config.
	if info.Config != nil && len(info.Config.ExposedPorts) > 0 {
		ci.ExposedPorts = info.Config.ExposedPorts
	}
	// Extract healthcheck config.
	if info.Config != nil && info.Config.Healthcheck != nil {
		ci.Healthcheck = info.Config.Healthcheck
	}

	return ci, nil
}

// findContainerByService locates a container by its docker-compose service label.
func (d *dockerClient) findContainerByService(ctx context.Context, serviceName string) (string, error) {
	filterArgs := filters.NewArgs(
		filters.Arg("label", "com.docker.compose.service="+serviceName),
		filters.Arg("status", "running"),
	)
	containers, err := d.cli.ContainerList(ctx, container.ListOptions{Filters: filterArgs})
	if err != nil {
		return "", fmt.Errorf("list containers by service %s: %w", serviceName, err)
	}
	if len(containers) == 0 {
		return "", fmt.Errorf("no running container found for service %s", serviceName)
	}
	return containers[0].ID, nil
}

// findContainerByImage locates a running container by image name prefix.
// This handles containers created by the updater that don't have compose labels.
func (d *dockerClient) findContainerByImage(ctx context.Context, serviceName string) (string, error) {
	filterArgs := filters.NewArgs(
		filters.Arg("status", "running"),
	)
	containers, err := d.cli.ContainerList(ctx, container.ListOptions{Filters: filterArgs})
	if err != nil {
		return "", fmt.Errorf("list containers: %w", err)
	}

	// Look for containers whose image starts with the registry prefix
	// serviceName = "metapus-app", image = "ghcr.io/.../metapus:v1.5.0"
	for _, c := range containers {
		// Match by image containing "metapus:" (the service image, not updater/postgres)
		if strings.Contains(c.Image, "metapus:") && !strings.Contains(c.Image, "updater") {
			return c.ID, nil
		}
	}
	return "", fmt.Errorf("no running container found matching service %s", serviceName)
}

func (d *dockerClient) Close() error {
	return d.cli.Close()
}
