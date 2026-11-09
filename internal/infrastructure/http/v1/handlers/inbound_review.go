package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

type InboundReviewHandler struct {
	repo *crypto_repo.InboundReviewRepo
}

func NewInboundReviewHandler(repo *crypto_repo.InboundReviewRepo) *InboundReviewHandler {
	return &InboundReviewHandler{repo: repo}
}

func (h *InboundReviewHandler) RegisterRoutes(rg *gin.RouterGroup) {
	group := rg.Group("/crypto/inbound-reviews")
	group.GET("", middleware.RequirePermission("crypto_inbound_review:read"), h.List)
	group.POST("/:id/resolve", middleware.RequirePermission("crypto_inbound_review:resolve"), h.Resolve)
}

func (h *InboundReviewHandler) List(c *gin.Context) {
	status := c.DefaultQuery("status", "needs_review")
	if status != "needs_review" && status != "resolved" {
		_ = c.Error(apperror.NewValidation("invalid review status"))
		c.Abort()
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || limit < 1 || limit > 100 {
		_ = c.Error(apperror.NewValidation("limit must be between 1 and 100"))
		c.Abort()
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		_ = c.Error(apperror.NewValidation("offset must be non-negative"))
		c.Abort()
		return
	}
	result, err := h.repo.List(c.Request.Context(), status, limit, offset)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	response := dto.InboundReviewListResponse{Items: make([]dto.InboundReviewItem, 0, len(result.Items)), Total: result.Total}
	for _, item := range result.Items {
		response.Items = append(response.Items, dto.InboundReviewItem{
			ID: item.ID, InvoiceID: item.InvoiceID, MerchantID: item.MerchantID,
			NetworkID: item.NetworkID, TokenID: item.TokenID, TxHash: item.TxHash,
			Amount: item.Amount, FromAddress: item.FromAddress, ToAddress: item.ToAddress,
			InvoiceStatus: item.InvoiceStatus, Status: item.Status,
			ReviewOutcome: item.ReviewOutcome, ReviewReference: item.ReviewReference,
			ReviewNote: item.ReviewNote, ReviewedBy: item.ReviewedBy,
			CreatedAt: item.CreatedAt, ReviewedAt: item.ReviewedAt, CanResolve: item.CanResolve,
			MatchReason: item.MatchReason, CustomerID: item.CustomerID,
		})
	}
	c.JSON(http.StatusOK, response)
}

func (h *InboundReviewHandler) Resolve(c *gin.Context) {
	eventID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid inbound event id"))
		c.Abort()
		return
	}
	var req dto.ResolveInboundReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}
	if req.Outcome != "credited_manually" && req.Outcome != "refunded_manually" && req.Outcome != "not_attributable" {
		_ = c.Error(apperror.NewValidation("invalid review outcome"))
		c.Abort()
		return
	}
	reference := strings.TrimSpace(req.Reference)
	note := strings.TrimSpace(req.Note)
	if note == "" || len(note) > 2000 || len(reference) > 500 ||
		(req.Outcome != "not_attributable" && reference == "") {
		_ = c.Error(apperror.NewValidation("review note and external reference are required"))
		c.Abort()
		return
	}
	if reference != "" {
		parsed, parseErr := url.ParseRequestURI(reference)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
			_ = c.Error(apperror.NewValidation("review reference must be an HTTPS URL"))
			c.Abort()
			return
		}
	}
	user := appctx.GetUser(c.Request.Context())
	if user == nil {
		_ = c.Error(apperror.NewUnauthorized("authentication required"))
		c.Abort()
		return
	}
	actorID, err := id.Parse(user.UserID)
	if err != nil {
		_ = c.Error(apperror.NewUnauthorized("invalid user context"))
		c.Abort()
		return
	}
	resolved, err := h.repo.Resolve(c.Request.Context(), eventID, actorID, req.Outcome, reference, note)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	if !resolved {
		_ = c.Error(apperror.NewConflict("review is already resolved or transfer is not eligible for this resolution"))
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, dto.ResolveInboundReviewResponse{Resolved: true})
}
