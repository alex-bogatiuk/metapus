package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/middleware"
)

type WalletPoolHandler struct {
	*BaseHandler
	svc *crypto.WalletPoolAdminService
}

func NewWalletPoolHandler(
	base *BaseHandler,
	svc *crypto.WalletPoolAdminService,
) *WalletPoolHandler {
	return &WalletPoolHandler{BaseHandler: base, svc: svc}
}

func (h *WalletPoolHandler) RegisterRoutes(rg *gin.RouterGroup, updateGuard gin.HandlerFunc) {
	group := rg.Group("/crypto/wallet-pools")
	{
		group.GET("", middleware.RequirePermission("crypto_wallet_pool:read"), h.List)
		group.PATCH(
			"/:networkId/policy",
			middleware.RequirePermission("crypto_wallet_pool:update"),
			updateGuard,
			h.UpdatePolicy,
		)
		group.POST(
			"/:networkId/reconcile",
			middleware.RequirePermission("crypto_wallet_pool:update"),
			h.Reconcile,
		)
		group.GET(
			"/:networkId/merchant-overrides/:merchantId",
			middleware.RequirePermission("crypto_wallet_pool:read"),
			h.GetMerchantAllocationLimits,
		)
		group.PUT(
			"/:networkId/merchant-overrides/:merchantId",
			middleware.RequirePermission("crypto_wallet_pool:update"),
			updateGuard,
			h.UpdateMerchantAllocationLimits,
		)
	}
}

func (h *WalletPoolHandler) GetMerchantAllocationLimits(c *gin.Context) {
	networkID, ok := h.parseNetworkID(c)
	if !ok {
		return
	}
	merchantID, ok := h.parseMerchantID(c)
	if !ok {
		return
	}
	limits, err := h.svc.GetMerchantAllocationLimits(c.Request.Context(), merchantID, networkID)
	if err != nil {
		h.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.NewMerchantWalletAllocationLimitsResponse(limits))
}

func (h *WalletPoolHandler) UpdateMerchantAllocationLimits(c *gin.Context) {
	networkID, ok := h.parseNetworkID(c)
	if !ok {
		return
	}
	merchantID, ok := h.parseMerchantID(c)
	if !ok {
		return
	}
	var request dto.UpdateMerchantWalletAllocationLimitsRequest
	if !h.BindJSON(c, &request) {
		return
	}
	limits, err := h.svc.UpdateMerchantAllocationLimits(
		c.Request.Context(),
		merchantID,
		networkID,
		request.ToDomain(),
	)
	if err != nil {
		h.Error(c, err)
		return
	}
	response := dto.NewMerchantWalletAllocationLimitsResponse(limits)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

func (h *WalletPoolHandler) List(c *gin.Context) {
	states, err := h.svc.List(c.Request.Context())
	if err != nil {
		h.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.NewWalletPoolListResponse(states))
}

func (h *WalletPoolHandler) UpdatePolicy(c *gin.Context) {
	networkID, ok := h.parseNetworkID(c)
	if !ok {
		return
	}
	var request dto.UpdateWalletPoolPolicyRequest
	if !h.BindJSON(c, &request) {
		return
	}

	updated, err := h.svc.UpdatePolicy(c.Request.Context(), networkID, request.ToDomain())
	if err != nil {
		h.Error(c, err)
		return
	}
	response := dto.NewWalletPoolStateResponse(updated)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

func (h *WalletPoolHandler) Reconcile(c *gin.Context) {
	networkID, ok := h.parseNetworkID(c)
	if !ok {
		return
	}
	results, state, err := h.svc.Reconcile(c.Request.Context(), networkID)
	if err != nil {
		h.Error(c, err)
		return
	}
	response := dto.NewWalletPoolReconcileResponse(results, state)
	h.CompleteIdempotency(c, http.StatusAccepted, "application/json", response)
	c.JSON(http.StatusAccepted, response)
}

func (h *WalletPoolHandler) parseNetworkID(c *gin.Context) (id.ID, bool) {
	networkID, err := id.Parse(c.Param("networkId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid networkId").
			WithDetail("networkId", c.Param("networkId")))
		return id.Nil(), false
	}
	return networkID, true
}

func (h *WalletPoolHandler) parseMerchantID(c *gin.Context) (id.ID, bool) {
	merchantID, err := id.Parse(c.Param("merchantId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid merchantId").
			WithDetail("merchantId", c.Param("merchantId")))
		return id.Nil(), false
	}
	return merchantID, true
}
