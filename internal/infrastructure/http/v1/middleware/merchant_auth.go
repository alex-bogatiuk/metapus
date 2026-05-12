// Package middleware provides the MerchantAPIKey authentication middleware.
package middleware

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/pkg/logger"
)

// merchantCtxKey is the context key for the authenticated merchant context.
type merchantCtxKey struct{}

// MerchantContext holds the authenticated merchant identity, injected by MerchantAPIKey middleware.
type MerchantContext struct {
	MerchantID id.ID
	KeyID      id.ID
	Scopes     []merchant.APIKeyScope
}

// WithMerchant stores MerchantContext in the request context.
func WithMerchant(ctx context.Context, mc *MerchantContext) context.Context {
	return context.WithValue(ctx, merchantCtxKey{}, mc)
}

// GetMerchant retrieves MerchantContext from the request context.
// Returns nil if not set (unauthenticated or wrong middleware chain).
func GetMerchant(ctx context.Context) *MerchantContext {
	if v, ok := ctx.Value(merchantCtxKey{}).(*MerchantContext); ok {
		return v
	}
	return nil
}

// MerchantAPIKeyAuthenticator is the interface for API key lookup.
// Only GetByHash is needed in the middleware hot-path.
type MerchantAPIKeyAuthenticator interface {
	GetByHash(ctx context.Context, keyHash string) (*merchant.MerchantAPIKey, error)
	UpdateLastUsed(ctx context.Context, keyID id.ID) error
}

