package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/integrationapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/middleware"
)

type IntegrationHandler struct {
	*BaseHandler
	service *integrationapp.Service
	tester  *integrationapp.ConnectionTestService
}

func NewIntegrationHandler(service *integrationapp.Service, testers ...*integrationapp.ConnectionTestService) *IntegrationHandler {
	h := &IntegrationHandler{BaseHandler: NewBaseHandler(), service: service}
	if len(testers) > 0 {
		h.tester = testers[0]
	}
	return h
}
func (h *IntegrationHandler) RegisterRoutes(group *gin.RouterGroup, guard gin.HandlerFunc) {
	g := group.Group("/integrations")
	g.GET("/providers", middleware.RequireAnyPermission("catalog:integration_account:read", "catalog:integration_account:create", "catalog:integration_account:update", "catalog:integration_pool:read", "catalog:integration_pool:create", "catalog:integration_pool:update"), h.Providers)
	g.GET("/pools/:id/status", middleware.RequirePermission("catalog:integration_pool:read"), h.Status)
	g.POST("/pools/:id/command", middleware.RequirePermission("catalog:integration_pool:update"), guard, middleware.CommandIdempotency(true), h.Command)
	g.POST("/pools/:id/test", middleware.RequirePermission("catalog:integration_pool:update"), guard, h.Test)
	g.GET("/pools/:id/rpc-key-names", middleware.RequireAnyPermission("catalog:integration_pool:read", "catalog:crypto_rpc_endpoint:create", "catalog:crypto_rpc_endpoint:update"), h.RPCKeyNames)
}

// RPCKeyNames answers for an endpoint draft: networkId, role and url are query
// parameters. Without networkId only the pool's common key names are returned.
func (h *IntegrationHandler) RPCKeyNames(c *gin.Context) {
	uid, ok := h.poolID(c)
	if !ok {
		return
	}
	q := integrations.RPCKeyQuery{PoolID: uid, Role: c.Query("role"), URL: c.Query("url")}
	if raw := c.Query("networkId"); raw != "" {
		network, err := id.Parse(raw)
		if err != nil {
			h.Error(c, apperror.NewValidation("Некорректная сеть"))
			return
		}
		q.NetworkID = network
	}
	options, err := h.service.RPCKeyOptions(c.Request.Context(), q)
	if err != nil {
		h.Error(c, err)
		return
	}
	h.OK(c, dto.FromIntegrationRPCKeyOptions(options))
}

func (h *IntegrationHandler) Providers(c *gin.Context) {
	h.OK(c, dto.IntegrationProviders())
}
func (h *IntegrationHandler) poolID(c *gin.Context) (id.ID, bool) {
	uid, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("Некорректный идентификатор пула"))
		return id.Nil(), false
	}
	return uid, true
}
func (h *IntegrationHandler) Status(c *gin.Context) {
	uid, ok := h.poolID(c)
	if !ok {
		return
	}
	state, err := h.service.Status(c.Request.Context(), uid)
	if err != nil {
		h.Error(c, err)
		return
	}
	h.OK(c, dto.FromIntegrationPoolStatus(state))
}
func (h *IntegrationHandler) Command(c *gin.Context) {
	uid, ok := h.poolID(c)
	if !ok {
		return
	}
	var req dto.IntegrationPoolCommandRequest
	if !h.BindJSON(c, &req) {
		return
	}
	state, err := h.service.Command(c.Request.Context(), uid, req.Action, req.Reason, req.ExpectedVersion)
	if err != nil {
		h.Error(c, err)
		return
	}
	out := dto.FromIntegrationPoolStatus(state)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", out)
	h.OK(c, out)
}
func (h *IntegrationHandler) Test(c *gin.Context) {
	uid, ok := h.poolID(c)
	if !ok {
		return
	}
	ready, err := h.tester.Test(c.Request.Context(), uid)
	if err != nil {
		h.Error(c, err)
		return
	}
	message := "Не удалось подтвердить доступ. Проверьте состояние аккаунтов."
	if ready {
		message = "Доступ подтверждён"
	}
	h.OK(c, dto.IntegrationConnectionTestResponse{Ready: ready, Message: message})
}
