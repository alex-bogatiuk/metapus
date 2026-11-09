package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/checkout"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/infrastructure/http/v1/dto"
	mw "metapus/internal/infrastructure/http/v1/middleware"
	"metapus/pkg/logger"
)

const (
	_minInvoiceTTLMinutes = 5
	_maxInvoiceTTLMinutes = 10080 // 7 days
)

// MerchantInvoiceHandler handles /merchant/v1/invoices endpoints.
// All methods require MerchantAPIKey middleware to have run first.
type MerchantInvoiceHandler struct {
	invoiceService         domain.DocumentService[*crypto_invoice.CryptoInvoice]
	merchantInvoiceService *cryptoapp.MerchantInvoiceService
	checkoutRefs           *checkout.Service
	webhookRepo            crypto.MerchantWebhookRepository
	queries                *cryptoapp.MerchantCryptoQueryService
}

// NewMerchantInvoiceHandler creates the handler.
func NewMerchantInvoiceHandler(
	invoiceService domain.DocumentService[*crypto_invoice.CryptoInvoice],
	merchantInvoiceService *cryptoapp.MerchantInvoiceService,
	checkoutRefs *checkout.Service,
	webhookRepo crypto.MerchantWebhookRepository,
	queries *cryptoapp.MerchantCryptoQueryService,
) *MerchantInvoiceHandler {
	return &MerchantInvoiceHandler{
		invoiceService:         invoiceService,
		merchantInvoiceService: merchantInvoiceService,
		checkoutRefs:           checkoutRefs,
		webhookRepo:            webhookRepo,
		queries:                queries,
	}
}

