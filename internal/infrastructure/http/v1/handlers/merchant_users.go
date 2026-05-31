package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/infrastructure/http/v1/dto"
)

// MerchantUserHandler manages user ↔ merchant associations.
// Routes: /api/v1/merchant-admin/merchants/:merchantId/users
type MerchantUserHandler struct {
	repo        merchant.MerchantUserRepository
	invalidator interface {
		BumpUserAuthVersion(ctx context.Context, userID id.ID, reason string) error
		InvalidateUserAuthCache(ctx context.Context, userID id.ID)
	}
}

// NewMerchantUserHandler creates the handler.
func NewMerchantUserHandler(
	repo merchant.MerchantUserRepository,
	invalidator interface {
		BumpUserAuthVersion(ctx context.Context, userID id.ID, reason string) error
		InvalidateUserAuthCache(ctx context.Context, userID id.ID)
	},
) *MerchantUserHandler {
	return &MerchantUserHandler{repo: repo, invalidator: invalidator}
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

	if err := h.runAccessMutation(c.Request.Context(), userID, "merchant_access_changed", func(ctx context.Context) error {
		return h.repo.Add(ctx, userID, merchantID, role)
	}); err != nil {
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

	if err := h.runAccessMutation(c.Request.Context(), userID, "merchant_access_removed", func(ctx context.Context) error {
		return h.repo.Remove(ctx, userID, merchantID)
	}); err != nil {
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

	if err := h.runAccessMutation(c.Request.Context(), userID, "merchant_role_changed", func(ctx context.Context) error {
		return h.repo.UpdateRole(ctx, userID, merchantID, role)
	}); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *MerchantUserHandler) runAccessMutation(
	ctx context.Context,
	userID id.ID,
	reason string,
	mutate func(context.Context) error,
) error {
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	if err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := mutate(ctx); err != nil {
			return err
		}
		if h.invalidator != nil {
			return h.invalidator.BumpUserAuthVersion(ctx, userID, reason)
		}
		return nil
	}); err != nil {
		return err
	}
	if h.invalidator != nil {
		h.invalidator.InvalidateUserAuthCache(ctx, userID)
	}
	return nil
}
