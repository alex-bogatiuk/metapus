package handlers

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
)

// ServiceAccountHandler handles API endpoints for service accounts.
type ServiceAccountHandler struct {
	*BaseHandler
	repo    integrations.Repository
	credMgr integrations.CredentialManager
}

// NewServiceAccountHandler creates a new handler.
func NewServiceAccountHandler(base *BaseHandler, repo integrations.Repository, credMgr integrations.CredentialManager) *ServiceAccountHandler {
	return &ServiceAccountHandler{
		BaseHandler: base,
		repo:        repo,
		credMgr:     credMgr,
	}
}

// List returns all service accounts.
func (h *ServiceAccountHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	accounts, err := h.repo.List(ctx)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Always return an array, even if empty
	if accounts == nil {
		accounts = []integrations.ServiceAccount{}
	}

	h.OK(c, accounts)
}

// Get returns a single service account.
func (h *ServiceAccountHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()

	accountID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id parameter").WithDetail("id", c.Param("id")))
		return
	}

	account, err := h.repo.GetByID(ctx, accountID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.OK(c, account)
}

// Create handles the creation of a new service account.
func (h *ServiceAccountHandler) Create(c *gin.Context) {
	ctx := c.Request.Context()

	var req integrations.CreateRequest
	if !h.BindJSON(c, &req) {
		return
	}

	if err := req.Validate(ctx); err != nil {
		h.Error(c, err)
		return
	}

	account, err := h.repo.Create(ctx, req)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Created(c, account.ID.String()) // Or return full object? Usually we return ID in Metapus.
}

// Update modifies an existing service account (excluding credentials).
func (h *ServiceAccountHandler) Update(c *gin.Context) {
	ctx := c.Request.Context()

	accountID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id parameter").WithDetail("id", c.Param("id")))
		return
	}

	var req integrations.UpdateRequest
	if !h.BindJSON(c, &req) {
		return
	}

	if err := req.Validate(ctx); err != nil {
		h.Error(c, err)
		return
	}

	account, err := h.repo.Update(ctx, accountID, req)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.OK(c, account)
}

// Delete removes a service account.
func (h *ServiceAccountHandler) Delete(c *gin.Context) {
	ctx := c.Request.Context()

	accountID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id parameter").WithDetail("id", c.Param("id")))
		return
	}

	if err := h.repo.Delete(ctx, accountID); err != nil {
		h.Error(c, err)
		return
	}

	h.NoContent(c)
}

// UpdateCredentials updates only the encrypted credentials for a service account.
func (h *ServiceAccountHandler) UpdateCredentials(c *gin.Context) {
	ctx := c.Request.Context()

	accountID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id parameter").WithDetail("id", c.Param("id")))
		return
	}

	var req integrations.UpdateCredentialsRequest
	if !h.BindJSON(c, &req) {
		return
	}

	if err := h.credMgr.WriteCredentials(ctx, accountID, req.Credentials); err != nil {
		h.Error(c, err)
		return
	}

	h.NoContent(c)
}

// TestConnection is a stub for testing out a service account connection.
func (h *ServiceAccountHandler) TestConnection(c *gin.Context) {
	// For API stubbing in Phase 1
	h.Success(c, "Connection successful (stub)")
}

// RegisterRoutes registers the handlers to the Gin router group.
func (h *ServiceAccountHandler) RegisterRoutes(rg *gin.RouterGroup) {
	accounts := rg.Group("/service-accounts")
	{
		accounts.GET("", h.List)
		accounts.POST("", h.Create)
		accounts.GET("/:id", h.Get)
		accounts.PUT("/:id", h.Update)
		accounts.DELETE("/:id", h.Delete)
		
		accounts.PUT("/:id/credentials", h.UpdateCredentials)
		accounts.POST("/:id/test", h.TestConnection)
	}
}