// CancelInvoice handles POST /merchant/v1/invoices/:id/cancel. Cancellation is
// idempotent, serialized with chain processing by the invoice row lock, and
// emits invoice.cancelled in the same transaction as wallet release.
func (h *MerchantInvoiceHandler) CancelInvoice(c *gin.Context) {
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
	invoiceID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid invoice id"))
		c.Abort()
		return
	}
	err = h.merchantInvoiceService.CancelMerchantInvoice(c.Request.Context(), mc.MerchantID, invoiceID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.Status(http.StatusNoContent)
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
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	ctx := c.Request.Context()
	// 1. Resolve token and snapshot the platform-controlled finality policy.
	tokenContext, err := h.queries.ResolveToken(ctx, req.Currency)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	// 2. Validate and build amount (minor units)
	if !req.Amount.IsPositive() {
		_ = c.Error(apperror.NewValidation("amount must be a positive integer (minor units)").
			WithDetail("field", "amount"))
		c.Abort()
		return
	}
	amount := req.Amount

	// TTL
	ttl := 0
	if req.TTLMinutes != nil {
		ttl = *req.TTLMinutes
		if ttl < _minInvoiceTTLMinutes || ttl > _maxInvoiceTTLMinutes {
			_ = c.Error(apperror.NewValidation(
				fmt.Sprintf("ttlMinutes must be between %d and %d", _minInvoiceTTLMinutes, _maxInvoiceTTLMinutes)).
				WithDetail("field", "ttlMinutes"))
			c.Abort()
			return
		}
	}

	var orderID string
	if req.OrderID != nil {
		orderID = *req.OrderID
	}
	var externalID string
	if req.ExternalID != nil {
		externalID = *req.ExternalID
	}
	var description string
	if req.Description != nil {
		description = *req.Description
	}
	var notificationURL string
	if req.NotificationURL != nil {
		// CWE-918: validate before storing to prevent SSRF at webhook delivery time.
		if err := ValidateNotificationURL(*req.NotificationURL); err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		notificationURL = *req.NotificationURL
		if h.webhookRepo == nil {
			_ = c.Error(apperror.NewValidation("notificationUrl requires a configured default webhook endpoint"))
			c.Abort()
			return
		}
		hasDefault, err := h.webhookRepo.HasDefaultEndpoint(ctx, mc.MerchantID)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		if !hasDefault {
			_ = c.Error(apperror.NewValidation("notificationUrl requires an active default webhook endpoint").WithDetail("field", "notificationUrl"))
			c.Abort()
			return
		}
	}
	var returnURL string
	if req.ReturnURL != nil {
		if err := ValidateReturnURL(*req.ReturnURL); err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		returnURL = *req.ReturnURL
	}
	var customerID string
	if req.CustomerID != nil {
		customerID = *req.CustomerID
	}

	result, err := h.merchantInvoiceService.CreateMerchantInvoice(ctx, cryptoapp.CreateMerchantInvoiceInput{
		MerchantID:            mc.MerchantID,
		TokenID:               tokenContext.TokenID,
		Amount:                amount,
		APIKeyID:              &mc.KeyID,
		CreationSource:        crypto_invoice.CreationSourceMerchantAPI,
		TTLMinutes:            ttl,
		OrderID:               orderID,
		ExternalID:            externalID,
		Description:           description,
		NotificationURL:       notificationURL,
		ReturnURL:             returnURL,
		Metadata:              req.Metadata,
		RequiredConfirmations: tokenContext.RequiredConfirmations,
		FinalityMode:          tokenContext.FinalityMode,
		CustomerID:            customerID,
	})
	if err != nil {
		if appErr, ok := apperror.AsAppError(err); ok && appErr.Code == apperror.CodeWalletPoolExhausted {
			err = appErr.WithDetail("currency", req.Currency)
		}
		_ = c.Error(err)
		c.Abort()
		return
	}

	// 3. Reload to get wallet address (assigned during creation hook)
	display, err := h.queries.InvoiceDisplay(ctx, result.Invoice.ID)
	if err != nil {
		// Non-fatal: respond with empty address, the payment page will have it
		logger.Warn(ctx, "merchant api: failed to resolve invoice display",
			"invoice_id", result.Invoice.ID, "error", err)
		display.WalletAddress = ""
		display.TokenCode = req.Currency
		display.NetworkCode = tokenContext.NetworkCode
		display.DecimalPlaces = tokenContext.DecimalPlaces
	}

	status := http.StatusCreated
	if !result.Created {
		status = http.StatusOK
	}
	resp := dto.MerchantInvoiceFromEntity(result.Invoice, display.WalletAddress, display.TokenCode, display.NetworkCode, display.DecimalPlaces, h.invoicePayURL(result.Invoice.PublicRef))
	if !result.Created {
		if summary, summaryErr := h.queries.InvoicePaymentsSummary(ctx, result.Invoice.ID); summaryErr == nil {
			resp.FeeAmount = summary.FeeAmount
			resp.NetAmount = summary.NetAmount
			resp.Payments = merchantPaymentResponses(summary.Payments)
			resp.ChainObservedAt = summary.ObservedAt
		}
	} else {
		resp.ChainObservedAt = h.queries.InvoiceObservedAt(ctx, result.Invoice.ID)
	}
	c.JSON(status, resp)
}

// ListCurrencies handles GET /merchant/v1/currencies.
// Returns active tokens with their code, network, and decimalPlaces so
// merchant integrations can convert amounts to minor units without hardcoding.
func (h *MerchantInvoiceHandler) ListCurrencies(c *gin.Context) {
	mc := mw.GetMerchant(c.Request.Context())
	if mc == nil {
		_ = c.Error(apperror.NewUnauthorized("merchant context missing"))
		c.Abort()
		return
	}

	currencies, err := h.queries.ListCurrencies(c.Request.Context())
	if err != nil {
		_ = c.Error(apperror.NewInternal(fmt.Errorf("list merchant currencies: %w", err)))
		c.Abort()
		return
	}

	items := make([]dto.MerchantCurrencyItem, 0, len(currencies))
	for _, cur := range currencies {
		items = append(items, dto.MerchantCurrencyItem{
			Code:                  cur.Code,
			Symbol:                cur.Symbol,
			Name:                  cur.Name,
			Network:               cur.Network,
			NetworkName:           cur.NetworkName,
			Standard:              cur.Standard,
			DecimalPlaces:         cur.DecimalPlaces,
			ContractAddress:       cur.ContractAddress,
			RequiredConfirmations: cur.RequiredConfirmations,
			FinalityMode:          cur.FinalityMode,
			AcceptsPayments:       cur.AcceptsPayments,
		})
	}
	c.JSON(http.StatusOK, dto.MerchantCurrencyListResponse{Items: items})
}

