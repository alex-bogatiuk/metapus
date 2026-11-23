package cryptohandlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/handlers"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/middleware"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

type WalletPoolHandler struct {
	*handlers.BaseHandler
	svc *crypto.WalletPoolAdminService
}

func NewWalletPoolHandler(
	base *handlers.BaseHandler,
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
	c.JSON(http.StatusOK, cryptodto.NewMerchantWalletAllocationLimitsResponse(limits))
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
	var request cryptodto.UpdateMerchantWalletAllocationLimitsRequest
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
	response := cryptodto.NewMerchantWalletAllocationLimitsResponse(limits)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

func (h *WalletPoolHandler) List(c *gin.Context) {
	states, err := h.svc.List(c.Request.Context())
	if err != nil {
		h.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, cryptodto.NewWalletPoolListResponse(states))
}

func (h *WalletPoolHandler) UpdatePolicy(c *gin.Context) {
	networkID, ok := h.parseNetworkID(c)
	if !ok {
		return
	}
	var request cryptodto.UpdateWalletPoolPolicyRequest
	if !h.BindJSON(c, &request) {
		return
	}

	updated, err := h.svc.UpdatePolicy(c.Request.Context(), networkID, request.ToDomain())
	if err != nil {
		h.Error(c, err)
		return
	}
	response := cryptodto.NewWalletPoolStateResponse(updated)
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
	response := cryptodto.NewWalletPoolReconcileResponse(results, state)
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
