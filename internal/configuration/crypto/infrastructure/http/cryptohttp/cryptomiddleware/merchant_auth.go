// Package middleware provides the MerchantAPIKey authentication middleware.
package cryptomiddleware

import (
	"context"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"metapus/pkg/sdk/appctx"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/http/middleware"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/logger"
	"metapus/pkg/sdk/tenant"
	"metapus/pkg/sdk/tenantdb"
)

// merchantCtxKey is the context key for the authenticated merchant context.
type merchantCtxKey struct{}

// MerchantContext holds the authenticated merchant identity, injected by MerchantAPIKey middleware.
type MerchantContext struct {
	MerchantID id.ID
	KeyID      id.ID
	Scopes     []merchant.APIKeyScope
}

// WithMerchant stores MerchantContext in the request context. Idempotent
// commands made in this context are scoped to the merchant.
func WithMerchant(ctx context.Context, mc *MerchantContext) context.Context {
	ctx = context.WithValue(ctx, merchantCtxKey{}, mc)
	if mc == nil {
		return ctx
	}
	merchantID := mc.MerchantID.String()
	return middleware.WithCommandScope(ctx, func(*gin.Context) (string, string, error) {
		return "merchant", merchantID, nil
	})
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
//  2. SHA-256(key) -> meta key index resolves tenant/key identity
//  3. Resolve tenant pool via TenantManager
//  4. Verify active key inside the tenant database
//  5. Inject TxManager + pool + MerchantContext into request context
//  6. Fire-and-forget UpdateLastUsed (non-blocking)
//
// The caller does NOT need to send X-Tenant-ID — tenant is resolved from the key.
func MerchantAPIKey(repo MerchantAPIKeyAuthenticator, manager *tenantdb.Manager, metaPool *pgxpool.Pool) gin.HandlerFunc {
	if repo == nil {
		panic("MerchantAPIKey middleware requires repo")
	}
	if manager == nil {
		panic("MerchantAPIKey middleware requires tenant manager")
	}
	if metaPool == nil {
		panic("MerchantAPIKey middleware requires meta pool")
	}

	return func(c *gin.Context) {
		rawKey := c.GetHeader("X-Api-Key")
		if rawKey == "" {
			_ = c.Error(apperror.NewUnauthorized("X-Api-Key header is required"))
			c.Abort()
			return
		}

		keyHash := merchant.HashKey(rawKey)

		var tenantID string
		var indexedKeyID id.ID
		var indexedMerchantID id.ID
		err := metaPool.QueryRow(c.Request.Context(), `
			SELECT tenant_id::text, api_key_id, merchant_id
			FROM merchant_api_key_index
			WHERE key_hash = $1
			  AND is_active = TRUE
			  AND (expires_at IS NULL OR expires_at > NOW())
			LIMIT 1
		`, keyHash).Scan(&tenantID, &indexedKeyID, &indexedMerchantID)
		if err != nil {
			if err != pgx.ErrNoRows {
				logger.Warn(c.Request.Context(), "merchant api: meta key lookup failed", "error", err)
			}
			_ = c.Error(apperror.NewUnauthorized("invalid api key"))
			c.Abort()
			return
		}

		// Bind the tenant's database
		ctx, release, err := middleware.BindTenant(c.Request.Context(), manager, tenantID)
		if err != nil {
			logger.Warn(c.Request.Context(), "merchant api: tenant pool error",
				"tenant_id", tenantID, "error", err)
			_ = c.Error(apperror.NewUnauthorized("invalid tenant or api key"))
			c.Abort()
			return
		}
		defer release()
		c.Request = c.Request.WithContext(ctx)

		// Lookup key by hash (hot-path — uses partial index)
		apiKey, err := repo.GetByHash(c.Request.Context(), keyHash)
		if err != nil {
			_ = c.Error(apperror.NewUnauthorized("invalid api key"))
			c.Abort()
			return
		}
		if apiKey.ID != indexedKeyID || apiKey.MerchantID != indexedMerchantID {
			logger.Warn(c.Request.Context(), "merchant api: tenant key metadata mismatch",
				"tenant_id", tenantID,
				"api_key_id", apiKey.ID,
				"indexed_api_key_id", indexedKeyID,
			)
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
			TenantID: tenant.GetTenant(ctx).ID,
		}
		ctx = appctx.WithUser(ctx, syntheticUser)
		c.Request = c.Request.WithContext(ctx)

		// Best-effort: record last used time without blocking the request.
		// The goroutine binds the tenant again in a context of its own: it is
		// not tied to the request lifetime, holds its own reference to the pool
		// (so pool eviction cannot close it mid-query, CWE-362) and gives up
		// after 5 seconds so it never outlives graceful shutdown.
		go func(keyID id.ID) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			bgCtx, release, err := middleware.BindTenant(bgCtx, manager, tenantID)
			if err != nil {
				logger.Warn(bgCtx, "merchant api: update last_used skipped", "tenant_id", tenantID, "error", err)
				return
			}
			defer release()
			if uerr := repo.UpdateLastUsed(bgCtx, keyID); uerr != nil {
				logger.Warn(bgCtx, "merchant api: update last_used failed",
					"key_id", keyID, "error", uerr)
			}
		}(apiKey.ID)

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
	return slices.Contains(mc.Scopes, scope)
}
