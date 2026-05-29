package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/documents/withdrawal_request"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres/portal_repo"
)

// Balance (detailed, three-bucket).

// GetBalanceDetailed handles GET /portal/v1/balances?merchant_id=...&source=coingecko
// Returns merchant balance split into total / pending / available buckets.
func (h *PortalHandler) GetBalanceDetailed(c *gin.Context) {
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if h.balanceCalc == nil {
		c.JSON(http.StatusOK, dto.PortalDetailedBalanceResponse{
			ByToken: []dto.PortalTokenDetailed{},
		})
		return
	}

	ctx := c.Request.Context()
	sourceCode := c.DefaultQuery("source", "coingecko")

	rateSourceID, err := h.rateSourceResolver.ResolveRateSourceID(ctx, sourceCode)
	if err != nil {
		_ = c.Error(apperror.NewValidation("unknown rate source: " + sourceCode))
		c.Abort()
		return
	}

	ids := h.repo.ScopeIDs(ctx, mid)

	balance, err := h.balanceCalc.CalculateForMerchants(ctx, h.repo, ids, rateSourceID, nil)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	// Pending amounts: invoices in 'paid' status (tx detected, not yet confirmed).
	pendingMap, err := h.repo.GetPendingAmounts(ctx, ids)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	totalBase := balance.TotalBase
	pendingBase := decimal.Zero
	availableBase := decimal.Zero

	byToken := make([]dto.PortalTokenDetailed, 0, len(balance.ByToken))
	for _, tb := range balance.ByToken {
		tokenIDStr := tb.TokenID.String()

		rawInt, err := strconv.ParseInt(tb.RawAmount, 10, 64)
		if err != nil {
			_ = c.Error(apperror.NewInternal(err))
			c.Abort()
			return
		}
		pendingRaw := pendingMap[tokenIDStr]
		availableRaw := rawInt - pendingRaw
		if availableRaw < 0 {
			availableRaw = 0
		}

		// Fiat valuation of pending/available using the same rate.
		pendingHuman := decimal.NewFromInt(pendingRaw).Shift(-int32(tb.DecimalPlaces))
		availableHuman := decimal.NewFromInt(availableRaw).Shift(-int32(tb.DecimalPlaces))

		if tb.HasRate {
			rateMultiplier := decimal.NewFromInt(int64(tb.Multiplier))
			tokenPendingBase := pendingHuman.Mul(tb.Rate).Div(rateMultiplier)
			tokenAvailableBase := availableHuman.Mul(tb.Rate).Div(rateMultiplier)
			pendingBase = pendingBase.Add(tokenPendingBase)
			availableBase = availableBase.Add(tokenAvailableBase)
		}

		byToken = append(byToken, dto.PortalTokenDetailed{
			PortalTokenBalance: dto.PortalTokenBalance{
				TokenID:      tokenIDStr,
				TokenSymbol:  tb.TokenSymbol,
				CurrencyCode: tb.CurrencyCode,
				RawAmount:    tb.RawAmount,
				HumanAmount:  tb.HumanAmount.StringFixed(8),
				Rate:         tb.Rate.StringFixed(12),
				Multiplier:   tb.Multiplier,
				BaseAmount:   tb.BaseAmount.StringFixed(2),
				HasRate:      tb.HasRate,
			},
			PendingRaw:     strconv.FormatInt(pendingRaw, 10),
			PendingHuman:   portal_repo.FormatMinorUnits(pendingRaw, tb.DecimalPlaces),
			AvailableRaw:   strconv.FormatInt(availableRaw, 10),
			AvailableHuman: portal_repo.FormatMinorUnits(availableRaw, tb.DecimalPlaces),
		})
	}

	c.JSON(http.StatusOK, dto.PortalDetailedBalanceResponse{
		TotalBase:     totalBase.StringFixed(2),
		PendingBase:   pendingBase.StringFixed(2),
		AvailableBase: availableBase.StringFixed(2),
		BaseCurrency:  balance.BaseCurrency,
		RateSource:    sourceCode,
		ByToken:       byToken,
	})
}

// Invoice.

