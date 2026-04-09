// cmd/updater/registry.go
//
// RegistryChecker periodically polls a Docker Registry v2 API
// to discover new image tags. Supports GHCR and any OCI-compliant registry.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// VersionInfo describes the latest discovered version.
type VersionInfo struct {
	Tag       string    `json:"tag"`
	Available bool      `json:"available"` // true if tag > current
	CheckedAt time.Time `json:"checkedAt"`
}

// RegistryChecker polls a Docker v2 registry for new tags.
type RegistryChecker struct {
	registryURL string // e.g. "https://ghcr.io"
	repository  string // e.g. "alex-bogatiuk/metapus"
	authToken   string // GitHub PAT or empty for public repos
	httpCli     *http.Client

	mu        sync.RWMutex
	latest    *VersionInfo
	lastError error
}

// NewRegistryChecker creates a checker from the full image reference.
// imageRef example: "ghcr.io/alex-bogatiuk/metapus"
func NewRegistryChecker(imageRef, authToken string) *RegistryChecker {
	registryURL, repository := parseImageRef(imageRef)
	return &RegistryChecker{
		registryURL: registryURL,
		repository:  repository,
		authToken:   authToken,
		httpCli:     &http.Client{Timeout: 15 * time.Second},
	}
}

// Latest returns the last discovered version info (thread-safe).
func (r *RegistryChecker) Latest() *VersionInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.latest == nil {
		return nil
	}
	cp := *r.latest
	return &cp
}

// LastError returns the last check error (thread-safe).
func (r *RegistryChecker) LastError() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastError
}

// Check fetches all tags from the registry, finds the highest semver tag,
// and compares it against currentVersion.
func (r *RegistryChecker) Check(ctx context.Context, currentVersion string) (*VersionInfo, error) {
	tags, err := r.listTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}

	currentSV, currentOK := parseSemver(currentVersion)

	var best semver
	var found bool
	for _, tag := range tags {
		sv, ok := parseSemver(tag)
		if !ok {
			continue // skip non-semver tags like "latest", "main", etc.
		}
		if sv.Pre != "" {
			continue // skip pre-release tags
		}
		if !found || sv.greaterThan(best) {
			best = sv
			found = true
		}
	}

	if !found {
		return &VersionInfo{Available: false, CheckedAt: time.Now()}, nil
	}

	info := &VersionInfo{
		Tag:       best.Raw,
		CheckedAt: time.Now(),
		Available: currentOK && best.greaterThan(currentSV),
	}

	r.mu.Lock()
	r.latest = info
	r.lastError = nil
	r.mu.Unlock()

	return info, nil
}

// RunBackground starts periodic registry checks in a goroutine.
// Runs until ctx is cancelled.
func (r *RegistryChecker) RunBackground(ctx context.Context, interval time.Duration, currentVersionFn func() string) {
	// Initial check after short delay (let server start)
	go func() {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		r.runCheck(ctx, currentVersionFn())

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.runCheck(ctx, currentVersionFn())
			}
		}
	}()
}

func (r *RegistryChecker) runCheck(ctx context.Context, currentVersion string) {
	_, err := r.Check(ctx, currentVersion)
	if err != nil {
		r.mu.Lock()
		r.lastError = err
		r.mu.Unlock()
	}
}

// --- Docker Registry v2 API ---

func (r *RegistryChecker) listTags(ctx context.Context) ([]string, error) {
	// Step 1: Get bearer token via token exchange (OCI distribution spec)
	token, err := r.getAuthToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	// Step 2: Fetch tags list
	tagsURL := fmt.Sprintf("%s/v2/%s/tags/list", r.registryURL, r.repository)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tagsURL, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := r.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch tags: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tags list returned %d", resp.StatusCode)
	}

	var result struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode tags: %w", err)
	}

	return result.Tags, nil
}

// getAuthToken obtains a short-lived bearer token via the Docker v2
// token authentication flow. Even public GHCR repos require a bearer
// token — the only difference is that no Basic auth header is sent.
func (r *RegistryChecker) getAuthToken(ctx context.Context) (string, error) {
	// GHCR token endpoint
	tokenURL := fmt.Sprintf("%s/token?service=%s&scope=repository:%s:pull",
		r.registryURL,
		strings.TrimPrefix(r.registryURL, "https://"),
		r.repository,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return "", err
	}

	// If a PAT is provided, attach Basic auth for private repos.
	// For public repos authToken is empty — anonymous token exchange still works.
	if r.authToken != "" {
		basicAuth := base64.StdEncoding.EncodeToString([]byte("oauth2:" + r.authToken))
		req.Header.Set("Authorization", "Basic "+basicAuth)
	}

	resp, err := r.httpCli.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}

	return tokenResp.Token, nil
}

// --- Semver parsing ---

type semver struct {
	Major, Minor, Patch int
	Pre                 string // pre-release suffix (e.g. "beta.1")
	Raw                 string // original tag string (e.g. "v1.5.0")
}

// parseSemver parses tags like "v1.2.3", "v1.2.3-beta.1", "1.0.0".
// Returns false for non-semver tags like "latest", "main", "sha-abc123".
func parseSemver(tag string) (semver, bool) {
	raw := tag
	tag = strings.TrimPrefix(tag, "v")

	// Split pre-release
	parts := strings.SplitN(tag, "-", 2)
	versionStr := parts[0]
	pre := ""
	if len(parts) > 1 {
		pre = parts[1]
	}

	// Split major.minor.patch
	nums := strings.Split(versionStr, ".")
	if len(nums) != 3 {
		return semver{}, false
	}

	major, err := strconv.Atoi(nums[0])
	if err != nil {
		return semver{}, false
	}
	minor, err := strconv.Atoi(nums[1])
	if err != nil {
		return semver{}, false
	}
	patch, err := strconv.Atoi(nums[2])
	if err != nil {
		return semver{}, false
	}

	// Ensure raw has "v" prefix for consistency
	if !strings.HasPrefix(raw, "v") {
		raw = "v" + raw
	}

	return semver{
		Major: major,
		Minor: minor,
		Patch: patch,
		Pre:   pre,
		Raw:   raw,
	}, true
}

// greaterThan returns true if a > b (semver ordering).
// Pre-release versions are considered lower than releases.
func (a semver) greaterThan(b semver) bool {
	if a.Major != b.Major {
		return a.Major > b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor > b.Minor
	}
	if a.Patch != b.Patch {
		return a.Patch > b.Patch
	}
	// Release > pre-release
	if a.Pre == "" && b.Pre != "" {
		return true
	}
	return false
}

// --- Helpers ---

// parseImageRef splits "ghcr.io/alex-bogatiuk/metapus" →
// ("https://ghcr.io", "alex-bogatiuk/metapus")
func parseImageRef(imageRef string) (registryURL, repository string) {
	parts := strings.SplitN(imageRef, "/", 2)
	if len(parts) == 2 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":")) {
		return "https://" + parts[0], parts[1]
	}
	// Docker Hub default
	return "https://registry-1.docker.io", imageRef
}
