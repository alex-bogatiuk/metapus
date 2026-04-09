// cmd/updater/orchestrator.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"

	"metapus/pkg/logger"
)

// AvailableUpdate holds info about a new version that can be installed.
type AvailableUpdate struct {
	Available      bool   `json:"available"`
	CurrentImage   string `json:"currentImage"`
	CurrentVersion string `json:"currentVersion"`
	LatestImage    string `json:"latestImage,omitempty"`
	LatestVersion  string `json:"latestVersion,omitempty"`
}

// Orchestrator manages the full update lifecycle using a state machine.
// Only one update operation can be active at a time.
type Orchestrator struct {
	cfg      *Config
	docker   DockerClient
	state    *StateStore
	log      *logger.Logger
	mu       sync.Mutex // prevents concurrent update operations
	http     *http.Client
	registry *RegistryChecker
}

// NewOrchestrator creates a new update orchestrator.
func NewOrchestrator(cfg *Config, docker DockerClient, state *StateStore, registry *RegistryChecker, log *logger.Logger) *Orchestrator {
	return &Orchestrator{
		cfg:      cfg,
		docker:   docker,
		state:    state,
		log:      log.WithComponent("orchestrator"),
		http:     &http.Client{Timeout: 60 * time.Second},
		registry: registry,
	}
}

// RecoverIfNeeded checks WAL state and handles interrupted operations.
// Called once at startup.
func (o *Orchestrator) RecoverIfNeeded(ctx context.Context) {
	if !o.state.NeedsRecovery() {
		return
	}

	st := o.state.Get()
	o.log.Warn("recovering from interrupted update",
		"phase", st.Phase,
		"target_image", st.TargetImage,
	)

	// For any interrupted phase, transition to failed and let user decide
	switch st.Phase {
	case PhasePulling, PhaseChecking:
		// Safe to just reset — no containers were started
		o.state.AppendLog("recovery: interrupted during pull/check, resetting to idle")
		_ = o.state.Reset()

	case PhaseStarting, PhaseHealthWait:
		// New container may exist — try to clean it up
		if st.NewContainerID != "" {
			o.state.AppendLog("recovery: cleaning up new container " + st.NewContainerID[:12])
			_ = o.docker.StopContainer(ctx, st.NewContainerID, 10*time.Second)
			_ = o.docker.RemoveContainer(ctx, st.NewContainerID)
		}
		_ = o.state.SetError(PhaseFailed, fmt.Errorf("interrupted during %s, cleaned up", st.Phase))

	case PhaseSwitching:
		// Critical: traffic may have been partially switched.
		// Attempt to restore old container alias.
		o.state.AppendLog("recovery: interrupted during traffic switch — restoring old container")
		if st.OldContainerID != "" {
			_ = o.docker.NetworkConnect(ctx, o.cfg.DockerNetwork, st.OldContainerID, o.cfg.ContainerName)
		}
		if st.NewContainerID != "" {
			_ = o.docker.NetworkDisconnect(ctx, o.cfg.DockerNetwork, st.NewContainerID)
			_ = o.docker.StopContainer(ctx, st.NewContainerID, 10*time.Second)
			_ = o.docker.RemoveContainer(ctx, st.NewContainerID)
		}
		_ = o.state.SetError(PhaseFailed, fmt.Errorf("interrupted during switch, old container restored"))

	case PhaseMigrating:
		// New container is live, migration was in progress.
		// Can't safely restore — mark as failed, let user decide.
		_ = o.state.SetError(PhaseFailed, fmt.Errorf("interrupted during migration — manual intervention required"))

	default:
		_ = o.state.SetError(PhaseFailed, fmt.Errorf("unknown interrupted phase: %s", st.Phase))
	}
}

// Start begins the full update lifecycle to the target image tag.
func (o *Orchestrator) Start(ctx context.Context, targetTag string) error {
	if !o.mu.TryLock() {
		return fmt.Errorf("another update operation is already running")
	}

	st := o.state.Get()
	if st.Phase != PhaseIdle && st.Phase != PhaseDone && st.Phase != PhaseFailed {
		o.mu.Unlock()
		return fmt.Errorf("cannot start update: current phase is %s", st.Phase)
	}

	targetImage := o.cfg.RegistryImage + ":" + targetTag

	// Initialize state
	if err := o.state.Update(func(s *UpdateState) {
		*s = UpdateState{
			Phase:       PhaseChecking,
			TargetImage: targetImage,
			TargetTag:   targetTag,
			StartedAt:   time.Now(),
			Log:         []LogEntry{{Time: time.Now(), Message: fmt.Sprintf("update started → %s", targetImage)}},
		}
	}); err != nil {
		o.mu.Unlock()
		return fmt.Errorf("init state: %w", err)
	}

	// Run phases in background goroutine.
	// Use context.Background() — the HTTP request ctx would cancel after response.
	go func() {
		defer o.mu.Unlock()
		bgCtx := context.Background()
		o.runPhases(bgCtx, targetImage, targetTag)
	}()

	return nil
}