// CreateInvoice handles POST /portal/v1/invoices?merchant_id=...
// Accepts human-readable amount (e.g. "10.5"), converts to minor units via decimal_places.
func (h *PortalHandler) CreateInvoice(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	var req dto.PortalCreateInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation(err.Error()))
		c.Abort()
		return
	}

	tokenID, err := id.Parse(req.TokenID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid tokenId"))
		c.Abort()
		return
	}

	ctx := c.Request.Context()

	// Convert human-readable amount to minor units using token decimal_places.
	tokenMeta, err := h.repo.GetTokenMeta(ctx, tokenID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("unknown token: " + req.TokenID))
		c.Abort()
		return
	}
	decimalPlaces := tokenMeta.DecimalPlaces

	humanDec, err := decimal.NewFromString(req.Amount)
	if err != nil || !humanDec.IsPositive() {
		_ = c.Error(apperror.NewValidation("amount must be a positive decimal string"))
		c.Abort()
		return
	}

	minorDec := humanDec.Shift(int32(decimalPlaces))
	if !minorDec.Equal(minorDec.Truncate(0)) {
		_ = c.Error(apperror.NewValidation("amount has too many decimal places for this token"))
		c.Abort()
		return
	}
	if minorDec.LessThanOrEqual(decimal.Zero) {
		_ = c.Error(apperror.NewValidation("amount must be positive"))
		c.Abort()
		return
	}
	if minorDec.GreaterThan(_maxInt64Dec) {
		_ = c.Error(apperror.NewValidation("amount exceeds maximum allowed value"))
		c.Abort()
		return
	}

	amount := types.NewCryptoAmountFromInt64(minorDec.IntPart())

	// Build domain entity.
	inv := crypto_invoice.NewCryptoInvoice(mid, tokenID, amount)
	inv.Status = crypto_invoice.InvoiceStatusCreated

	// TTL: default 60 min, range [5, 1440].
	ttl := 60
	if req.TTLMinutes != nil {
		ttl = *req.TTLMinutes
	}
	if ttl < 5 || ttl > 1440 {
		_ = c.Error(apperror.NewValidation("ttlMinutes must be between 5 and 1440"))
		c.Abort()
		return
	}
	inv.ExpiresAt = time.Now().Add(time.Duration(ttl) * time.Minute)

	// Optional fields.
	if req.Description != nil {
		inv.Description = *req.Description
	}
	if req.OrderID != nil {
		inv.OrderID = *req.OrderID
	}
	if req.CustomerEmail != nil {
		inv.CustomerEmail = *req.CustomerEmail
	}

	// Portal audit trail: capture user ID.
	if uc := appctx.GetUser(ctx); uc != nil && uc.UserID != "" {
		if uid, parseErr := id.Parse(uc.UserID); parseErr == nil {
			inv.APIKeyID = &uid
		}
	}

	// Create via service (triggers hooks: wallet lease, outbox, etc.)
	if err := h.invoiceService.Create(ctx, inv); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	// Reload to get wallet address assigned during hook
	pool := tenant.MustGetPool(ctx)
	walletAddress, _, network, err := fetchInvoiceDisplay(ctx, pool, inv.ID)
	if err != nil {
		walletAddress = ""
		network = tokenMeta.Network
	}

	c.JSON(http.StatusCreated, dto.PortalCreateInvoiceResponse{
		ID:            inv.GetID().String(),
		Number:        inv.Number,
		Status:        string(inv.Status),
		Amount:        strconv.FormatInt(minorDec.IntPart(), 10),
		Symbol:        tokenMeta.Symbol,
		Network:       network,
		DecimalPlaces: decimalPlaces,
		WalletAddress: walletAddress,
		ExpiresAt:     inv.ExpiresAt.Format(time.RFC3339),
		CreatedAt:     inv.CreatedAt.Format(time.RFC3339),
	})
}

// GetInvoiceDetail handles GET /portal/v1/invoices/:id
func (h *PortalHandler) GetInvoiceDetail(c *gin.Context) {
	invoiceID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid invoice id"))
		c.Abort()
		return
	}

	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	detail, err := h.repo.GetInvoiceDetail(c.Request.Context(), invoiceID, mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, detail)
}

// Withdrawals (read-only).

// ListWithdrawals handles GET /portal/v1/withdrawals?merchant_id=...&status=...&sort=...&order=...
func (h *PortalHandler) ListWithdrawals(c *gin.Context) {
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	filter := portal_repo.WithdrawalFilter{
		Status: c.Query("status"),
		Sort:   c.DefaultQuery("sort", "created_at"),
		Order:  c.DefaultQuery("order", "desc"),
	}

	// Validate sort column (prevent SQL injection).
	switch filter.Sort {
	case "created_at", "number", "status", "amount":
		// valid
	default:
		filter.Sort = "created_at"
	}
	switch filter.Order {
	case "asc", "desc":
		// valid
	default:
		filter.Order = "desc"
	}

	items, total, err := h.repo.ListWithdrawals(c.Request.Context(), mid, filter, limit, offset)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, dto.PortalWithdrawalListResponse{
		Items: items,
		Total: total,
	})
}

// Withdrawal address whitelist.

// ListWhitelistedAddresses handles GET /portal/v1/withdrawal-addresses?merchant_id=...
func (h *PortalHandler) ListWhitelistedAddresses(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	items, err := h.repo.ListWhitelistedAddresses(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, dto.PortalWithdrawalAddressListResponse{Items: items})
}

