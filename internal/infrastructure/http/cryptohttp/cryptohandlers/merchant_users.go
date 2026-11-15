package cryptohandlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/portalapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/infrastructure/http/cryptohttp/cryptodto"
)

// MerchantUserHandler manages user ↔ merchant associations.
// Routes: /api/v1/merchant-admin/merchants/:merchantId/users
type MerchantUserHandler struct {
	service *portalapp.MerchantUserService
}

func NewMerchantUserHandler(service *portalapp.MerchantUserService) *MerchantUserHandler {
	return &MerchantUserHandler{service: service}
}

// ListUsers handles GET /merchant-admin/merchants/:merchantId/users.
func (h *MerchantUserHandler) ListUsers(c *gin.Context) {
	merchantID, err := id.Parse(c.Param("merchantId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid merchant id"))
		c.Abort()
		return
	}

	users, err := h.service.ListByMerchant(c.Request.Context(), merchantID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	items := make([]cryptodto.MerchantUserItem, len(users))
	for i, u := range users {
		items[i] = cryptodto.MerchantUserFromDomain(u)
	}

	c.JSON(http.StatusOK, cryptodto.MerchantUserListResponse{Items: items, Total: len(items)})
}

// AddUser handles POST /merchant-admin/merchants/:merchantId/users.
func (h *MerchantUserHandler) AddUser(c *gin.Context) {
	merchantID, err := id.Parse(c.Param("merchantId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid merchant id"))
		c.Abort()
		return
	}

	var req cryptodto.AddMerchantUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
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

	if err := h.service.Add(c.Request.Context(), userID, merchantID, role); err != nil {
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

	if err := h.service.Remove(c.Request.Context(), userID, merchantID); err != nil {
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

	var req cryptodto.UpdateMerchantUserRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	role := merchant.MerchantRole(req.Role)
	if !role.IsValid() {
		_ = c.Error(apperror.NewValidation("invalid role").WithDetail("field", "role"))
		c.Abort()
		return
	}

	if err := h.service.UpdateRole(c.Request.Context(), userID, merchantID, role); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}