// Rollback attempts to restore the previous container after a failed or completed update.
func (o *Orchestrator) Rollback(ctx context.Context) error {
	if !o.mu.TryLock() {
		return fmt.Errorf("another update operation is already running")
	}

	st := o.state.Get()
	if st.Phase != PhaseDone && st.Phase != PhaseFailed {
		o.mu.Unlock()
		return fmt.Errorf("rollback only available in done or failed phase (current: %s)", st.Phase)
	}

	if st.OldContainerID == "" {
		o.mu.Unlock()
		return fmt.Errorf("no old container to restore")
	}

	if err := o.state.Transition(PhaseRollback); err != nil {
		o.mu.Unlock()
		return fmt.Errorf("transition to rollback: %w", err)
	}

	go func() {
		defer o.mu.Unlock()
		bgCtx := context.Background()
		o.runRollback(bgCtx, st)
	}()

	return nil
}

// CheckAvailable queries the current running container version and returns info.
// Does NOT check the registry (manual trigger in UI will call Start directly with a tag).
func (o *Orchestrator) CheckAvailable(ctx context.Context) (*AvailableUpdate, error) {
	result := &AvailableUpdate{
		Available: false,
	}

	// Get current server version via /api/v1/system/version
	resp, err := o.http.Get(o.cfg.ServerURL + "/api/v1/system/version")
	if err != nil {
		return result, fmt.Errorf("get server version: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("server version returned %d", resp.StatusCode)
	}

	var versionResp struct {
		Version   string `json:"version"`
		BuildTime string `json:"buildTime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&versionResp); err != nil {
		return result, fmt.Errorf("decode version: %w", err)
	}

	result.CurrentVersion = versionResp.Version
	result.CurrentImage = o.cfg.RegistryImage + ":" + versionResp.Version

	// Get current container image info
	info, err := o.docker.InspectContainer(ctx, o.cfg.ContainerName)
	if err == nil {
		result.CurrentImage = info.Image

		// If API returns "dev" (binary built without VERSION arg),
		// extract actual version from Docker image tag instead.
		if result.CurrentVersion == "dev" || result.CurrentVersion == "" {
			if parts := strings.SplitN(info.Image, ":", 2); len(parts) == 2 && parts[1] != "" {
				result.CurrentVersion = parts[1]
			}
		}
	}

	// Enrich with registry checker data (cached background result)
	if o.registry != nil {
		if latest := o.registry.Latest(); latest != nil && latest.Tag != "" {
			result.LatestVersion = latest.Tag
			result.LatestImage = o.cfg.RegistryImage + ":" + latest.Tag

			// Fresh semver comparison between resolved current and latest.
			// We don't trust latest.Available because it was computed against a
			// potentially stale currentVersion snapshot.
			currentSV, currentOK := parseSemver(result.CurrentVersion)
			latestSV, latestOK := parseSemver(latest.Tag)
			if !currentOK {
				// Non-semver current (e.g. "dev") → any valid latest tag is an upgrade.
				result.Available = latestOK
			} else if latestOK {
				result.Available = latestSV.greaterThan(currentSV)
			}
		}
	}

	return result, nil
}

// --- Internal phase execution ---

func (o *Orchestrator) runPhases(ctx context.Context, targetImage, targetTag string) {
	var err error

	// Phase 1: Pull image
	o.state.AppendLog("pulling image " + targetImage)
	if err = o.state.Transition(PhasePulling); err != nil {
		o.failUpdate(err)
		return
	}
	if err = o.docker.PullImage(ctx, targetImage, o.cfg.RegistryToken, func(current, total int64) {
		detail := fmt.Sprintf("Скачано %s", formatBytesGo(current))
		if total > 0 {
			detail = fmt.Sprintf("Скачано %s / %s", formatBytesGo(current), formatBytesGo(total))
		}
		_ = o.state.Update(func(s *UpdateState) {
			s.PullCurrent = current
			s.PullTotal = total
			s.PhaseDetail = detail
		})
	}); err != nil {
		o.failUpdate(fmt.Errorf("pull image: %w", err))
		return
	}
	o.state.AppendLog("image pulled successfully")
	_ = o.state.Update(func(s *UpdateState) { s.PhaseDetail = "" })

	// Phase 2: Discover old container
	oldInfo, err := o.docker.InspectContainer(ctx, o.cfg.ContainerName)
	if err != nil {
		o.failUpdate(fmt.Errorf("inspect current container: %w", err))
		return
	}
	if err = o.state.Update(func(s *UpdateState) {
		s.OldContainerID = oldInfo.ID
	}); err != nil {
		o.failUpdate(err)
		return
	}
	o.state.AppendLog(fmt.Sprintf("current container: %s (%s)", oldInfo.Name, oldInfo.ID[:12]))

	// Phase 3: Start new container (WITHOUT host port bindings — old is still using them).
	if err = o.state.Transition(PhaseStarting); err != nil {
		o.failUpdate(err)
		return
	}

	// Inherit environment from old container
	env := o.getContainerEnv(ctx, oldInfo.ID)
	// Start without port bindings for blue-green: old container still holds host ports.
	newID, err := o.docker.StartContainer(ctx, targetImage, env, o.cfg.DockerNetwork, nil)
	if err != nil {
		o.failUpdate(fmt.Errorf("start new container: %w", err))
		return
	}
	if err = o.state.Update(func(s *UpdateState) {
		s.NewContainerID = newID
	}); err != nil {
		o.failUpdate(err)
		return
	}
	o.state.AppendLog(fmt.Sprintf("new container started: %s", newID[:12]))

	// Phase 4: Health check
	if err = o.state.Transition(PhaseHealthWait); err != nil {
		o.cleanupNewContainer(ctx, newID)
		o.failUpdate(err)
		return
	}
	o.state.AppendLog(fmt.Sprintf("waiting for health check (timeout: %s)", o.cfg.HealthTimeout))
	_ = o.state.Update(func(s *UpdateState) {
		s.PhaseDetail = "Ожидание ответа от нового контейнера..."
	})
	if err = o.docker.WaitHealthy(ctx, newID, o.cfg.HealthTimeout); err != nil {
		o.state.AppendLog("health check failed, removing new container")
		o.cleanupNewContainer(ctx, newID)
		o.failUpdate(fmt.Errorf("health check: %w", err))
		return
	}
	o.state.AppendLog("health check passed")
	_ = o.state.Update(func(s *UpdateState) { s.PhaseDetail = "" })

	// Phase 5: Switch traffic (connect-first)
	if err = o.state.Transition(PhaseSwitching); err != nil {
		o.cleanupNewContainer(ctx, newID)
		o.failUpdate(err)
		return
	}
	o.state.AppendLog("switching traffic: connecting new container with alias")

	// Disconnect new container first (it was auto-connected without alias during StartContainer).
	_ = o.docker.NetworkDisconnect(ctx, o.cfg.DockerNetwork, newID)

	// Connect new container WITH alias (both respond temporarily)
	if err = o.docker.NetworkConnect(ctx, o.cfg.DockerNetwork, newID, o.cfg.ContainerName); err != nil {
		o.cleanupNewContainer(ctx, newID)
		o.failUpdate(fmt.Errorf("connect new container: %w", err))
		return
	}

	// Disconnect old container from network (traffic now goes to new only)
	if err = o.docker.NetworkDisconnect(ctx, o.cfg.DockerNetwork, oldInfo.ID); err != nil {
		// Non-fatal: old container might already be disconnected
		o.state.AppendLog(fmt.Sprintf("warning: disconnect old container: %s", err))
	}
	o.state.AppendLog("traffic switched to new container")

	// Phase 6: Trigger DB migration on new server
	if err = o.state.Transition(PhaseMigrating); err != nil {
		o.failUpdate(err)
		return
	}
	o.state.AppendLog("triggering database migration")
	_ = o.state.Update(func(s *UpdateState) {
		s.PhaseDetail = "Запуск миграции базы данных..."
	})
	if err = o.triggerMigration(ctx); err != nil {
		o.failUpdate(fmt.Errorf("db migration: %w", err))
		return
	}
	o.state.AppendLog("database migration completed")
	_ = o.state.Update(func(s *UpdateState) { s.PhaseDetail = "" })

	// Phase 7: Done — stop old container, then recreate new with host port bindings.
	if err = o.state.Transition(PhaseDone); err != nil {
		o.state.AppendLog(fmt.Sprintf("warning: transition to done: %s", err))
	}
	o.state.AppendLog("stopping old container")
	if err = o.docker.StopContainer(ctx, oldInfo.ID, o.cfg.DrainTimeout); err != nil {
		o.state.AppendLog(fmt.Sprintf("warning: stop old container: %s", err))
	}

	// Recreate new container WITH inherited host port bindings (now that old is stopped).
	if len(oldInfo.PortBindings) > 0 {
		o.state.AppendLog("recreating container with host port bindings")
		recreatedID, recreateErr := o.recreateWithPorts(ctx, newID, targetImage, env, oldInfo)
		if recreateErr != nil {
			o.state.AppendLog(fmt.Sprintf("warning: recreate with ports failed: %s (container still running without host ports)", recreateErr))
		} else {
			newID = recreatedID
			_ = o.state.Update(func(s *UpdateState) {
				s.NewContainerID = newID
			})
			o.state.AppendLog(fmt.Sprintf("container recreated with port bindings: %s", newID[:12]))
		}
	}

	_ = o.state.Update(func(s *UpdateState) {
		s.CompletedAt = time.Now()
	})
	o.state.AppendLog(fmt.Sprintf("update completed successfully → %s", targetTag))
	o.log.Info("update completed", "target", targetImage)
}

// recreateWithPorts stops the portless new container and creates a replacement
// with the old container's host port bindings. Returns the new container ID.
func (o *Orchestrator) recreateWithPorts(ctx context.Context, currentID, imageRef string, env []string, oldInfo *ContainerInfo) (string, error) {
	// Stop and remove portless container
	if err := o.docker.StopContainer(ctx, currentID, 10*time.Second); err != nil {
		return "", fmt.Errorf("stop portless container: %w", err)
	}
	_ = o.docker.RemoveContainer(ctx, currentID)

	// Create new container WITH port bindings + healthcheck
	newID, err := o.docker.StartContainer(ctx, imageRef, env, o.cfg.DockerNetwork, oldInfo)
	if err != nil {
		return "", fmt.Errorf("create container with ports: %w", err)
	}

	// Re-establish network alias
	_ = o.docker.NetworkDisconnect(ctx, o.cfg.DockerNetwork, newID)
	if err = o.docker.NetworkConnect(ctx, o.cfg.DockerNetwork, newID, o.cfg.ContainerName); err != nil {
		return newID, fmt.Errorf("reconnect with alias: %w", err)
	}

	return newID, nil
}

func (o *Orchestrator) runRollback(ctx context.Context, st UpdateState) {
	o.state.AppendLog("starting rollback")

	// 1. Restore old container network alias
	if st.OldContainerID != "" {
		o.state.AppendLog("restoring old container alias")
		if err := o.docker.NetworkConnect(ctx, o.cfg.DockerNetwork, st.OldContainerID, o.cfg.ContainerName); err != nil {
			// Old container might need to be restarted
			o.state.AppendLog(fmt.Sprintf("reconnect old container: %s (attempting restart)", err))
			_ = o.docker.StopContainer(ctx, st.OldContainerID, 5*time.Second)
			if startErr := o.restartOldContainer(ctx, st.OldContainerID); startErr != nil {
				_ = o.state.SetError(PhaseFailed, fmt.Errorf("cannot restore old container: %w", startErr))
				return
			}
		}
	}

	// 2. Disconnect and remove new container
	if st.NewContainerID != "" {
		o.state.AppendLog("removing new container")
		_ = o.docker.NetworkDisconnect(ctx, o.cfg.DockerNetwork, st.NewContainerID)
		_ = o.docker.StopContainer(ctx, st.NewContainerID, 10*time.Second)
		_ = o.docker.RemoveContainer(ctx, st.NewContainerID)
	}

	// 3. If migration was triggered, rollback via server API
	if st.Phase == PhaseDone || st.Phase == PhaseMigrating {
		o.state.AppendLog("rolling back database migration")
		if err := o.rollbackMigration(ctx); err != nil {
			o.state.AppendLog(fmt.Sprintf("warning: db rollback: %s", err))
		}
	}

	_ = o.state.Reset()
	o.state.AppendLog("rollback completed, system restored")
	o.log.Info("rollback completed")
}

// --- Helpers ---

func (o *Orchestrator) failUpdate(err error) {
	o.log.Error("update failed", "error", err)
	_ = o.state.SetError(PhaseFailed, err)
}

func (o *Orchestrator) cleanupNewContainer(ctx context.Context, containerID string) {
	_ = o.docker.StopContainer(ctx, containerID, 10*time.Second)
	_ = o.docker.RemoveContainer(ctx, containerID)
}

func (o *Orchestrator) triggerMigration(ctx context.Context) error {
	url := fmt.Sprintf("%s/internal/tenants/%s/trigger-update", o.cfg.ServerURL, o.cfg.TenantID)

	// Retry a few times — after network switching, DNS may need time to propagate.
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			o.state.AppendLog(fmt.Sprintf("migration trigger retry %d/5: %s", attempt+1, lastErr))
			time.Sleep(5 * time.Second)
		}
		resp, err := o.http.Post(url, "application/json", nil)
		if err != nil {
			lastErr = fmt.Errorf("trigger update request: %w", err)
			continue
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
			// Poll migration status until complete
			return o.waitForMigration(ctx)
		}

		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		// "already up to date" is not an error — schema is current, no migration needed.
		if strings.Contains(bodyStr, "already up to date") {
			o.state.AppendLog("schema already up to date, no migration needed")
			return nil
		}

		lastErr = fmt.Errorf("trigger update: status %d, body: %s", resp.StatusCode, bodyStr)
	}
	return lastErr
}

func (o *Orchestrator) waitForMigration(ctx context.Context) error {
	url := fmt.Sprintf("%s/internal/tenants/%s/migration-status", o.cfg.ServerURL, o.cfg.TenantID)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(5 * time.Minute) // Max migration time
	started := time.Now()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("migration timeout after 5 minutes")
		case <-ticker.C:
			elapsed := time.Since(started).Truncate(time.Second)
			_ = o.state.Update(func(s *UpdateState) {
				s.PhaseDetail = fmt.Sprintf("Выполняется миграция... (%s)", elapsed)
			})

			resp, err := o.http.Get(url)
			if err != nil {
				o.state.AppendLog(fmt.Sprintf("migration check error: %s", err))
				continue
			}

			var status struct {
				Status string `json:"status"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
				_ = resp.Body.Close()
				continue
			}
			_ = resp.Body.Close()

			switch status.Status {
			case "active":
				_ = o.state.Update(func(s *UpdateState) {
					s.PhaseDetail = fmt.Sprintf("Миграция завершена (%s)", elapsed)
				})
				return nil // Migration completed successfully
			case "migration_failed":
				return fmt.Errorf("migration failed on new server")
			case "updating":
				continue // Still running
			default:
				o.state.AppendLog(fmt.Sprintf("unexpected migration status: %s", status.Status))
			}
		}
	}
}