// ListInvoices handles GET /merchant/v1/invoices.
// Supports cursor pagination and reconciliation filters (createdFrom/To, completedFrom/To, status, currency, orderId, externalId, customerId).
func (h *MerchantInvoiceHandler) ListInvoices(c *gin.Context) {
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

	filter, err := parseMerchantInvoiceListFilter(c, mc.MerchantID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	page, err := h.queries.ListInvoices(c.Request.Context(), filter)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	items := make([]dto.MerchantInvoiceResponse, 0, len(page.Items))
	for _, record := range page.Items {
		item := dto.MerchantInvoiceFromEntity(
			record.Invoice,
			record.WalletAddress,
			record.TokenCode,
			record.NetworkCode,
			record.DecimalPlaces,
			h.invoicePayURL(record.Invoice.PublicRef),
		)
		item.FeeAmount = record.Payments.FeeAmount
		item.NetAmount = record.Payments.NetAmount
		item.ChainObservedAt = record.Payments.ObservedAt
		item.Payments = merchantPaymentResponses(record.Payments.Payments)
		items = append(items, item)
	}

	c.JSON(http.StatusOK, dto.MerchantInvoiceListResponse{
		Object:     "list",
		Items:      items,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	})
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

	h.writeMerchantInvoiceResponse(c, inv, http.StatusOK)
}

func (h *MerchantInvoiceHandler) writeMerchantInvoiceResponse(c *gin.Context, inv *crypto_invoice.CryptoInvoice, status int) {
	ctx := c.Request.Context()
	display, err := h.queries.InvoiceDisplay(ctx, inv.ID)
	if err != nil {
		display = cryptoapp.MerchantInvoiceDisplay{}
	}

	resp := dto.MerchantInvoiceFromEntity(inv, display.WalletAddress, display.TokenCode, display.NetworkCode, display.DecimalPlaces, h.invoicePayURL(inv.PublicRef))
	summary, err := h.queries.InvoicePaymentsSummary(ctx, inv.ID)
	if err != nil {
		_ = c.Error(apperror.NewInternal(fmt.Errorf("load invoice payments: %w", err)))
		c.Abort()
		return
	}
	resp.FeeAmount = summary.FeeAmount
	resp.NetAmount = summary.NetAmount
	resp.Payments = merchantPaymentResponses(summary.Payments)
	resp.ChainObservedAt = summary.ObservedAt
	c.JSON(status, resp)
}

func (h *MerchantInvoiceHandler) invoicePayURL(publicRef string) string {
	if h.checkoutRefs == nil || publicRef == "" {
		return ""
	}
	return h.checkoutRefs.InvoicePayURL(publicRef)
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
		_ = c.Error(apperror.NewInvalidRequest(err))
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

	if err := key.ValidateAt(c.Request.Context(), time.Now().UTC()); err != nil {
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

	c.JSON(http.StatusOK, dto.MerchantAPIKeyListResponse{Items: items, Total: len(items)})
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

func merchantPaymentResponses(items []cryptoapp.MerchantInvoicePayment) []dto.MerchantPaymentResponse {
	result := make([]dto.MerchantPaymentResponse, 0, len(items))
	for _, source := range items {
		item := dto.MerchantPaymentResponse{
			ID: source.ID.String(), TxHash: source.TxHash, LogIndex: source.EventIndex, EventKind: source.EventKind,
			Amount: source.Amount, FeeAmount: source.FeeAmount, NetAmount: source.NetAmount,
			Status: source.Status, Canonicality: source.Canonicality, FinalityStatus: source.FinalityStatus,
			Confirmations: source.Confirmations, RequiredConfirmations: source.RequiredConfirmations,
			SourceNonce: source.SourceNonce, SourceAddress: source.SourceAddress,
			DetectedAt: source.DetectedAt.UTC().Format(time.RFC3339Nano),
		}
		if source.ReplacementTxHash != "" {
			item.ReplacementTxHash = &source.ReplacementTxHash
		}
		if source.ConfirmedAt != nil {
			formatted := source.ConfirmedAt.UTC().Format(time.RFC3339Nano)
			item.ConfirmedAt = &formatted
		}
		result = append(result, item)
	}
	return result
}

func parseMerchantInvoiceListFilter(c *gin.Context, merchantID id.ID) (cryptoapp.MerchantInvoiceListFilter, error) {
	filter := cryptoapp.MerchantInvoiceListFilter{
		MerchantID: merchantID,
		Currency:   strings.TrimSpace(c.Query("currency")),
		OrderID:    strings.TrimSpace(c.Query("orderId")),
		ExternalID: strings.TrimSpace(c.Query("externalId")),
		CustomerID: strings.TrimSpace(c.Query("customerId")),
		Cursor:     strings.TrimSpace(c.Query("cursor")),
	}

	if rawStatuses := strings.TrimSpace(c.Query("status")); rawStatuses != "" {
		parts := strings.Split(rawStatuses, ",")
		statuses := make([]string, 0, len(parts))
		for _, part := range parts {
			st := crypto_invoice.InvoiceStatus(strings.TrimSpace(part))
			if st == "" {
				continue
			}
			if !st.IsValid() {
				return cryptoapp.MerchantInvoiceListFilter{}, apperror.NewValidation(fmt.Sprintf("invalid status filter: %q", st)).WithDetail("field", "status")
			}
			statuses = append(statuses, string(st))
		}
		filter.Statuses = statuses
	}

	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit < 1 || limit > 100 {
			return cryptoapp.MerchantInvoiceListFilter{}, apperror.NewValidation("limit must be an integer between 1 and 100").WithDetail("field", "limit")
		}
		filter.Limit = limit
	}

	var err error
	if filter.CreatedFrom, err = parseOptionalRFC3339Query(c, "createdFrom"); err != nil {
		return cryptoapp.MerchantInvoiceListFilter{}, err
	}
	if filter.CreatedTo, err = parseOptionalRFC3339Query(c, "createdTo"); err != nil {
		return cryptoapp.MerchantInvoiceListFilter{}, err
	}
	if filter.CreatedFrom != nil && filter.CreatedTo != nil && filter.CreatedFrom.After(*filter.CreatedTo) {
		return cryptoapp.MerchantInvoiceListFilter{}, apperror.NewValidation("createdFrom must be before or equal to createdTo").WithDetail("field", "createdFrom")
	}
	if filter.CompletedFrom, err = parseOptionalRFC3339Query(c, "completedFrom"); err != nil {
		return cryptoapp.MerchantInvoiceListFilter{}, err
	}
	if filter.CompletedTo, err = parseOptionalRFC3339Query(c, "completedTo"); err != nil {
		return cryptoapp.MerchantInvoiceListFilter{}, err
	}
	if filter.CompletedFrom != nil && filter.CompletedTo != nil && filter.CompletedFrom.After(*filter.CompletedTo) {
		return cryptoapp.MerchantInvoiceListFilter{}, apperror.NewValidation("completedFrom must be before or equal to completedTo").WithDetail("field", "completedFrom")
	}

	return filter, nil
}

func parseOptionalRFC3339Query(c *gin.Context, name string) (*time.Time, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t, err = time.Parse(time.RFC3339, raw)
	}
	if err != nil {
		return nil, apperror.NewValidation(fmt.Sprintf("invalid %s: must be RFC3339 timestamp", name)).WithDetail("field", name)
	}
	utc := t.UTC()
	return &utc, nil
}