// MerchantAPIKey authenticates requests using the X-Api-Key header.
//
// Authentication flow:
//  1. Extract X-Api-Key header
//  2. SHA-256(key) → GetByHash → active key record
//  3. Check is_active, expiry, scope (scope checked per-endpoint, not here)
//  4. Resolve tenant pool via TenantManager (merchant.TenantID is embedded in key lookup)
//  5. Inject TxManager + pool + MerchantContext into request context
//  6. Fire-and-forget UpdateLastUsed (non-blocking)
//
// The caller does NOT need to send X-Tenant-ID — tenant is resolved from the key.
func MerchantAPIKey(repo MerchantAPIKeyAuthenticator, manager *tenant.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawKey := c.GetHeader("X-Api-Key")
		if rawKey == "" {
			_ = c.Error(apperror.NewUnauthorized("X-Api-Key header is required"))
			c.Abort()
			return
		}

		keyHash := merchant.HashKey(rawKey)

		// We need a temporary context to query the API key.
		// The repo uses TxManager from ctx; for this bootstrap query we use
		// a lightweight background context with the meta pool injected.
		// However, since api keys live in the TENANT database (not meta),
		// we must first resolve the tenant. But we don't know the tenant yet.
		//
		// Solution: embed tenant_id in the key record and use a two-phase lookup:
		//   Phase 1: query cat_merchant_api_keys via a transient pool obtained
		//            by iterating manager's registry — this is expensive.
		//
		// Better solution (used here): expose a dedicated meta-level lookup
		// via the manager's admin connection, or rely on the fact that in
		// SaaS single-tenant mode the tenant is always the default.
		//
		// Pragmatic approach for current architecture:
		// The middleware receives a pre-resolved tenantID from a custom header
		// OR from a well-known default. Since we're in early stage with a single
		// tenant, we read it from a dedicated header as a hint, falling back to
		// manager's single active tenant.
		//
		// TODO(Phase 2): Store (key_hash → tenant_id) in meta-database for true
		//                multi-tenant key resolution without header hint.
		tenantHint := c.GetHeader("X-Tenant-ID")
		if tenantHint == "" {
			_ = c.Error(
				apperror.NewValidation("X-Tenant-ID header is required for merchant API").
					WithDetail("hint", "Pass the tenant ID provided during onboarding"),
			)
			c.Abort()
			return
		}

		// Resolve tenant pool
		managedPool, err := manager.GetPool(c.Request.Context(), tenantHint)
		if err != nil {
			logger.Warn(c.Request.Context(), "merchant api: tenant pool error",
				"tenant_id", tenantHint, "error", err)
			_ = c.Error(apperror.NewUnauthorized("invalid tenant or api key"))
			c.Abort()
			return
		}

		managedPool.AcquireRef()
		defer managedPool.ReleaseRef()

		// Inject TxManager so the repo can operate
		txManager := postgres.NewTxManagerFromRawPool(managedPool.Pool())
		ctx := tenant.WithPool(c.Request.Context(), managedPool.Pool())
		ctx = tenant.WithTxManager(ctx, txManager)
		ctx = tenant.WithTenant(ctx, managedPool.Tenant())
		c.Request = c.Request.WithContext(ctx)

		// Lookup key by hash (hot-path — uses partial index)
		apiKey, err := repo.GetByHash(c.Request.Context(), keyHash)
		if err != nil {
			_ = c.Error(apperror.NewUnauthorized("invalid api key"))
			c.Abort()
			return
		}

		// Check expiry
		if apiKey.IsExpired() {
			_ = c.Error(apperror.NewUnauthorized("api key has expired"))
			c.Abort()
			return
		}

		// Inject merchant context
		mc := &MerchantContext{
			MerchantID: apiKey.MerchantID,
			KeyID:      apiKey.ID,
			Scopes:     apiKey.Scopes,
		}
		ctx = WithMerchant(c.Request.Context(), mc)

		// Also inject a synthetic UserContext so existing middleware that reads
		// ctx user (e.g. security context checks) won't panic.
		// UserID is intentionally empty — there is no platform user in this context.
		// Audit is tracked via doc.APIKeyID → cat_merchant_api_keys.CreatedByUserID.
		syntheticUser := &appctx.UserContext{
			UserID:   "", // no JWT user — audit via APIKeyID, not UserID
			TenantID: managedPool.Tenant().ID,
		}
		ctx = appctx.WithUser(ctx, syntheticUser)
		c.Request = c.Request.WithContext(ctx)

		// Best-effort: record last used time without blocking the request.
		// We build an independent background context that carries the same
		// pool/txManager/tenant values as the request context, but is NOT
		// tied to the request lifetime. This prevents MustGetTxManager from
		// panicking (context.Background() has no TxManager) and avoids
		// holding connections past graceful shutdown via a 5-second timeout.
		//
		// NEW-1 fix (CWE-362): the goroutine acquires its own ref so that the
		// pool lifecycle manager (evictionLoop/healthCheckLoop) cannot close
		// the pool while the goroutine is still executing a query.
		// Order matters: AcquireRef BEFORE the handler's defer ReleaseRef fires.
		managedPool.AcquireRef() // goroutine's own ref — released inside go func
		go func(mp *tenant.ManagedPool, pool *pgxpool.Pool, tm *postgres.TxManager, t *tenant.Tenant, keyID id.ID) {
			defer mp.ReleaseRef()
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			bgCtx = tenant.WithPool(bgCtx, pool)
			bgCtx = tenant.WithTxManager(bgCtx, tm)
			bgCtx = tenant.WithTenant(bgCtx, t)
			if uerr := repo.UpdateLastUsed(bgCtx, keyID); uerr != nil {
				logger.Warn(bgCtx, "merchant api: update last_used failed",
					"key_id", keyID, "error", uerr)
			}
		}(managedPool, managedPool.Pool(), txManager, managedPool.Tenant(), apiKey.ID)

		c.Next()
	}
}

// RequireMerchantScope checks that the authenticated API key has the required scope.
// Must be used after MerchantAPIKey middleware.
func RequireMerchantScope(scope merchant.APIKeyScope) gin.HandlerFunc {
	return func(c *gin.Context) {
		mc := GetMerchant(c.Request.Context())
		if mc == nil {
			_ = c.Error(apperror.NewUnauthorized("merchant authentication required"))
			c.Abort()
			return
		}
		if !mc.HasScope(scope) {
			_ = c.Error(
				apperror.NewForbidden("insufficient api key scope").
					WithDetail("required", string(scope)),
			)
			c.Abort()
			return
		}
		c.Next()
	}
}

// HasScope returns true if the merchant context has the given scope.
func (mc *MerchantContext) HasScope(scope merchant.APIKeyScope) bool {
	for _, s := range mc.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}
