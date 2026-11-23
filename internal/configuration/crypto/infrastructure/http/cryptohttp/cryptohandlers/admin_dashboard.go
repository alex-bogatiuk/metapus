package cryptohandlers

import (
	"net/http"
	"strconv"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/dashboardapp"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/handlers"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/middleware"
	"github.com/gin-gonic/gin"
)

type AdminDashboardHandler struct {
	*handlers.BaseHandler
	svc *dashboardapp.Service
}

func NewAdminDashboardHandler(svc *dashboardapp.Service) *AdminDashboardHandler {
	return &AdminDashboardHandler{BaseHandler: handlers.NewBaseHandler(), svc: svc}
}

func (h *AdminDashboardHandler) RegisterRoutes(rg *gin.RouterGroup) {
	// These are tenant-wide projections; granular document permissions alone
	// cannot authorize bypassing a user's row or field scope.
	group := rg.Group("/admin/dashboard", middleware.RequireSystemAdmin())
	group.GET("/queues", h.Queues)
	group.GET("/finance", h.Finance)
	group.GET("/analytics", h.Analytics)
}

func (h *AdminDashboardHandler) Queues(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "5"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid queue limit"))
		return
	}
	result, err := h.svc.Queues(c.Request.Context(), c.DefaultQuery("kind", "all"), limit)
	if err != nil {
		h.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *AdminDashboardHandler) Finance(c *gin.Context) {
	result, err := h.svc.Finance(c.Request.Context())
	if err != nil {
		h.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *AdminDashboardHandler) Analytics(c *gin.Context) {
	result, err := h.svc.Analytics(c.Request.Context(), c.DefaultQuery("period", "30d"))
	if err != nil {
		h.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
