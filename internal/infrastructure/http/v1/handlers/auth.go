// Package handlers provides HTTP request handlers.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/security_profile"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/middleware"
)

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	*BaseHandler
	service     *auth.Service
	profileRepo security_profile.Repository
}

// NewAuthHandler creates a new auth handler.
func NewAuthHandler(base *BaseHandler, service *auth.Service, profileRepo security_profile.Repository) *AuthHandler {
	return &AuthHandler{
		BaseHandler: base,
		service:     service,
		profileRepo: profileRepo,
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

	info := auth.SessionInfo{
		UserAgent: c.Request.UserAgent(),
		IPAddress: c.ClientIP(),
	}

	tokens, user, err := h.service.Login(ctx, req.ToCredentials(), info)
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

	info := auth.SessionInfo{
		UserAgent: c.Request.UserAgent(),
		IPAddress: c.ClientIP(),
	}

	tokens, err := h.service.RefreshToken(ctx, req.RefreshToken, info)
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

// GetUser handles GET /auth/users/:userId (admin only)
func (h *AuthHandler) GetUser(c *gin.Context) {
	ctx := c.Request.Context()

	userID, err := id.Parse(c.Param("userId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid userId"))
		return
	}

	user, err := h.service.GetUserByID(ctx, userID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromUser(user))
}

// UpdateUser handles PUT /auth/users/:userId (admin only)
func (h *AuthHandler) UpdateUser(c *gin.Context) {
	ctx := c.Request.Context()

	userID, err := id.Parse(c.Param("userId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid userId"))
		return
	}

	var req dto.UpdateUserRequest
	if !h.BindJSON(c, &req) {
		return
	}

	user, err := h.service.UpdateUser(ctx, userID, req.FirstName, req.LastName, req.IsActive, req.IsAdmin)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromUser(user))
}

// CreateUserByAdmin handles POST /auth/users (admin only)
func (h *AuthHandler) CreateUserByAdmin(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.CreateUserAdminRequest
	if !h.BindJSON(c, &req) {
		return
	}

	domainReq := auth.RegisterRequest{
		Email:     req.Email,
		Password:  req.Password,
		FirstName: req.FirstName,
		LastName:  req.LastName,
	}

	user, err := h.service.CreateUserByAdmin(ctx, domainReq, req.RoleCodes)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.FromUser(user))
}

// ListRolePermissions handles GET /auth/roles/:roleId/permissions
func (h *AuthHandler) ListRolePermissions(c *gin.Context) {
	ctx := c.Request.Context()

	roleID, err := id.Parse(c.Param("roleId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid roleId"))
		return
	}

	permissions, err := h.service.ListRolePermissions(ctx, roleID)
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

// GetEffectiveAccess handles GET /auth/users/:userId/effective-access (admin only).
// Returns the combined RBAC permissions, RLS dimensions, FLS policies, and CEL rules for a user.
func (h *AuthHandler) GetEffectiveAccess(c *gin.Context) {
	ctx := c.Request.Context()

	userID, err := id.Parse(c.Param("userId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid userId"))
		return
	}

	user, err := h.service.GetUserByID(ctx, userID)
	if err != nil {
		h.Error(c, err)
		return
	}

	resp := dto.EffectiveAccessResponse{
		User:        dto.FromUser(user),
		Permissions: user.Permissions,
	}

	// Load security profile for RLS/FLS/CEL
	if h.profileRepo != nil {
		profile, err := h.profileRepo.GetByUserID(ctx, userID)
		if err == nil && profile != nil {
			// RLS dimensions (unresolved — just IDs for now)
			if len(profile.Dimensions) > 0 {
				dims := make(map[string][]dto.RLSDimensionItem, len(profile.Dimensions))
				for dimName, ids := range profile.Dimensions {
					items := make([]dto.RLSDimensionItem, len(ids))
					for i, id := range ids {
						items[i] = dto.RLSDimensionItem{ID: id}
					}
					dims[dimName] = items
				}
				resp.RLSDimensions = dims
			}

			// FLS policies — extract hidden fields
			if len(profile.FieldPolicies) > 0 {
				for _, fp := range profile.FieldPolicies {
					hidden := []string{}
					for _, f := range fp.AllowedFields {
						if len(f) > 1 && f[0] == '-' {
							hidden = append(hidden, f[1:])
						}
					}
					if len(hidden) > 0 {
						resp.FLSPolicies = append(resp.FLSPolicies, dto.EffectiveFLSPolicy{
							EntityName:   fp.EntityName,
							Action:       fp.Action,
							HiddenFields: hidden,
						})
					}
				}
			}

			// CEL rules
			if len(profile.PolicyRules) > 0 {
				for _, rule := range profile.PolicyRules {
					if !rule.Enabled {
						continue
					}
					resp.CELRules = append(resp.CELRules, dto.EffectiveCELRule{
						Name:       rule.Name,
						EntityName: rule.EntityName,
						Effect:     rule.Effect,
						Expression: rule.Expression,
						Priority:   rule.Priority,
					})
				}
			}
		}
	}

	c.JSON(http.StatusOK, resp)
}

// ListUsers handles GET /auth/users (admin only)
func (h *AuthHandler) ListUsers(c *gin.Context) {
	ctx := c.Request.Context()

	filter := auth.UserFilter{
		Search: c.Query("search"),
		Limit:  100,
	}

	users, total, err := h.service.ListUsers(ctx, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	response := make([]*dto.UserResponse, len(users))
	for i := range users {
		response[i] = dto.FromUser(&users[i])
	}

	c.JSON(http.StatusOK, gin.H{"items": response, "total": total})
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
	protected.GET("/users", middleware.RequireRole("admin"), h.ListUsers)
	protected.POST("/users", middleware.RequireRole("admin"), h.CreateUserByAdmin)
	protected.GET("/users/:userId", middleware.RequireRole("admin"), h.GetUser)
	protected.PUT("/users/:userId", middleware.RequireRole("admin"), h.UpdateUser)
	protected.GET("/users/:userId/effective-access", middleware.RequireRole("admin"), h.GetEffectiveAccess)
	protected.GET("/roles", h.ListRoles)
	protected.GET("/roles/:roleId/permissions", h.ListRolePermissions)
	protected.GET("/permissions", h.ListPermissions)
}
