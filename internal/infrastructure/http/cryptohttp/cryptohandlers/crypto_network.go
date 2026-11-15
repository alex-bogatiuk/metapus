package cryptohandlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
)

type CryptoNetworkHandler struct {
	*handlers.BaseHandler
	svc    *crypto.NetworkPolicyAdminService
	shadow *crypto.ShadowControlAdminService
}

func NewCryptoNetworkHandler(base *handlers.BaseHandler, svc *crypto.NetworkPolicyAdminService, shadow *crypto.ShadowControlAdminService) *CryptoNetworkHandler {
	return &CryptoNetworkHandler{BaseHandler: base, svc: svc, shadow: shadow}
}

func (h *CryptoNetworkHandler) RegisterRoutes(rg *gin.RouterGroup, updateGuard gin.HandlerFunc) {
	group := rg.Group("/admin/crypto/networks")
	group.GET("", middleware.RequirePermission("crypto_network:read"), h.List)
	group.GET("/:networkId/status", middleware.RequirePermission("crypto_network:read"), h.Status)
	group.PATCH("/:networkId/policy", middleware.RequirePermission("crypto_network:update"), updateGuard, h.UpdatePolicy)
	group.POST("/:networkId/watching", middleware.RequirePermission("crypto_network:update"), updateGuard, h.SetWatching)
	group.GET("/:networkId/shadow", middleware.RequirePermission("crypto_network:read"), h.ShadowStatus)
	group.POST("/:networkId/shadow", middleware.RequirePermission("crypto_network:update"), middleware.RequireSystemAdmin(), updateGuard, h.SetShadowPaused)
}

func (h *CryptoNetworkHandler) List(c *gin.Context) {
	states, err := h.svc.List(c.Request.Context())
	if err != nil {
		h.Error(c, err)
		return
	}
	items := make([]dto.CryptoNetworkStateResponse, 0, len(states))
	for _, state := range states {
		items = append(items, dto.CryptoNetworkStateFromDomain(state))
	}
	c.JSON(http.StatusOK, dto.CryptoNetworkListResponse{Items: items})
}

func (h *CryptoNetworkHandler) Status(c *gin.Context) {
	networkID, ok := h.networkID(c)
	if !ok {
		return
	}
	state, err := h.svc.Get(c.Request.Context(), networkID)
	if err != nil {
		h.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.CryptoNetworkStateFromDomain(state))
}

func (h *CryptoNetworkHandler) UpdatePolicy(c *gin.Context) {
	networkID, ok := h.networkID(c)
	if !ok {
		return
	}
	var request dto.UpdateCryptoNetworkPolicyRequest
	if !h.BindJSON(c, &request) {
		return
	}
	patch, err := request.ToDomain()
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid crypto network policy").WithCause(err))
		return
	}
	state, err := h.svc.Update(c.Request.Context(), networkID, patch)
	if err != nil {
		h.Error(c, err)
		return
	}
	response := dto.CryptoNetworkStateFromDomain(state)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

func (h *CryptoNetworkHandler) SetWatching(c *gin.Context) {
	networkID, ok := h.networkID(c)
	if !ok {
		return
	}
	var request dto.SetCryptoNetworkWatchingRequest
	if !h.BindJSON(c, &request) {
		return
	}
	state, err := h.svc.SetWatching(c.Request.Context(), networkID, *request.Watching, request.Reason)
	if err != nil {
		h.Error(c, err)
		return
	}
	response := dto.CryptoNetworkStateFromDomain(state)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

func (h *CryptoNetworkHandler) networkID(c *gin.Context) (id.ID, bool) {
	value, err := id.Parse(c.Param("networkId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid networkId"))
		return id.Nil(), false
	}
	return value, true
}
