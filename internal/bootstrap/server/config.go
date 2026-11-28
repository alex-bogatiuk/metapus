package server

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/domain/auth"
	"github.com/alex-bogatiuk/metapus/internal/platform/envconfig"
)

// config is everything the platform API server reads from its environment.
// loadConfig validates all of it before any connection is opened, so a
// misconfigured deployment fails at once and names every bad variable. A
// configuration reads its own settings (Configuration.Server).
type config struct {
	AppEnv   string
	LogLevel string
	Port     string

	MetaDatabaseURL       string
	TenantDBUser          string
	TenantDBPassword      string
	TenantMaxPools        int
	TenantMaxConnsPerPool int
	TenantPoolIdleTimeout time.Duration
	PrewarmPools          bool

	JWTSecret                      string
	AuthStateCacheTTL              time.Duration
	AuthStateCacheMaxEntries       int
	SecurityProfileCacheTTL        time.Duration
	SecurityProfileCacheMaxEntries int
	Auth                           auth.ServiceConfig

	PDFRendererURL       string
	ControlPlaneTenantID string
}

func (c config) development() bool { return c.AppEnv == "development" }

func loadConfig() (config, error) {
	env := envconfig.New()
	cfg := config{
		AppEnv:   env.String("APP_ENV", "development"),
		LogLevel: env.String("LOG_LEVEL", "info"),
		Port:     env.String("APP_PORT", "8080"),

		MetaDatabaseURL:       env.Required("META_DATABASE_URL"),
		TenantDBUser:          env.Required("TENANT_DB_USER"),
		TenantDBPassword:      env.Required("TENANT_DB_PASSWORD"),
		TenantMaxPools:        env.PositiveInt("TENANT_MAX_POOLS", 100),
		TenantMaxConnsPerPool: env.PositiveInt("TENANT_MAX_CONNS_PER_POOL", 10),
		TenantPoolIdleTimeout: env.Duration("TENANT_POOL_IDLE_TIMEOUT", 30*time.Minute),
		PrewarmPools:          env.Bool("PREWARM_POOLS", false),

		JWTSecret:                      env.Required("JWT_SECRET"),
		AuthStateCacheTTL:              env.Duration("AUTH_STATE_CACHE_TTL", 5*time.Minute),
		AuthStateCacheMaxEntries:       env.PositiveInt("AUTH_STATE_CACHE_MAX_ENTRIES", 10000),
		SecurityProfileCacheTTL:        env.Duration("SECURITY_PROFILE_CACHE_TTL", 5*time.Minute),
		SecurityProfileCacheMaxEntries: env.PositiveInt("SECURITY_PROFILE_CACHE_MAX_ENTRIES", 10000),
		Auth:                           loadAuthServiceConfig(env),

		PDFRendererURL:       env.String("PDF_RENDERER_URL", ""),
		ControlPlaneTenantID: strings.TrimSpace(env.String("CONTROL_PLANE_TENANT_ID", "")),
	}
	if err := env.Err(); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func loadAuthServiceConfig(env *envconfig.Reader) auth.ServiceConfig {
	cfg := auth.DefaultServiceConfig()
	cfg.AdminMFAMode = parseMFAMode(env, env.String("ADMIN_MFA_MODE", string(cfg.AdminMFAMode)))
	cfg.AdminMFAGracePeriod = time.Duration(env.NonNegativeInt("ADMIN_MFA_GRACE_PERIOD_HOURS", 168)) * time.Hour
	cfg.AdminStepUpTTL = time.Duration(env.PositiveInt("ADMIN_STEP_UP_TTL_SECONDS", 300)) * time.Second
	cfg.AdminSecurityMode = parseAdminSecurityMode(env, env.String("ADMIN_SECURITY_MODE", string(cfg.AdminSecurityMode)))
	cfg.AdminAllowedCIDRs = parseCIDRs(env, "ADMIN_ALLOWED_CIDRS", env.String("ADMIN_ALLOWED_CIDRS", ""))
	cfg.MFAIssuer = env.String("AUTH_MFA_ISSUER", cfg.MFAIssuer)
	cfg.MFAEncryptionKey = parseMFAEncryptionKey(env, strings.TrimSpace(env.String("AUTH_MFA_ENCRYPTION_KEY", "")))
	cfg.AllowSelfRegistration = env.Bool("AUTH_ALLOW_SELF_REGISTRATION", false)
	return cfg
}

func parseMFAEncryptionKey(env *envconfig.Reader, raw string) []byte {
	const name = "AUTH_MFA_ENCRYPTION_KEY"
	if raw == "" {
		env.Fail(name, errors.New("is required for MFA enrollment and step-up"))
		return nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		env.Fail(name, errors.New("must be base64-encoded 32 bytes"))
		return nil
	}
	return key
}

func parseMFAMode(env *envconfig.Reader, value string) auth.MFAMode {
	mode := auth.MFAMode(strings.ToLower(strings.TrimSpace(value)))
	switch mode {
	case auth.MFAModeOff, auth.MFAModeOptional, auth.MFAModeRequired:
		return mode
	default:
		env.Fail("ADMIN_MFA_MODE", fmt.Errorf("must be off, optional or required, got %q", value))
		return auth.MFAModeOff
	}
}

func parseAdminSecurityMode(env *envconfig.Reader, value string) auth.AdminSecurityMode {
	mode := auth.AdminSecurityMode(strings.ToLower(strings.TrimSpace(value)))
	switch mode {
	case auth.AdminSecurityOff, auth.AdminSecurityWarn, auth.AdminSecurityEnforce:
		return mode
	default:
		env.Fail("ADMIN_SECURITY_MODE", fmt.Errorf("must be off, warn or enforce, got %q", value))
		return auth.AdminSecurityOff
	}
}

// parseCIDRs accepts a comma-separated list of CIDR prefixes or bare IP
// addresses; a bare address becomes a single-host prefix.
func parseCIDRs(env *envconfig.Reader, name, value string) []netip.Prefix {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		raw := strings.TrimSpace(part)
		if raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			addr, addrErr := netip.ParseAddr(raw)
			if addrErr != nil {
				env.Fail(name, fmt.Errorf("invalid CIDR %q: %w", raw, err))
				continue
			}
			prefix = netip.PrefixFrom(addr.Unmap(), addr.BitLen())
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes
}
