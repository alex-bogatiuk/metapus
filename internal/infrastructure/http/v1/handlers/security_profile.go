package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/security_profile"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres"
)

// SecurityProfileHandler handles CRUD operations for security profiles.
type SecurityProfileHandler struct {
	BaseHandler
	repo  security_profile.Repository
	audit *postgres.AuditService
}

// NewSecurityProfileHandler creates a new SecurityProfileHandler.
func NewSecurityProfileHandler(repo security_profile.Repository, audit *postgres.AuditService) *SecurityProfileHandler {
	return &SecurityProfileHandler{repo: repo, audit: audit}
}

// List returns all security profiles.
// GET /api/v1/security/profiles
func (h *SecurityProfileHandler) List(c *gin.Context) {
	profiles, err := h.repo.List(c.Request.Context())
	if err != nil {
		h.Error(c, err)
		return
	}

	items := dto.FromSecurityProfiles(profiles)
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// Get retrieves a single security profile by ID.
// GET /api/v1/security/profiles/:profileId
func (h *SecurityProfileHandler) Get(c *gin.Context) {
	profileID, err := id.Parse(c.Param("profileId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid profileId"))
		return
	}

	profile, err := h.repo.GetByID(c.Request.Context(), profileID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromSecurityProfile(profile))
}

// Create creates a new security profile.
// POST /api/v1/security/profiles
func (h *SecurityProfileHandler) Create(c *gin.Context) {
	var req dto.CreateSecurityProfileRequest
	if !h.BindJSON(c, &req) {
		return
	}

	profile := req.ToDomain()

	if err := profile.Validate(c.Request.Context()); err != nil {
		h.Error(c, err)
		return
	}

	if err := h.repo.Create(c.Request.Context(), profile); err != nil {
		h.Error(c, err)
		return
	}

	// Audit log
	h.logAudit(c.Request.Context(), profile.ID, postgres.AuditActionCreate, map[string]any{
		"code": profile.Code,
		"name": profile.Name,
	})

	// Re-fetch to get timestamps and full data
	created, err := h.repo.GetByID(c.Request.Context(), profile.ID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.FromSecurityProfile(created))
}

// Update modifies an existing security profile.
// PUT /api/v1/security/profiles/:profileId
func (h *SecurityProfileHandler) Update(c *gin.Context) {
	profileID, err := id.Parse(c.Param("profileId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid profileId"))
		return
	}

	var req dto.UpdateSecurityProfileRequest
	if !h.BindJSON(c, &req) {
		return
	}

	// Fetch existing profile (snapshot before changes)
	oldProfile, err := h.repo.GetByID(c.Request.Context(), profileID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Build old state for diff
	oldState := profileSnapshot(oldProfile)

	// Apply partial updates
	req.ApplyTo(oldProfile)

	if err := oldProfile.Validate(c.Request.Context()); err != nil {
		h.Error(c, err)
		return
	}

	if err := h.repo.Update(c.Request.Context(), oldProfile); err != nil {
		h.Error(c, err)
		return
	}

	// Audit log — compute diff
	newState := profileSnapshot(oldProfile)
	if diff := postgres.Diff(oldState, newState); len(diff) > 0 {
		h.logAudit(c.Request.Context(), profileID, postgres.AuditActionUpdate, diff)
	}

	// Re-fetch to get updated timestamps
	updated, err := h.repo.GetByID(c.Request.Context(), profileID)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.FromSecurityProfile(updated))
}

// Delete removes a security profile.
// DELETE /api/v1/security/profiles/:profileId
func (h *SecurityProfileHandler) Delete(c *gin.Context) {
	profileID, err := id.Parse(c.Param("profileId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid profileId"))
		return
	}

	// Fetch before delete for audit
	profile, fetchErr := h.repo.GetByID(c.Request.Context(), profileID)

	if err := h.repo.Delete(c.Request.Context(), profileID); err != nil {
		h.Error(c, err)
		return
	}

	// Audit log
	changes := map[string]any{"profileId": profileID.String()}
	if fetchErr == nil && profile != nil {
		changes["code"] = profile.Code
		changes["name"] = profile.Name
	}
	h.logAudit(c.Request.Context(), profileID, postgres.AuditActionDelete, changes)

	h.Success(c, "profile deleted")
}