// AddWhitelistedAddress handles POST /portal/v1/withdrawal-addresses?merchant_id=...
func (h *PortalHandler) AddWhitelistedAddress(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	var req dto.PortalAddWithdrawalAddressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation(err.Error()))
		c.Abort()
		return
	}

	// Validate blockchain address format: alphanumeric only (CWE-79 prevention).
	if !_blockchainAddressRe.MatchString(req.Address) {
		_ = c.Error(apperror.NewValidation("address must contain only alphanumeric characters"))
		c.Abort()
		return
	}
	if len(req.Address) < 20 || len(req.Address) > 128 {
		_ = c.Error(apperror.NewValidation("address length must be between 20 and 128 characters"))
		c.Abort()
		return
	}

	newID, err := h.repo.AddWhitelistedAddress(c.Request.Context(), mid, req)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": newID})
}

// RemoveWhitelistedAddress handles DELETE /portal/v1/withdrawal-addresses/:id?merchant_id=...
func (h *PortalHandler) RemoveWhitelistedAddress(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	addressID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid address id"))
		c.Abort()
		return
	}

	if err := h.repo.RemoveWhitelistedAddress(c.Request.Context(), mid, addressID); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.Status(http.StatusNoContent)
}

// Withdrawal requests (debit-first pattern).

// ListWithdrawalRequests handles GET /portal/v1/withdrawal-requests?merchant_id=...
func (h *PortalHandler) ListWithdrawalRequests(c *gin.Context) {
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	ctx := c.Request.Context()
	ids := h.repo.ScopeIDs(ctx, mid)

	items, total, err := h.repo.ListWithdrawalRequests(ctx, ids)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, dto.PortalWithdrawalRequestListResponse{
		Items: items,
		Total: total,
	})
}

// CreateWithdrawalRequest handles POST /portal/v1/withdrawal-requests?merchant_id=...
// Implements debit-first pattern: Engine.Post() creates EXPENSE movement, reducing balance.
func (h *PortalHandler) CreateWithdrawalRequest(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	var req dto.PortalCreateWithdrawalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation(err.Error()))
		c.Abort()
		return
	}

	tokenID, err := id.Parse(req.TokenID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid tokenId"))
		c.Abort()
		return
	}

	addressID, err := id.Parse(req.AddressID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid addressId"))
		c.Abort()
		return
	}

	ctx := c.Request.Context()

	// Validate address ownership: must belong to the merchant's whitelist.
	addr, err := h.repo.GetWhitelistedAddress(ctx, mid, addressID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("address not found in whitelist"))
		c.Abort()
		return
	}

	// Convert human-readable amount to minor units.
	humanDec, err := decimal.NewFromString(req.Amount)
	if err != nil || !humanDec.IsPositive() {
		_ = c.Error(apperror.NewValidation("amount must be a positive decimal string"))
		c.Abort()
		return
	}

	tokenMeta, err := h.repo.GetTokenMeta(ctx, tokenID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("unknown token: " + req.TokenID))
		c.Abort()
		return
	}
	if addr.NetworkID != tokenMeta.NetworkID.String() {
		_ = c.Error(apperror.NewValidation("address is not whitelisted for this token's network"))
		c.Abort()
		return
	}
	decimalPlaces := tokenMeta.DecimalPlaces

	minorDec := humanDec.Shift(int32(decimalPlaces))
	if !minorDec.Equal(minorDec.Truncate(0)) {
		_ = c.Error(apperror.NewValidation("amount has too many decimal places for this token"))
		c.Abort()
		return
	}
	if minorDec.GreaterThan(_maxInt64Dec) {
		_ = c.Error(apperror.NewValidation("amount exceeds maximum allowed value"))
		c.Abort()
		return
	}

	amount := types.NewCryptoAmountFromInt64(minorDec.IntPart())

	// Build domain entity.
	wr := withdrawal_request.NewWithdrawalRequest(mid, tokenID, addressID, addr.Address, amount)

	// Validate (pure function, no DB).
	if err := wr.Validate(ctx); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	// Generate document number.
	number, err := h.numeratorService.GetNextNumber(
		ctx, numerator.DefaultConfig("WR"),
		nil, time.Now(),
	)
	if err != nil {
		_ = c.Error(apperror.NewInternal(err))
		c.Abort()
		return
	}
	wr.Number = number

	// Post via engine: debit-first pattern.
	// Engine.Post wraps everything in RunInTransaction:
	//   1. Advisory lock (DocumentLocker)
	//   2. Generate EXPENSE movement via WithdrawalRequest.GenerateCryptoMerchantBalanceMovements
	//   3. Validate balance sufficiency (CryptoMerchantBalanceRecorder.ValidateBeforePost)
	//   4. Record movements; DB trigger updates balance
	//   5. updateDoc callback inserts doc_withdrawal_requests
	if err := h.postingEngine.Post(ctx, wr, func(txCtx context.Context) error {
		return h.repo.CreateWithdrawalRequest(txCtx,
			wr.GetID(), mid, tokenID, minorDec.IntPart(),
			addr.Address, addressID, number,
			wr.IsPosted(), wr.GetPostedVersion(),
		)
	}); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":     wr.GetID().String(),
		"number": number,
		"status": "pending_approval",
	})
}

