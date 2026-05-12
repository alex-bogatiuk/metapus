package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/infrastructure/http/v1/dto"
	mw "metapus/internal/infrastructure/http/v1/middleware"
	"metapus/pkg/logger"
)

const (
	_defaultInvoiceTTLMinutes = 60
	_minInvoiceTTLMinutes     = 5
	_maxInvoiceTTLMinutes     = 1440 // 24h
)

// MerchantInvoiceHandler handles /merchant/v1/invoices endpoints.
// All methods require MerchantAPIKey middleware to have run first.
type MerchantInvoiceHandler struct {
	invoiceService domain.DocumentService[*crypto_invoice.CryptoInvoice]
	apiKeyRepo     merchant.APIKeyRepository
	tokenResolver  merchantTokenResolver
}

// merchantTokenResolver resolves token code → (walletAddress, tokenCode, networkName).
// Filled from a lightweight SQL read (same pattern as payment_page.go).
type merchantTokenResolver interface {
	ResolveToken(ctx context.Context, tokenCode string) (tokenID id.ID, err error)
	FetchInvoiceDisplay(ctx context.Context, invoiceID id.ID) (walletAddress, tokenCode, network string, err error)
}

// NewMerchantInvoiceHandler creates the handler.
func NewMerchantInvoiceHandler(
	invoiceService domain.DocumentService[*crypto_invoice.CryptoInvoice],
	apiKeyRepo merchant.APIKeyRepository,
) *MerchantInvoiceHandler {
	return &MerchantInvoiceHandler{
		invoiceService: invoiceService,
		apiKeyRepo:     apiKeyRepo,
	}
}

// CreateInvoice handles POST /merchant/v1/invoices.
//
// Idempotency: if orderId is provided and a matching invoice already exists
// for this merchant, the existing invoice is returned without creating a duplicate.
func (h *MerchantInvoiceHandler) CreateInvoice(c *gin.Context) {
	mc := mw.GetMerchant(c.Request.Context())
	if mc == nil {
		_ = c.Error(apperror.NewUnauthorized("merchant context missing"))
		c.Abort()
		return
	}

	if !mc.HasScope(merchant.ScopeInvoiceCreate) {
		_ = c.Error(apperror.NewForbidden("missing scope: invoice:create"))
		c.Abort()
		return
	}

	var req dto.CreateMerchantInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation(err.Error()))
		c.Abort()
		return
	}

	ctx := c.Request.Context()
	pool := tenant.MustGetPool(ctx)

	// 1. Idempotency: check for existing invoice by orderId
	if req.OrderID != nil && *req.OrderID != "" {
		existing, walletAddress, tokenCode, network, err := h.findByOrderID(ctx, pool, mc.MerchantID, *req.OrderID)
		if err == nil && existing != nil {
			c.JSON(http.StatusOK, dto.MerchantInvoiceFromEntity(existing, walletAddress, tokenCode, network))
			return
		}
	}

	// 2. Resolve token by currency code
	tokenID, err := resolveTokenByCode(ctx, pool, req.Currency)
	if err != nil {
		_ = c.Error(apperror.NewValidation(fmt.Sprintf("unknown currency: %s", req.Currency)).
			WithDetail("field", "currency"))
		c.Abort()
		return
	}

	// 3. Validate and build amount (minor units)
	if req.Amount <= 0 {
		_ = c.Error(apperror.NewValidation("amount must be a positive integer (minor units)").
			WithDetail("field", "amount"))
		c.Abort()
		return
	}
	amount := types.NewCryptoAmountFromInt64(req.Amount)

	// 4. Build domain entity
	inv := crypto_invoice.NewCryptoInvoice(mc.MerchantID, tokenID, amount)
	inv.Status = crypto_invoice.InvoiceStatusCreated

	// API key audit trail — link invoice to the key that created it.
	keyID := mc.KeyID
	inv.APIKeyID = &keyID

	// TTL
	ttl := _defaultInvoiceTTLMinutes
	if req.TTLMinutes != nil {
		ttl = *req.TTLMinutes
	}
	if ttl < _minInvoiceTTLMinutes || ttl > _maxInvoiceTTLMinutes {
		_ = c.Error(apperror.NewValidation(
			fmt.Sprintf("ttlMinutes must be between %d and %d", _minInvoiceTTLMinutes, _maxInvoiceTTLMinutes)).
			WithDetail("field", "ttlMinutes"))
		c.Abort()
		return
	}
	inv.ExpiresAt = time.Now().Add(time.Duration(ttl) * time.Minute)

	// Optional fields
	if req.OrderID != nil {
		inv.OrderID = *req.OrderID
	}
	if req.Description != nil {
		inv.Description = *req.Description
	}
	if req.CallbackURL != nil {
		// CWE-918: validate before storing to prevent SSRF at webhook delivery time.
		if err := merchant.ValidateCallbackURL(*req.CallbackURL); err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		inv.CallbackURL = *req.CallbackURL
	}
	if req.CustomerEmail != nil {
		inv.CustomerEmail = *req.CustomerEmail
	}

	// 5. Create via service (triggers hooks: wallet lease, outbox, etc.)
	if err := h.invoiceService.Create(ctx, inv); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	// 6. Reload to get wallet address (assigned during creation hook)
	walletAddress, tokenCode, network, err := fetchInvoiceDisplay(ctx, pool, inv.ID)
	if err != nil {
		// Non-fatal: respond with empty address, the payment page will have it
		logger.Warn(ctx, "merchant api: failed to resolve invoice display",
			"invoice_id", inv.ID, "error", err)
		walletAddress = ""
		tokenCode = req.Currency
		network = ""
	}

	c.JSON(http.StatusCreated, dto.MerchantInvoiceFromEntity(inv, walletAddress, tokenCode, network))
}

