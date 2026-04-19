package handlers

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/automations"
)

// AutomationAccountHandler handles API endpoints for automation accounts.
type AutomationAccountHandler struct {
	*BaseHandler
	repo    automations.AccountRepository
	credMgr automations.CredentialManager
}

// NewAutomationAccountHandler creates a new handler.
func NewAutomationAccountHandler(base *BaseHandler, repo automations.AccountRepository, credMgr automations.CredentialManager) *AutomationAccountHandler {
	return &AutomationAccountHandler{
		BaseHandler: base,
		repo:        repo,
		credMgr:     credMgr,
	}
}

// List returns all automation accounts.
func (h *AutomationAccountHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	accounts, err := h.repo.List(ctx)
	if err != nil {
		h.Error(c, err)
		return
	}

	if accounts == nil {
		accounts = []automations.Account{}
	}

	h.OK(c, accounts)
}

// Get returns a single automation account.
func (h *AutomationAccountHandler) Get(c *gin.Context) {
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

// Create handles the creation of a new automation account.
func (h *AutomationAccountHandler) Create(c *gin.Context) {
	ctx := c.Request.Context()

	var req automations.CreateAccountRequest
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

	h.Created(c, account.ID.String())
}

// Update modifies an existing automation account (excluding credentials).
func (h *AutomationAccountHandler) Update(c *gin.Context) {
	ctx := c.Request.Context()

	accountID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id parameter").WithDetail("id", c.Param("id")))
		return
	}

	var req automations.UpdateAccountRequest
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

// Delete removes an automation account.
func (h *AutomationAccountHandler) Delete(c *gin.Context) {
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

// UpdateCredentials updates only the encrypted credentials for an account.
func (h *AutomationAccountHandler) UpdateCredentials(c *gin.Context) {
	ctx := c.Request.Context()

	accountID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id parameter").WithDetail("id", c.Param("id")))
		return
	}

	var req struct {
		Credentials string `json:"credentials"`
	}
	if !h.BindJSON(c, &req) {
		return
	}

	if err := h.credMgr.WriteCredentials(ctx, accountID, []byte(req.Credentials)); err != nil {
		h.Error(c, err)
		return
	}

	h.NoContent(c)
}

// TestConnection is a stub for testing an account connection.
func (h *AutomationAccountHandler) TestConnection(c *gin.Context) {
	h.Success(c, "Connection successful (stub)")
}

// RegisterRoutes registers the handlers to the Gin router group.
func (h *AutomationAccountHandler) RegisterRoutes(rg *gin.RouterGroup) {
	accounts := rg.Group("/automation-accounts")
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
