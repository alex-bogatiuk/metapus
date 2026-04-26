// Package handlers provides HTTP request handlers.
package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/eventlog"
	"metapus/internal/core/id"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/security_profile"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/pkg/logger"
)

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	*BaseHandler
	service        *auth.Service
	profileRepo    security_profile.Repository
	eventWriter    eventlog.Writer
	wsTicketStore  *auth.WSTicketStore
}

// NewAuthHandler creates a new auth handler.
func NewAuthHandler(base *BaseHandler, service *auth.Service, profileRepo security_profile.Repository, eventWriter eventlog.Writer, wsTicketStore *auth.WSTicketStore) *AuthHandler {
	return &AuthHandler{
		BaseHandler:   base,
		service:       service,
		profileRepo:   profileRepo,
		eventWriter:   eventWriter,
		wsTicketStore: wsTicketStore,
	}
}

// emitSessionEvent writes a session event to the event log (best-effort).
func (h *AuthHandler) emitSessionEvent(ctx context.Context, eventType eventlog.EventType, severity eventlog.Severity, email, clientIP, message string, details map[string]any) {
	if h.eventWriter == nil {
		return
	}
	event := eventlog.Event{
		Category:  eventlog.CategorySession,
		Severity:  severity,
		EventType: eventType,
		Source:    "api",
		ClientIP:  clientIP,
		Message:   message,
		Details:   details,
	}
	if err := h.eventWriter.Write(ctx, event); err != nil {
		logger.Warn(ctx, "eventlog: failed to write session event",
			"eventType", eventType,
			"email", email,
			"error", err,
		)
	}
}

// setRefreshTokenCookie sets the refresh token as an httpOnly cookie.
// This prevents XSS-based token theft — JavaScript cannot read httpOnly cookies.
func (h *AuthHandler) setRefreshTokenCookie(c *gin.Context, refreshToken string) {
	secure := os.Getenv("APP_ENV") != "development"
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(
		"refreshToken",
		refreshToken,
		7*24*3600,     // 7 days (matches RefreshTokenExpiry)
		"/api/v1/auth", // scoped to auth endpoints only
		"",             // domain: auto-detect
		secure,         // Secure: true in production
		true,           // HttpOnly: always true
	)
}

// clearRefreshTokenCookie clears the refresh token cookie.
func (h *AuthHandler) clearRefreshTokenCookie(c *gin.Context) {
	secure := os.Getenv("APP_ENV") != "development"
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(
		"refreshToken",
		"",
		-1,             // expire immediately
		"/api/v1/auth",
		"",
		secure,
		true,
	)
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
		h.emitSessionEvent(ctx, eventlog.EventSessionLoginFailed, eventlog.SeverityWarning,
			req.Email, c.ClientIP(),
			fmt.Sprintf("Login failed: %s", req.Email),
			map[string]any{"email": req.Email, "user_agent": c.Request.UserAgent(), "error": err.Error()},
		)
		h.Error(c, err)
		return
	}

	h.emitSessionEvent(ctx, eventlog.EventSessionLogin, eventlog.SeverityInfo,
		req.Email, c.ClientIP(),
		fmt.Sprintf("User logged in: %s", user.Email),
		map[string]any{"email": user.Email, "user_id": user.ID.String(), "user_agent": c.Request.UserAgent()},
	)

	// Set refresh token as httpOnly cookie (not in JSON body).
	h.setRefreshTokenCookie(c, tokens.RefreshToken)

	c.JSON(http.StatusOK, dto.LoginResponse{
		Tokens: dto.FromTokenPair(tokens),
		User:   dto.FromUser(user),
	})
}

// Refresh handles POST /auth/refresh
// Reads refresh token exclusively from httpOnly cookie.
// SEC-02: JSON body fallback removed — it would bypass httpOnly protection.
func (h *AuthHandler) Refresh(c *gin.Context) {
	ctx := c.Request.Context()

	// SEC-02: Only accept refresh token from httpOnly cookie.
	// JSON body fallback was removed to prevent XSS-based token replay.
	refreshToken, err := c.Cookie("refreshToken")
	if err != nil || refreshToken == "" {
		h.Error(c, apperror.NewUnauthorized("missing refresh token cookie"))
		return
	}

	info := auth.SessionInfo{
		UserAgent: c.Request.UserAgent(),
		IPAddress: c.ClientIP(),
	}

	tokens, err := h.service.RefreshToken(ctx, refreshToken, info)
	if err != nil {
		// Clear invalid cookie on failure
		h.clearRefreshTokenCookie(c)
		h.Error(c, err)
		return
	}

	h.emitSessionEvent(ctx, eventlog.EventSessionRefresh, eventlog.SeverityInfo,
		"", c.ClientIP(),
		"Token refreshed",
		map[string]any{"user_agent": c.Request.UserAgent()},
	)

	// Set new refresh token cookie.
	h.setRefreshTokenCookie(c, tokens.RefreshToken)

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

	// Clear refresh token cookie on logout.
	h.clearRefreshTokenCookie(c)

	h.emitSessionEvent(ctx, eventlog.EventSessionLogout, eventlog.SeverityInfo,
		user.Email, c.ClientIP(),
		fmt.Sprintf("User logged out: %s", user.Email),
		map[string]any{"email": user.Email, "user_id": user.UserID},
	)

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

	// Enrich with security profile briefs (best-effort)
	if repo, ok := h.profileRepo.(interface {
		GetProfileBriefByUserIDs(ctx context.Context, userIDs []id.ID) (map[id.ID]*security_profile.ProfileBrief, error)
	}); ok && len(users) > 0 {
		userIDs := make([]id.ID, len(users))
		for i := range users {
			userIDs[i] = users[i].ID
		}
		if briefs, err := repo.GetProfileBriefByUserIDs(ctx, userIDs); err == nil {
			for i := range users {
				if brief, ok := briefs[users[i].ID]; ok {
					response[i].SecurityProfile = &dto.SecurityProfileBrief{
						ID:   brief.ID.String(),
						Code: brief.Code,
						Name: brief.Name,
					}
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"items": response, "total": total})
}

// CreateRole handles POST /auth/roles
func (h *AuthHandler) CreateRole(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.CreateRoleRequest
	if !h.BindJSON(c, &req) {
		return
	}

	role, err := h.service.CreateRole(ctx, req.Code, req.Name, req.Description)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.FromRole(role))
}

// GetRole handles GET /auth/roles/:roleId
func (h *AuthHandler) GetRole(c *gin.Context) {
	ctx := c.Request.Context()

	roleID, err := id.Parse(c.Param("roleId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid roleId"))
		return
	}

	role, perms, userCount, err := h.service.GetRole(ctx, roleID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromRoleDetailed(role, perms, userCount))
}

