package cryptohandlers

import (
	"context"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
)

type CryptoRPCConfigurationHandler struct {
	*handlers.BaseHandler
	service *cryptoapp.RPCConfigurationService
}

func NewCryptoRPCConfigurationHandler(service *cryptoapp.RPCConfigurationService) *CryptoRPCConfigurationHandler {
	return &CryptoRPCConfigurationHandler{BaseHandler: handlers.NewBaseHandler(), service: service}
}

func (h *CryptoRPCConfigurationHandler) RegisterRoutes(rg *gin.RouterGroup, guard gin.HandlerFunc) {
	g := rg.Group("/admin/crypto/networks", middleware.RequirePermission("crypto_network:read"), middleware.RequirePermission("catalog:crypto_rpc_endpoint:read"))
	g.GET("/:networkId/rpc-configuration", h.Get)
	g.PATCH("/:networkId/rpc-configuration", middleware.RequirePermission("catalog:crypto_rpc_endpoint:update"), guard, middleware.CommandIdempotency(true), h.Update)
}

func rpcConfigurationResponse(ctx context.Context, state rpcconfig.RPCConfiguration) cryptodto.CryptoRPCConfigurationResponse {
	u := appctx.GetUser(ctx)
	canManage := u != nil && (u.IsAdmin || slices.Contains(u.Permissions, "catalog:crypto_rpc_endpoint:update"))
	if policy := security.GetFieldPolicy(ctx, "crypto_rpc_endpoint", "write"); policy != nil {
		canManage = canManage && policy.IsFieldAllowed("priority") && policy.IsFieldAllowed("is_active") && policy.IsFieldAllowed("min_request_interval_ms")
	}
	out := cryptodto.CryptoRPCConfigurationResponse{RPCConfiguration: state, CanManage: canManage && state.Source == "database"}
	if state.Source != "database" {
		out.LockedReason = "rpc_configuration_file"
	} else if !canManage {
		out.LockedReason = "permission_denied"
	}
	return out
}

func (h *CryptoRPCConfigurationHandler) network(c *gin.Context) (id.ID, bool) {
	uid, err := id.Parse(c.Param("networkId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("Некорректная сеть"))
		return uid, false
	}
	return uid, true
}

func (h *CryptoRPCConfigurationHandler) Get(c *gin.Context) {
	network, ok := h.network(c)
	if !ok {
		return
	}
	state, err := h.service.Get(c.Request.Context(), network)
	if err != nil {
		h.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, rpcConfigurationResponse(c.Request.Context(), state))
}

func (h *CryptoRPCConfigurationHandler) Update(c *gin.Context) {
	network, ok := h.network(c)
	if !ok {
		return
	}
	var req cryptodto.UpdateCryptoRPCConfigurationRequest
	if !h.BindJSON(c, &req) {
		return
	}
	update, err := req.ToDomain()
	if err != nil {
		h.Error(c, err)
		return
	}
	state, err := h.service.Update(c.Request.Context(), network, update)
	if err != nil {
		h.Error(c, err)
		return
	}
	out := rpcConfigurationResponse(c.Request.Context(), state)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", out)
	c.JSON(http.StatusOK, out)
}