// GetInvoice handles GET /merchant/v1/invoices/:id.
func (h *MerchantInvoiceHandler) GetInvoice(c *gin.Context) {
	mc := mw.GetMerchant(c.Request.Context())
	if mc == nil {
		_ = c.Error(apperror.NewUnauthorized("merchant context missing"))
		c.Abort()
		return
	}

	if !mc.HasScope(merchant.ScopeInvoiceRead) {
		_ = c.Error(apperror.NewForbidden("missing scope: invoice:read"))
		c.Abort()
		return
	}

	invoiceID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid invoice id"))
		c.Abort()
		return
	}

	ctx := c.Request.Context()

	inv, err := h.invoiceService.GetByID(ctx, invoiceID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	// Enforce ownership — merchant can only read their own invoices
	if inv.MerchantID != mc.MerchantID {
		_ = c.Error(apperror.NewNotFound("invoice", invoiceID.String()))
		c.Abort()
		return
	}

	pool := tenant.MustGetPool(ctx)
	walletAddress, tokenCode, network, err := fetchInvoiceDisplay(ctx, pool, inv.ID)
	if err != nil {
		walletAddress = ""
		tokenCode = ""
		network = ""
	}

	c.JSON(http.StatusOK, dto.MerchantInvoiceFromEntity(inv, walletAddress, tokenCode, network))
}

// ─────────────────────────────────────────────────────────────────
// API Key management endpoints (for admin routes, not merchant routes)
// These are called from /api/v1/catalog/merchants/:id/api-keys
// ─────────────────────────────────────────────────────────────────

// MerchantAPIKeyHandler manages API keys for a merchant via the protected admin API.
type MerchantAPIKeyHandler struct {
	repo merchant.APIKeyRepository
}

// NewMerchantAPIKeyHandler creates the handler.
func NewMerchantAPIKeyHandler(repo merchant.APIKeyRepository) *MerchantAPIKeyHandler {
	return &MerchantAPIKeyHandler{repo: repo}
}