// UpdateRole handles PUT /auth/roles/:roleId
func (h *AuthHandler) UpdateRole(c *gin.Context) {
	ctx := c.Request.Context()

	roleID, err := id.Parse(c.Param("roleId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid roleId"))
		return
	}

	var req dto.UpdateRoleRequest
	if !h.BindJSON(c, &req) {
		return
	}

	role, err := h.service.UpdateRole(ctx, roleID, req.Name, req.Description)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromRole(role))
}

// DeleteRole handles DELETE /auth/roles/:roleId
func (h *AuthHandler) DeleteRole(c *gin.Context) {
	ctx := c.Request.Context()

	roleID, err := id.Parse(c.Param("roleId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid roleId"))
		return
	}

	affectedUsers, err := h.service.DeleteRole(ctx, roleID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "role deleted", "affectedUsers": affectedUsers})
}

// SetRolePermissions handles PUT /auth/roles/:roleId/permissions
func (h *AuthHandler) SetRolePermissions(c *gin.Context) {
	ctx := c.Request.Context()

	roleID, err := id.Parse(c.Param("roleId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid roleId"))
		return
	}

	var req dto.SetRolePermissionsRequest
	if !h.BindJSON(c, &req) {
		return
	}

	permIDs := make([]id.ID, 0, len(req.PermissionIDs))
	for _, pidStr := range req.PermissionIDs {
		pid, err := id.Parse(pidStr)
		if err != nil {
			h.Error(c, apperror.NewValidation("invalid permission id: "+pidStr))
			return
		}
		permIDs = append(permIDs, pid)
	}

	if err := h.service.SetRolePermissions(ctx, roleID, permIDs); err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "permissions updated"})
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

// Impersonate handles POST /auth/users/:userId/impersonate (admin only).
// Returns tokens that allow acting as the target user.
func (h *AuthHandler) Impersonate(c *gin.Context) {
	ctx := c.Request.Context()

	targetUserID, err := id.Parse(c.Param("userId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid userId"))
		return
	}

	info := auth.SessionInfo{
		UserAgent: c.Request.UserAgent(),
		IPAddress: c.ClientIP(),
	}

	// Get admin user from context for audit trail
	adminUser := appctx.GetUser(ctx)

	tokens, user, err := h.service.Impersonate(ctx, targetUserID, info)
	if err != nil {
		h.Error(c, err)
		return
	}

	adminEmail := ""
	adminID := ""
	if adminUser != nil {
		adminEmail = adminUser.Email
		adminID = adminUser.UserID
	}
	h.emitSessionEvent(ctx, eventlog.EventSessionImpersonate, eventlog.SeverityWarning,
		adminEmail, c.ClientIP(),
		fmt.Sprintf("Admin %s impersonated user %s", adminEmail, user.Email),
		map[string]any{
			"admin_email":    adminEmail,
			"admin_user_id":  adminID,
			"target_email":   user.Email,
			"target_user_id": targetUserID.String(),
		},
	)

	// SEC-01: Set refresh token as httpOnly cookie (same as Login/Refresh).
	h.setRefreshTokenCookie(c, tokens.RefreshToken)

	c.JSON(http.StatusOK, dto.LoginResponse{
		Tokens: dto.FromTokenPair(tokens),
		User:   dto.FromUser(user),
	})
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
	protected.POST("/users/:userId/impersonate", middleware.RequireRole("admin"), h.Impersonate)
	protected.GET("/roles", h.ListRoles)
	protected.POST("/roles", middleware.RequireRole("admin"), h.CreateRole)
	protected.GET("/roles/:roleId", h.GetRole)
	protected.PUT("/roles/:roleId", middleware.RequireRole("admin"), h.UpdateRole)
	protected.DELETE("/roles/:roleId", middleware.RequireRole("admin"), h.DeleteRole)
	protected.GET("/roles/:roleId/permissions", h.ListRolePermissions)
	protected.PUT("/roles/:roleId/permissions", middleware.RequireRole("admin"), h.SetRolePermissions)
	protected.GET("/permissions", h.ListPermissions)

	// WebSocket ticket issuer (requires JWT auth)
	if h.wsTicketStore != nil {
		protected.POST("/ws-ticket", h.IssueWSTicket)
	}
}

// IssueWSTicket handles POST /auth/ws-ticket — returns a single-use ticket for WebSocket auth.
func (h *AuthHandler) IssueWSTicket(c *gin.Context) {
	ctx := c.Request.Context()

	user := appctx.GetUser(ctx)
	if user == nil {
		h.Error(c, apperror.NewUnauthorized("not authenticated"))
		return
	}

	ticket, err := h.wsTicketStore.IssueTicket(user.UserID, user.TenantID)
	if err != nil {
		h.Error(c, apperror.NewInternal(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"ticket": ticket})
}
