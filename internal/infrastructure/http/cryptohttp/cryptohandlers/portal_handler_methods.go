package cryptohandlers

import (
	"errors"
	"metapus/internal/domain/crypto/aml"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/infrastructure/storage/postgres/portal_repo"
)

// Balance (detailed, three-bucket).

// GetBalanceDetailed handles GET /portal/v1/balances?merchant_id=...&source=coingecko
// Returns merchant balance split into total / pending / available buckets.
func (h *PortalHandler) GetBalanceDetailed(c *gin.Context) { h.serveValuedBalance(c) }

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

	var req cryptodto.PortalCreateInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
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
	amount, err := types.NewRawAmountFromBigInt(minorDec.BigInt())
	if err != nil {
		_ = c.Error(apperror.NewValidation("amount must be a positive integer"))
		c.Abort()
		return
	}

	// An omitted TTL is resolved from merchant settings by the application service.
	ttl := 0
	if req.TTLMinutes != nil {
		ttl = *req.TTLMinutes
		if ttl < 5 || ttl > 10080 {
			_ = c.Error(apperror.NewValidation("ttlMinutes must be between 5 and 10080"))
			c.Abort()
			return
		}
	}
	var description string
	if req.Description != nil {
		description = *req.Description
	}
	var orderID string
	if req.OrderID != nil {
		orderID = *req.OrderID
	}
	var customerID string
	if req.CustomerID != nil {
		customerID = *req.CustomerID
	}

	uc := appctx.GetUser(ctx)
	if uc == nil || uc.UserID == "" {
		_ = c.Error(apperror.NewUnauthorized("user context missing"))
		c.Abort()
		return
	}
	createdByID, err := id.Parse(uc.UserID)
	if err != nil {
		_ = c.Error(apperror.NewUnauthorized("invalid user context"))
		c.Abort()
		return
	}

	result, err := h.merchantInvoiceService.CreateMerchantInvoice(ctx, cryptoapp.CreateMerchantInvoiceInput{
		MerchantID:     mid,
		TokenID:        tokenID,
		Amount:         amount,
		CreatedByID:    createdByID,
		CreationSource: crypto_invoice.CreationSourcePortal,
		TTLMinutes:     ttl,
		OrderID:        orderID,
		Description:    description,
		CustomerID:     customerID,
	})
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	inv := result.Invoice

	// Reload to get wallet address assigned during hook
	display, err := h.merchantCryptoQueries.InvoiceDisplay(ctx, inv.ID)
	if err != nil {
		display.WalletAddress = ""
		display.NetworkCode = tokenMeta.Network
	}

	payURL := ""
	if h.checkoutRefs != nil {
		payURL = h.checkoutRefs.InvoicePayURL(inv.PublicRef)
	}
	status := http.StatusCreated
	if !result.Created {
		status = http.StatusOK
	}
	c.JSON(status, cryptodto.PortalCreateInvoiceResponse{
		Object:        "crypto_invoice",
		ID:            inv.GetID().String(),
		Version:       inv.Version,
		CheckoutRef:   inv.PublicRef,
		PayURL:        payURL,
		Number:        inv.Number,
		Status:        string(inv.Status),
		Amount:        inv.ExpectedAmount.String(),
		Symbol:        tokenMeta.Symbol,
		Network:       display.NetworkCode,
		DecimalPlaces: decimalPlaces,
		WalletAddress: display.WalletAddress,
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
	detail.PayURL = h.checkoutRefs.InvoicePayURL(detail.CheckoutRef)
	c.JSON(http.StatusOK, detail)
}

// GetRefundablePayment handles GET /portal/v1/payments/:id/refund?merchant_id=...
// It reports what is left to refund of a credited payment of the merchant.
func (h *PortalHandler) GetRefundablePayment(c *gin.Context) {
	paymentID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid payment id"))
		c.Abort()
		return
	}
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	payment, err := h.withdrawalService.RefundablePayment(c.Request.Context(), mid, paymentID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, cryptodto.PortalRefundablePaymentResponse{
		PaymentID: payment.PaymentID.String(), InvoiceID: payment.InvoiceID.String(), InvoiceNumber: payment.InvoiceNumber,
		TokenID: payment.TokenID.String(), FromAddress: payment.FromAddress, Amount: payment.Amount.String(),
		CommittedAmount: payment.Committed.String(), RefundableAmount: payment.Refundable().String(),
	})
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
	c.JSON(http.StatusOK, cryptodto.PortalWithdrawalListResponse{
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
	c.JSON(http.StatusOK, cryptodto.PortalWithdrawalAddressListResponse{Items: items})
}

// AddWhitelistedAddress handles POST /portal/v1/withdrawal-addresses?merchant_id=...
func (h *PortalHandler) AddWhitelistedAddress(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	var req cryptodto.PortalAddWithdrawalAddressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	networkID, err := id.Parse(req.NetworkID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid networkId"))
		c.Abort()
		return
	}
	newID, err := h.withdrawalService.AddAddress(c.Request.Context(), mid, networkID, req.Address, req.Label)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusCreated, cryptodto.PortalWithdrawalAddressCreatedResponse{Object: "withdrawal_address", ID: newID.String(), Version: 1})
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

	if err := h.withdrawalService.RemoveAddress(c.Request.Context(), mid, addressID); err != nil {
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
	c.JSON(http.StatusOK, cryptodto.PortalWithdrawalRequestListResponse{
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

	var req cryptodto.PortalCreateWithdrawalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
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

	var refundPaymentID *id.ID
	if req.RefundPaymentID != "" {
		paymentID, err := id.Parse(req.RefundPaymentID)
		if err != nil {
			_ = c.Error(apperror.NewValidation("invalid refundPaymentId"))
			c.Abort()
			return
		}
		refundPaymentID = &paymentID
	}

	result, err := h.withdrawalService.Create(c.Request.Context(), cryptoapp.CreatePortalWithdrawalInput{
		MerchantID: mid,
		TokenID:    tokenID,
		AddressID:  addressID,
		Amount:     req.Amount,
		RiskClass:  aml.Class(req.RiskClass), RiskAccepted: req.RiskAccepted,
		RefundPaymentID: refundPaymentID, RefundReason: req.RefundReason,
	})
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusAccepted, cryptodto.PortalWithdrawalCreatedResponse{Object: "withdrawal_request", ID: result.ID.String(), Version: 1, Number: result.Number, Status: string(result.Status)})
}

// RejectWithdrawalRequest handles POST /portal/v1/withdrawal-requests/:id/reject?merchant_id=...
// Creates storno (compensating) movements to restore merchant balance.
func (h *PortalHandler) RejectWithdrawalRequest(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
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
	var body cryptodto.ReasonRequest
	_ = c.ShouldBindJSON(&body) // ignore error; reason is optional

	if err := h.withdrawalService.Reject(c.Request.Context(), mid, requestID, body.Reason); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	status, err := h.repo.WithdrawalRequestStatus(c.Request.Context(), mid, requestID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, cryptodto.StatusResponse{Status: status})
}

// Webhooks.

// Settings: fee schedule.

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
	c.JSON(http.StatusOK, cryptodto.PortalFeeScheduleResponse{Items: items})
}

// Persistent customer deposit addresses.

// ListDepositAddresses handles GET /portal/v1/deposit-addresses?merchant_id=...
func (h *PortalHandler) ListDepositAddresses(c *gin.Context) {
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

	filter := portal_repo.DepositAddressFilter{
		Search:    c.Query("search"),
		NetworkID: c.Query("network_id"),
	}

	items, total, quotas, err := h.repo.ListDepositAddresses(c.Request.Context(), mid, filter, limit, offset)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, cryptodto.PortalDepositAddressListResponse{
		Items:  items,
		Total:  total,
		Quotas: quotas,
	})
}

// CreateDepositAddress handles POST /portal/v1/deposit-addresses?merchant_id=...
// Assigns or returns an existing persistent wallet for (merchant_id, network_id, customerId).
func (h *PortalHandler) CreateDepositAddress(c *gin.Context) {
	mid, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	var req cryptodto.PortalCreateDepositAddressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	networkID, err := id.Parse(req.NetworkID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid networkId").WithDetail("field", "networkId"))
		c.Abort()
		return
	}

	customerID := strings.TrimSpace(req.CustomerID)
	if customerID == "" || len(customerID) > 255 {
		_ = c.Error(apperror.NewValidation("customerId must be between 1 and 255 characters").WithDetail("field", "customerId"))
		c.Abort()
		return
	}

	if h.walletService == nil {
		_ = c.Error(apperror.NewInternal(errors.New("persistent wallet service is not configured")))
		c.Abort()
		return
	}

	ctx := c.Request.Context()
	networkCode, networkName, tokens, err := h.repo.GetDepositNetworkMeta(ctx, mid, networkID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	existing, existingErr := h.walletService.FindPersistentByCustomerID(ctx, mid, networkID, customerID)
	if existingErr != nil {
		if appErr, ok := apperror.AsAppError(existingErr); !ok || appErr.Code != apperror.CodeNotFound {
			_ = c.Error(existingErr)
			c.Abort()
			return
		}
	}

	w, err := h.walletService.AssignPersistentAddress(ctx, mid, networkID, customerID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	reused := existingErr == nil && existing != nil && existing.ID == w.ID
	status := http.StatusCreated
	if reused {
		status = http.StatusOK
	}

	c.JSON(status, cryptodto.PortalCreateDepositAddressResponse{
		Object:      "deposit_address",
		ID:          w.ID.String(),
		CustomerID:  w.CustomerID,
		Address:     w.Address,
		NetworkID:   networkID.String(),
		NetworkCode: networkCode,
		NetworkName: networkName,
		Tokens:      tokens,
		Reused:      reused,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		QRCode:      generateQRDataURL(w.Address),
	})
}
