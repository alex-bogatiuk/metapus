package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/infrastructure/http/v1/dto"
)

// MerchantUserHandler manages user ↔ merchant associations.
// Routes: /api/v1/merchant-admin/merchants/:merchantId/users
type MerchantUserHandler struct {
	repo merchant.MerchantUserRepository
}

// NewMerchantUserHandler creates the handler.
func NewMerchantUserHandler(repo merchant.MerchantUserRepository) *MerchantUserHandler {
	return &MerchantUserHandler{repo: repo}
}

// ListUsers handles GET /merchant-admin/merchants/:merchantId/users.
func (h *MerchantUserHandler) ListUsers(c *gin.Context) {
	merchantID, err := id.Parse(c.Param("merchantId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid merchant id"))
		c.Abort()
		return
	}

	users, err := h.repo.ListByMerchant(c.Request.Context(), merchantID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	items := make([]dto.MerchantUserItem, len(users))
	for i, u := range users {
		items[i] = dto.MerchantUserFromDomain(u)
	}

	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// AddUser handles POST /merchant-admin/merchants/:merchantId/users.
func (h *MerchantUserHandler) AddUser(c *gin.Context) {
	merchantID, err := id.Parse(c.Param("merchantId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid merchant id"))
		c.Abort()
		return
	}

	var req dto.AddMerchantUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation(err.Error()))
		c.Abort()
		return
	}

	userID, err := id.Parse(req.UserID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid user id").WithDetail("field", "userId"))
		c.Abort()
		return
	}

	role := merchant.MerchantRole(req.Role)
	if !role.IsValid() {
		_ = c.Error(apperror.NewValidation("invalid role").WithDetail("field", "role"))
		c.Abort()
		return
	}

	if err := h.repo.Add(c.Request.Context(), userID, merchantID, role); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

// RemoveUser handles DELETE /merchant-admin/merchants/:merchantId/users/:userId.
func (h *MerchantUserHandler) RemoveUser(c *gin.Context) {
	merchantID, err := id.Parse(c.Param("merchantId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid merchant id"))
		c.Abort()
		return
	}

	userID, err := id.Parse(c.Param("userId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid user id"))
		c.Abort()
		return
	}

	if err := h.repo.Remove(c.Request.Context(), userID, merchantID); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

// UpdateRole handles PATCH /merchant-admin/merchants/:merchantId/users/:userId/role.
func (h *MerchantUserHandler) UpdateRole(c *gin.Context) {
	merchantID, err := id.Parse(c.Param("merchantId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid merchant id"))
		c.Abort()
		return
	}

	userID, err := id.Parse(c.Param("userId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid user id"))
		c.Abort()
		return
	}

	var req dto.UpdateMerchantUserRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation(err.Error()))
		c.Abort()
		return
	}

	role := merchant.MerchantRole(req.Role)
	if !role.IsValid() {
		_ = c.Error(apperror.NewValidation("invalid role").WithDetail("field", "role"))
		c.Abort()
		return
	}

	if err := h.repo.UpdateRole(c.Request.Context(), userID, merchantID, role); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}