// RejectWithdrawalRequest handles POST /portal/v1/withdrawal-requests/:id/reject?merchant_id=...
// Creates storno (compensating) movements to restore merchant balance.
func (h *PortalHandler) RejectWithdrawalRequest(c *gin.Context) {
	mid, err := parseActiveMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	requestID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid withdrawal request id"))
		c.Abort()
		return
	}

	// Optional rejection reason.
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body) // ignore error; reason is optional

	ctx := c.Request.Context()
	ids := h.repo.ScopeIDs(ctx, mid)

	// Execute storno inside a transaction:
	//   1. Lock the request row (FOR UPDATE) to prevent concurrent rejection
	//   2. Validate status == pending_approval
	//   3. Create compensating RECEIPT movements (restore balance)
	//   4. Update status to 'rejected'
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		_ = c.Error(apperror.NewInternal(err))
		c.Abort()
		return
	}

	if err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		docID, postedVersion, status, err := h.repo.GetWithdrawalRequestForStorno(txCtx, ids, requestID)
		if err != nil {
			return apperror.NewNotFound("withdrawal_request", requestID.String())
		}

		if status != string(withdrawal_request.StatusPendingApproval) {
			return apperror.NewConflict("withdrawal request already processed (status: " + status + ")")
		}

		// Storno: create compensating RECEIPT movements to restore balance.
		if err := h.merchantBalanceSvc.StornoMovements(txCtx, docID, postedVersion+1); err != nil {
			return err
		}

		return h.repo.RejectWithdrawalRequest(txCtx, requestID, body.Reason)
	}); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "rejected"})
}

// Webhooks.

// ListWebhookDeliveries handles GET /portal/v1/webhooks/deliveries?merchant_id=...
func (h *PortalHandler) ListWebhookDeliveries(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	items, total, err := h.repo.GetWebhookDeliveriesByMerchant(c.Request.Context(), mid, limit, offset)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}

// SendTestWebhook handles POST /portal/v1/webhooks/test?merchant_id=...
func (h *PortalHandler) SendTestWebhook(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if h.webhookDispatcher == nil {
		_ = c.Error(apperror.NewInternal(nil).WithDetail("reason", "webhook dispatcher not configured"))
		c.Abort()
		return
	}

	ctx := c.Request.Context()

	webhookURL, webhookSecret, err := h.repo.GetMerchantWebhookSecret(ctx, mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	if webhookURL == "" {
		_ = c.Error(apperror.NewValidation("webhook URL not configured; update merchant settings first"))
		c.Abort()
		return
	}
	if webhookSecret == "" {
		_ = c.Error(apperror.NewValidation("webhook secret not configured; rotate secret first"))
		c.Abort()
		return
	}

	data := map[string]any{
		"test":       true,
		"merchantId": mid.String(),
	}

	delivery, dispatchErr := h.webhookDispatcher.Dispatch(
		ctx, h.deliveryRepo, nil, mid,
		webhookURL, webhookSecret,
		crypto.WebhookEventType("test"), data, 1,
	)

	resp := dto.PortalTestWebhookResponse{
		Success: dispatchErr == nil,
	}
	if delivery != nil {
		resp.StatusCode = delivery.StatusCode
		resp.ResponseTimeMs = delivery.ResponseTimeMs
	}
	if dispatchErr != nil {
		errMsg := dispatchErr.Error()
		resp.Error = &errMsg
	}

	c.JSON(http.StatusOK, resp)
}

// Settings: webhook secret + fee schedule.

// RevealWebhookSecret handles POST /portal/v1/settings/webhook-secret/reveal?merchant_id=...
func (h *PortalHandler) RevealWebhookSecret(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	_, secret, err := h.repo.GetMerchantWebhookSecret(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	if secret == "" {
		_ = c.Error(apperror.NewNotFound("webhook_secret", mid.String()))
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, dto.PortalWebhookSecretResponse{Secret: secret})
}

// RotateWebhookSecret handles POST /portal/v1/settings/webhook-secret/rotate?merchant_id=...
func (h *PortalHandler) RotateWebhookSecret(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	newSecret, err := h.repo.RotateWebhookSecret(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, dto.PortalWebhookSecretResponse{Secret: newSecret})
}

// GetFeeSchedule handles GET /portal/v1/settings/fees?merchant_id=...
func (h *PortalHandler) GetFeeSchedule(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	items, err := h.repo.GetEffectiveFees(c.Request.Context(), mid)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, dto.PortalFeeScheduleResponse{Items: items})
}
