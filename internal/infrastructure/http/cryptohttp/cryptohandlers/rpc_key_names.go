package cryptohandlers

import (
	"context"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto/rpcconfig"
	"metapus/internal/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
)

// RPCKeyOptionsReader lists the key names, never values, an endpoint may read.
type RPCKeyOptionsReader interface {
	RPCKeyOptions(context.Context, rpcconfig.RPCKeyQuery) (rpcconfig.RPCKeyOptions, error)
}

// RPCKeyNamesHandler serves the RPC endpoint form next to the platform's
// integration pool routes.
type RPCKeyNamesHandler struct {
	*handlers.BaseHandler
	reader RPCKeyOptionsReader
}

func NewRPCKeyNamesHandler(reader RPCKeyOptionsReader) *RPCKeyNamesHandler {
	return &RPCKeyNamesHandler{BaseHandler: handlers.NewBaseHandler(), reader: reader}
}

// RegisterRoutes mounts GET /integrations/pools/:id/rpc-key-names on the system group.
func (h *RPCKeyNamesHandler) RegisterRoutes(system *gin.RouterGroup) {
	system.GET("/integrations/pools/:id/rpc-key-names", middleware.RequireAnyPermission("catalog:integration_pool:read", "catalog:crypto_rpc_endpoint:create", "catalog:crypto_rpc_endpoint:update"), h.RPCKeyNames)
}

// RPCKeyNames answers for an endpoint draft: networkId, role and url are query
// parameters. Without networkId only the pool's common key names are returned.
func (h *RPCKeyNamesHandler) RPCKeyNames(c *gin.Context) {
	pool, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("Некорректный идентификатор пула"))
		return
	}
	q := rpcconfig.RPCKeyQuery{PoolID: pool, Role: c.Query("role"), URL: c.Query("url")}
	if raw := c.Query("networkId"); raw != "" {
		network, err := id.Parse(raw)
		if err != nil {
			h.Error(c, apperror.NewValidation("Некорректная сеть"))
			return
		}
		q.NetworkID = network
	}
	options, err := h.reader.RPCKeyOptions(c.Request.Context(), q)
	if err != nil {
		h.Error(c, err)
		return
	}
	h.OK(c, cryptodto.FromIntegrationRPCKeyOptions(options))
}
