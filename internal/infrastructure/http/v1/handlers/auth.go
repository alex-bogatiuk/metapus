// Package handlers provides HTTP request handlers.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain/auth"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/middleware"
)

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	*BaseHandler
	service *auth.Service
}

// NewAuthHandler creates a new auth handler.
func NewAuthHandler(base *BaseHandler, service *auth.Service) *AuthHandler {
	return &AuthHandler{
		BaseHandler: base,
		service:     service,
	}
}

// Register handles POST /auth/register
func (h *AuthHandler) Register(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.RegisterRequest
	if !h.BindJSON(c, &req) {
		return
	}

	user, err := h.service.Register(ctx, req.ToAuthRequest())
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.FromUser(user))
}

// Login handles POST /auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.LoginRequest
	if !h.BindJSON(c, &req) {
		return
	}

	tokens, user, err := h.service.Login(ctx, req.ToCredentials())
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.LoginResponse{
		Tokens: dto.FromTokenPair(tokens),
		User:   dto.FromUser(user),
	})
}

// Refresh handles POST /auth/refresh
func (h *AuthHandler) Refresh(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.RefreshTokenRequest
	if !h.BindJSON(c, &req) {
		return
	}

	tokens, err := h.service.RefreshToken(ctx, req.RefreshToken)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromTokenPair(tokens))
}

// Logout handles POST /auth/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	ctx := c.Request.Context()

	user := appctx.GetUser(ctx)
	if user == nil {
		h.Error(c, apperror.NewUnauthorized("not authenticated"))
		return
	}

	userID, err := id.Parse(user.UserID)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid user id"))
		return
	}

	if err := h.service.Logout(ctx, userID); err != nil {
		h.Error(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// Me handles GET /auth/me
func (h *AuthHandler) Me(c *gin.Context) {
	ctx := c.Request.Context()

	userCtx := appctx.GetUser(ctx)
	if userCtx == nil {
		h.Error(c, apperror.NewUnauthorized("not authenticated"))
		return
	}

	userID, err := id.Parse(userCtx.UserID)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid user id"))
		return
	}

	user, err := h.service.GetUserByID(ctx, userID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromUser(user))
}

// AssignRole handles POST /auth/assign-role
func (h *AuthHandler) AssignRole(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.AssignRoleRequest
	if !h.BindJSON(c, &req) {
		return
	}

	userID, err := id.Parse(req.UserID)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid user id"))
		return
	}

	if err := h.service.AssignRole(ctx, userID, req.RoleCode); err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "role assigned successfully"})
}

// RevokeRole handles POST /auth/revoke-role
func (h *AuthHandler) RevokeRole(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.AssignRoleRequest
	if !h.BindJSON(c, &req) {
		return
	}

	userID, err := id.Parse(req.UserID)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid user id"))
		return
	}

	if err := h.service.RevokeRole(ctx, userID, req.RoleCode); err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "role revoked successfully"})
}

// ListRoles handles GET /auth/roles
func (h *AuthHandler) ListRoles(c *gin.Context) {
	ctx := c.Request.Context()
	roles, err := h.service.ListRoles(ctx)
	if err != nil {
		h.Error(c, err)
		return
	}

	response := make([]*dto.RoleResponse, len(roles))
	for i := range roles {
		response[i] = dto.FromRole(&roles[i])
	}

	c.JSON(http.StatusOK, gin.H{"items": response})
}

// ListPermissions handles GET /auth/permissions
func (h *AuthHandler) ListPermissions(c *gin.Context) {
	ctx := c.Request.Context()

	permissions, err := h.service.ListPermissions(ctx)
	if err != nil {
		h.Error(c, err)
		return
	}

	response := make([]*dto.PermissionResponse, len(permissions))
	for i := range permissions {
		response[i] = dto.FromPermission(&permissions[i])
	}

	c.JSON(http.StatusOK, gin.H{"items": response})
}

// RegisterRoutes registers auth routes.
func (h *AuthHandler) RegisterRoutes(public, protected *gin.RouterGroup) {
	// Public routes (no auth required)
	public.POST("/register", h.Register)
	public.POST("/login", h.Login)
	public.POST("/refresh", h.Refresh)

	// Protected routes (auth required)
	protected.POST("/logout", h.Logout)
	protected.GET("/me", h.Me)
	// NOTE: These endpoints are privileged. Keep them protected from privilege escalation.
	protected.POST("/assign-role", middleware.RequireRole("admin"), h.AssignRole)
	protected.POST("/revoke-role", middleware.RequireRole("admin"), h.RevokeRole)
	protected.GET("/roles", h.ListRoles)
	protected.GET("/permissions", h.ListPermissions)
}
