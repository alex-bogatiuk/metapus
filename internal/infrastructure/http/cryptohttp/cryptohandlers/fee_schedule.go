package cryptohandlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/http/cryptohttp/cryptodto"
)

// FeeScheduleHandler handles HTTP requests for fee schedule management.
// Supports two scopes:
//   - Merchant-scoped: /merchant-admin/merchants/:merchantId/fee-schedule
//   - Global:          /api/v1/system/fee-schedule
type FeeScheduleHandler struct {
	repo crypto.FeeScheduleRepository
}

// NewFeeScheduleHandler creates a new fee schedule handler.
func NewFeeScheduleHandler(repo crypto.FeeScheduleRepository) *FeeScheduleHandler {
	return &FeeScheduleHandler{repo: repo}
}

// ListByMerchant handles GET /merchant-admin/merchants/:merchantId/fee-schedule.
func (h *FeeScheduleHandler) ListByMerchant(c *gin.Context) {
	merchantID, err := id.Parse(c.Param("merchantId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid merchant id"))
		c.Abort()
		return
	}

	schedules, err := h.repo.ListByMerchant(c.Request.Context(), merchantID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, cryptodto.FeeScheduleListResponse{Items: cryptodto.FromFeeScheduleList(schedules), Total: len(schedules)})
}

// ListGlobal handles GET /system/fee-schedule.
func (h *FeeScheduleHandler) ListGlobal(c *gin.Context) {
	schedules, err := h.repo.ListGlobal(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, cryptodto.FeeScheduleListResponse{Items: cryptodto.FromFeeScheduleList(schedules), Total: len(schedules)})
}

// UpsertMerchant handles PUT /merchant-admin/merchants/:merchantId/fee-schedule.
func (h *FeeScheduleHandler) UpsertMerchant(c *gin.Context) {
	merchantID, err := id.Parse(c.Param("merchantId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid merchant id"))
		c.Abort()
		return
	}

	var req cryptodto.FeeScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	schedule, err := h.toSchedule(&req, &merchantID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if err := h.repo.Upsert(c.Request.Context(), schedule); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

// UpsertGlobal handles PUT /system/fee-schedule.
func (h *FeeScheduleHandler) UpsertGlobal(c *gin.Context) {
	var req cryptodto.FeeScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	schedule, err := h.toSchedule(&req, nil)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	if err := h.repo.Upsert(c.Request.Context(), schedule); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

// DeleteMerchant handles DELETE /merchant-admin/merchants/:merchantId/fee-schedule.
func (h *FeeScheduleHandler) DeleteMerchant(c *gin.Context) {
	merchantID, err := id.Parse(c.Param("merchantId"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid merchant id"))
		c.Abort()
		return
	}

	var req cryptodto.FeeScheduleDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	tokenID, err := id.Parse(req.TokenID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid token id").WithDetail("field", "tokenId"))
		c.Abort()
		return
	}

	direction := crypto.FeeDirection(req.Direction)
	if !crypto.IsValidFeeDirection(direction) {
		_ = c.Error(apperror.NewValidation("invalid direction").WithDetail("field", "direction"))
		c.Abort()
		return
	}

	if err := h.repo.Delete(c.Request.Context(), &merchantID, tokenID, direction); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

// DeleteGlobal handles DELETE /system/fee-schedule.
func (h *FeeScheduleHandler) DeleteGlobal(c *gin.Context) {
	var req cryptodto.FeeScheduleDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	tokenID, err := id.Parse(req.TokenID)
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid token id").WithDetail("field", "tokenId"))
		c.Abort()
		return
	}

	direction := crypto.FeeDirection(req.Direction)
	if !crypto.IsValidFeeDirection(direction) {
		_ = c.Error(apperror.NewValidation("invalid direction").WithDetail("field", "direction"))
		c.Abort()
		return
	}

	if err := h.repo.Delete(c.Request.Context(), nil, tokenID, direction); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

// toSchedule validates and converts a request DTO to a domain FeeSchedule.
func (h *FeeScheduleHandler) toSchedule(req *cryptodto.FeeScheduleRequest, merchantID *id.ID) (*crypto.FeeSchedule, error) {
	tokenID, err := id.Parse(req.TokenID)
	if err != nil {
		return nil, apperror.NewValidation("invalid token id").WithDetail("field", "tokenId")
	}

	direction := crypto.FeeDirection(req.Direction)
	if !crypto.IsValidFeeDirection(direction) {
		return nil, apperror.NewValidation("invalid direction").WithDetail("field", "direction")
	}

	if req.PercentBP < 0 || req.PercentBP > 10000 {
		return nil, apperror.NewValidation("percentBp must be between 0 and 10000").WithDetail("field", "percentBp")
	}

	return &crypto.FeeSchedule{
		MerchantID: merchantID,
		TokenID:    tokenID,
		Direction:  direction,
		FixedFee:   req.FixedFee,
		PercentBP:  req.PercentBP,
		MinFee:     req.MinFee,
		MaxFee:     req.MaxFee,
	}, nil
}