// CreateKey handles POST /catalog/merchants/:merchantId/api-keys.
func (h *MerchantAPIKeyHandler) CreateKey(c *gin.Context) {
	merchantID, err := id.Parse(c.Param("merchantId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid merchant id"))
		c.Abort()
		return
	}

	var req dto.CreateMerchantAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation(err.Error()))
		c.Abort()
		return
	}

	// Convert string scopes to typed scopes
	var scopes []merchant.APIKeyScope
	if len(req.Scopes) > 0 {
		scopes = make([]merchant.APIKeyScope, len(req.Scopes))
		for i, s := range req.Scopes {
			scopes[i] = merchant.APIKeyScope(s)
		}
	}

	// Capture which platform user is issuing this key (JWT context, always present here).
	var createdByUserID *id.ID
	if uc := appctx.GetUser(c.Request.Context()); uc != nil && uc.UserID != "" {
		if uid, parseErr := id.Parse(uc.UserID); parseErr == nil {
			createdByUserID = &uid
		}
	}

	plaintext, key, err := merchant.GenerateKey(merchantID, req.Name, scopes, req.ExpiresAt, createdByUserID)
	if err != nil {
		_ = c.Error(apperror.NewInternal(err))
		c.Abort()
		return
	}

	if err := key.Validate(c.Request.Context()); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if err := h.repo.Create(c.Request.Context(), key); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, dto.MerchantAPIKeyToCreateResponse(key, plaintext))
}

// ListKeys handles GET /catalog/merchants/:merchantId/api-keys.
func (h *MerchantAPIKeyHandler) ListKeys(c *gin.Context) {
	merchantID, err := id.Parse(c.Param("merchantId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid merchant id"))
		c.Abort()
		return
	}

	keys, err := h.repo.ListByMerchant(c.Request.Context(), merchantID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	items := make([]dto.MerchantAPIKeyListItem, len(keys))
	for i, k := range keys {
		items[i] = dto.MerchantAPIKeyToListItem(k)
	}

	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// RevokeKey handles DELETE /catalog/merchants/:merchantId/api-keys/:keyId.
func (h *MerchantAPIKeyHandler) RevokeKey(c *gin.Context) {
	merchantID, err := id.Parse(c.Param("merchantId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid merchant id"))
		c.Abort()
		return
	}
	keyID, err := id.Parse(c.Param("keyId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid key id"))
		c.Abort()
		return
	}

	if err := h.repo.Revoke(c.Request.Context(), keyID, merchantID); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

// ─────────────────────────────────────────────────────────────────
// Private helpers
// ─────────────────────────────────────────────────────────────────

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// findByOrderID looks up an existing invoice by merchant + order_id.
func (h *MerchantInvoiceHandler) findByOrderID(
	ctx context.Context, pool querier, merchantID id.ID, orderID string,
) (*crypto_invoice.CryptoInvoice, string, string, string, error) {
	const q = `
		SELECT ci.id
		FROM doc_crypto_invoices ci
		WHERE ci.merchant_id = $1 AND ci.order_id = $2
		LIMIT 1`

	var invID id.ID
	if err := pool.QueryRow(ctx, q, merchantID, orderID).Scan(&invID); err != nil {
		return nil, "", "", "", err
	}

	inv, err := h.invoiceService.GetByID(ctx, invID)
	if err != nil {
		return nil, "", "", "", err
	}

	walletAddress, tokenCode, network, _ := fetchInvoiceDisplay(ctx, pool, invID)
	return inv, walletAddress, tokenCode, network, nil
}

// fetchInvoiceDisplay retrieves wallet address + token info for an invoice in one query.
func fetchInvoiceDisplay(ctx context.Context, pool querier, invoiceID id.ID) (walletAddress, tokenCode, network string, err error) {
	const q = `
		SELECT
			COALESCE(w.address, '') AS wallet_address,
			COALESCE(t.code, '') AS token_code,
			COALESCE(n.name, '') AS network_name
		FROM doc_crypto_invoices ci
		LEFT JOIN cat_wallets w ON w.id = ci.wallet_id
		LEFT JOIN cat_tokens t ON t.id = ci.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE ci.id = $1`

	err = pool.QueryRow(ctx, q, invoiceID).Scan(&walletAddress, &tokenCode, &network)
	return
}

// resolveTokenByCode looks up a token ID by its code (e.g. "USDT_TRC20").
func resolveTokenByCode(ctx context.Context, pool querier, code string) (id.ID, error) {
	const q = `SELECT id FROM cat_tokens WHERE code = $1 AND deletion_mark = FALSE LIMIT 1`
	var tokenID id.ID
	if err := pool.QueryRow(ctx, q, code).Scan(&tokenID); err != nil {
		if err == pgx.ErrNoRows {
			return id.ID{}, fmt.Errorf("token not found: %s", code)
		}
		return id.ID{}, fmt.Errorf("resolve token: %w", err)
	}
	return tokenID, nil
}