func (o *Orchestrator) rollbackMigration(ctx context.Context) error {
	url := fmt.Sprintf("%s/internal/tenants/%s/rollback-update", o.cfg.ServerURL, o.cfg.TenantID)
	resp, err := o.http.Post(url, "application/json", nil)
	if err != nil {
		return fmt.Errorf("rollback request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("rollback: status %d, body: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (o *Orchestrator) getContainerEnv(ctx context.Context, containerID string) []string {
	info, err := o.docker.InspectContainer(ctx, containerID)
	if err != nil {
		return nil
	}
	// We need the full env — use the raw Docker inspect
	rawInfo, err := o.docker.(*dockerClient).cli.ContainerInspect(ctx, containerID)
	if err != nil || rawInfo.Config == nil {
		o.state.AppendLog("warning: could not inherit env from old container")
		return nil
	}

	// Filter out internal Docker env vars
	var env []string
	for _, e := range rawInfo.Config.Env {
		if !strings.HasPrefix(e, "PATH=") && !strings.HasPrefix(e, "HOSTNAME=") {
			env = append(env, e)
		}
	}

	_ = info // suppress unused
	return env
}

func (o *Orchestrator) restartOldContainer(ctx context.Context, containerID string) error {
	// The old container might be stopped; try starting it
	dc, ok := o.docker.(*dockerClient)
	if !ok {
		return fmt.Errorf("cannot restart: docker client type assertion failed")
	}

	return dc.cli.ContainerStart(ctx, containerID, container.StartOptions{})
}

// formatBytesGo formats bytes into a human-readable string (e.g. "45.2 MB").
func formatBytesGo(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.0f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