// ListProfileUsers returns users assigned to a security profile.
// GET /api/v1/security/profiles/:profileId/users
func (h *SecurityProfileHandler) ListProfileUsers(c *gin.Context) {
	profileID, err := id.Parse(c.Param("profileId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid profileId"))
		return
	}

	repo, ok := h.repo.(interface {
		ListUsersByProfileID(ctx context.Context, profileID id.ID) ([]security_profile.ProfileUser, error)
	})
	if !ok {
		h.Error(c, apperror.NewInternal(nil).WithDetail("reason", "repo does not support ListUsersByProfileID"))
		return
	}

	users, err := repo.ListUsersByProfileID(c.Request.Context(), profileID)
	if err != nil {
		h.Error(c, err)
		return
	}

	items := make([]dto.ProfileUserItem, len(users))
	for i, u := range users {
		items[i] = dto.ProfileUserItem{
			ID:       u.ID.String(),
			Email:    u.Email,
			FullName: u.FullName(),
		}
	}

	c.JSON(http.StatusOK, gin.H{"items": items})
}

// AssignUser assigns a user to a security profile.
// POST /api/v1/security/profiles/:profileId/users
func (h *SecurityProfileHandler) AssignUser(c *gin.Context) {
	profileID, err := id.Parse(c.Param("profileId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid profileId"))
		return
	}

	var req dto.AssignProfileRequest
	if !h.BindJSON(c, &req) {
		return
	}

	userID, err := id.Parse(req.UserID)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid userId"))
		return
	}

	if err := h.repo.AssignToUser(c.Request.Context(), userID, profileID); err != nil {
		h.Error(c, err)
		return
	}

	// Audit log
	h.logAudit(c.Request.Context(), profileID, postgres.AuditActionUpdate, map[string]any{
		"action": "assign_user",
		"userId": userID.String(),
	})

	h.Success(c, "user assigned to profile")
}

// RemoveUser removes a user from a security profile.
// DELETE /api/v1/security/profiles/:profileId/users/:userId
func (h *SecurityProfileHandler) RemoveUser(c *gin.Context) {
	profileID, err := id.Parse(c.Param("profileId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid profileId"))
		return
	}

	userID, err := id.Parse(c.Param("userId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid userId"))
		return
	}

	if err := h.repo.RemoveFromUser(c.Request.Context(), userID, profileID); err != nil {
		h.Error(c, err)
		return
	}

	// Audit log
	h.logAudit(c.Request.Context(), profileID, postgres.AuditActionUpdate, map[string]any{
		"action": "remove_user",
		"userId": userID.String(),
	})

	h.Success(c, "user removed from profile")
}

// GetAuditHistory returns audit log entries for a security profile.
// GET /api/v1/security/profiles/:profileId/audit
func (h *SecurityProfileHandler) GetAuditHistory(c *gin.Context) {
	profileID, err := id.Parse(c.Param("profileId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid profileId"))
		return
	}

	limit := h.ParseIntQuery(c, "limit", 50)
	if limit > 200 {
		limit = 200
	}

	entries, err := h.audit.GetEntityHistory(c.Request.Context(), "security_profile", profileID, limit)
	if err != nil {
		h.Error(c, err)
		return
	}

	items := make([]dto.AuditEntryResponse, len(entries))
	for i, e := range entries {
		var changes map[string]any
		if len(e.Changes) > 0 {
			_ = json.Unmarshal(e.Changes, &changes)
		}
		items[i] = dto.AuditEntryResponse{
			ID:        e.ID.String(),
			Action:    string(e.Action),
			UserID:    e.UserID,
			UserEmail: e.UserEmail,
			Changes:   changes,
			CreatedAt: e.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, gin.H{"items": items})
}

// --- Internal helpers ---

// logAudit logs an audit entry for a security profile, ignoring errors (best-effort).
func (h *SecurityProfileHandler) logAudit(ctx context.Context, entityID id.ID, action postgres.AuditAction, changes map[string]any) {
	if h.audit == nil {
		return
	}
	_ = h.audit.LogChange(ctx, "security_profile", entityID, action, changes)
}

// profileSnapshot creates a flat map of a profile's state for diff computation.
func profileSnapshot(p *security_profile.SecurityProfile) map[string]any {
	m := map[string]any{
		"code":        p.Code,
		"name":        p.Name,
		"description": p.Description,
	}
	if p.Dimensions != nil {
		dimsJSON, _ := json.Marshal(p.Dimensions)
		m["dimensions"] = string(dimsJSON)
	}
	if p.FieldPolicies != nil {
		fpJSON, _ := json.Marshal(p.FieldPolicies)
		m["fieldPolicies"] = string(fpJSON)
	}
	return m
}
