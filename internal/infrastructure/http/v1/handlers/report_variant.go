package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/reports/variants"
	"metapus/internal/infrastructure/http/v1/dto"
)

type ReportVariantHandler struct {
	*BaseHandler
	service *variants.Service
}

func NewReportVariantHandler(base *BaseHandler, service *variants.Service) *ReportVariantHandler {
	return &ReportVariantHandler{
		BaseHandler: base,
		service:     service,
	}
}

func (h *ReportVariantHandler) GetList(datasetKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		list, err := h.service.GetList(ctx, datasetKey)
		if err != nil {
			c.Error(err)
			c.Abort()
			return
		}

		c.JSON(http.StatusOK, dto.MapVariantListResponse(list))
	}
}

func (h *ReportVariantHandler) Create(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.CreateVariantRequest
	if !h.BindJSON(c, &req) {
		return
	}

	variant := &variants.ReportVariant{
		ID:         uuid.New(),
		DatasetKey: req.DatasetKey,
		Name:       req.Name,
		Visibility: req.Visibility,
		IsDefault:  req.IsDefault,
		Config:     req.Config,
	}

	if err := h.service.Create(ctx, variant); err != nil {
		c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, dto.MapVariantResponse(variant))
}

func (h *ReportVariantHandler) Update(c *gin.Context) {
	ctx := c.Request.Context()
	idParam := c.Param("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid UUID"))
		return
	}

	var req dto.UpdateVariantRequest
	if !h.BindJSON(c, &req) {
		return
	}

	variant := &variants.ReportVariant{
		ID:         id,
		Name:       req.Name,
		Visibility: req.Visibility,
		IsDefault:  req.IsDefault,
		Config:     req.Config,
		Version:    req.Version,
	}

	if err := h.service.Update(ctx, variant); err != nil {
		c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *ReportVariantHandler) Delete(c *gin.Context) {
	ctx := c.Request.Context()
	idParam := c.Param("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid UUID"))
		return
	}

	if err := h.service.Delete(ctx, id); err != nil {
		c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
