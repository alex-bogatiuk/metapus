package cryptohandlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/application/portalapp"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/money_movement_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/portal_repo"
	"metapus/pkg/sdk/appctx"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/cursor"
	"metapus/pkg/sdk/domain"
	"metapus/pkg/sdk/entity"
	"metapus/pkg/sdk/http/dto"
	"metapus/pkg/sdk/http/handlers"
	"metapus/pkg/sdk/http/middleware"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/readaccess"
	"metapus/pkg/sdk/security"
)

// WithdrawalRequestWorkflowHandler exposes withdrawal request document workflow routes.
type WithdrawalRequestWorkflowHandler struct {
	*handlers.BaseHandler
	repo                *portal_repo.DashboardRepo
	relatedDocsHandler  *handlers.RelatedDocumentsHandler
	approvalService     *cryptoapp.WithdrawalApprovalService
	signingJobs         *money_movement_repo.Repo
	stepUpChecker       middleware.StepUpChecker
	rejectionService    *cryptoapp.WithdrawalRejectionService
	valuation           *portalapp.WithdrawalValuationService
	movementProviders   []entity.MovementProvider
	movementRefResolver domain.RefResolver
}

// NewWithdrawalRequestWorkflowHandler creates a new withdrawal request workflow handler.
func NewWithdrawalRequestWorkflowHandler(
	base *handlers.BaseHandler,
	repo *portal_repo.DashboardRepo,
	finder domain.RelatedDocFinder,
	approvalRepo *money_movement_repo.Repo,
	approvalService *cryptoapp.WithdrawalApprovalService,
	stepUpChecker middleware.StepUpChecker,
	rejectionService *cryptoapp.WithdrawalRejectionService,
	valuation *portalapp.WithdrawalValuationService,
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
) *WithdrawalRequestWorkflowHandler {
	h := &WithdrawalRequestWorkflowHandler{
		BaseHandler:         base,
		repo:                repo,
		signingJobs:         approvalRepo,
		stepUpChecker:       stepUpChecker,
		approvalService:     approvalService,
		rejectionService:    rejectionService,
		valuation:           valuation,
		movementProviders:   movementProviders,
		movementRefResolver: movementRefResolver,
	}
	if finder != nil {
		h.relatedDocsHandler = handlers.NewRelatedDocumentsHandler(finder, withdrawal_request.DocumentType)
	}
	return h
}

// RegisterDocumentRoutes mounts the custom document workflow routes.
func (h *WithdrawalRequestWorkflowHandler) RegisterDocumentRoutes(group *gin.RouterGroup, permission string) {
	group.GET("", middleware.RequirePermission(permission+":read"), h.List)
	group.POST("/export-list", middleware.RequirePermission(permission+":read"), h.ExportList)
	group.GET("/:id", middleware.RequirePermission(permission+":read"), h.Get)
	group.GET("/:id/related-documents", middleware.RequirePermission(permission+":read"), h.GetRelatedDocuments)
	group.GET("/:id/movements", middleware.RequirePermission(permission+":read"), h.GetMovements)
	group.POST("/:id/approve", middleware.RequirePermission(permission+":post"), h.requestScopeGuard(), middleware.CommandIdempotency(true), h.Approve)
	group.POST("/:id/reject", middleware.RequirePermission(permission+":post"), h.requestScopeGuard(), middleware.CommandIdempotency(true), h.Reject)
	group.GET("/:id/signing-job", middleware.RequirePermission(permission+":read"), h.GetSigningJob)
	group.POST("/:id/retry-signing", middleware.RequirePermission(permission+":post"), h.requestScopeGuard(),
		middleware.SensitiveActionGuard(h.stepUpChecker, "withdrawal_requests.retry_signing", nil),
		middleware.CommandIdempotency(true), h.RetrySigning)
}

func (h *WithdrawalRequestWorkflowHandler) GetSigningJob(c *gin.Context) {
	requestID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid withdrawal request id"))
		return
	}
	if err := h.checkRequestScope(c.Request.Context(), requestID, false); err != nil {
		h.Error(c, err)
		return
	}
	status, err := h.signingJobs.GetSigningJobStatus(c.Request.Context(), withdrawal_request.DocumentType, requestID)
	if err != nil {
		h.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, cryptodto.SigningJobStatusResponse{
		Status: status.Status, Attempts: status.Attempts, LastError: status.LastError,
	})
}

func (h *WithdrawalRequestWorkflowHandler) RetrySigning(c *gin.Context) {
	requestID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid withdrawal request id"))
		return
	}
	if err := h.signingJobs.ReplaySigningJob(c.Request.Context(), withdrawal_request.DocumentType, requestID, parseContextUserID(c.Request.Context())); err != nil {
		h.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, cryptodto.StatusResponse{Status: "pending"})
}

// Scope precedes idempotency so a replay still checks current merchant access.
func (h *WithdrawalRequestWorkflowHandler) requestScopeGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID, err := id.Parse(c.Param("id"))
		if err != nil {
			h.Error(c, apperror.NewValidation("invalid withdrawal request id"))
			return
		}
		if err := h.checkRequestScope(c.Request.Context(), requestID, true); err != nil {
			h.Error(c, err)
			return
		}
		c.Next()
	}
}

// List handles GET /api/v1/document/withdrawal-request.
func (h *WithdrawalRequestWorkflowHandler) List(c *gin.Context) {
	listFilter, err := h.ParseListFilter(c, "-created_at")
	if err != nil {
		h.Error(c, err)
		return
	}
	status, err := normalizeWithdrawalRequestStatus(c.Query("status"))
	if err != nil {
		h.Error(c, err)
		return
	}
	page, err := h.repo.ListAdminWithdrawalRequestsPage(c.Request.Context(), listFilter, status)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, page)
}

// ExportList uses the same scoped query and filters as the document list.
func (h *WithdrawalRequestWorkflowHandler) ExportList(c *gin.Context) {
	req, filter, err := handlers.ParseExportRequest(c)
	if err != nil {
		h.Error(c, err)
		return
	}
	if filter.OrderBy == "name" {
		filter.OrderBy = "-created_at"
	}
	filter.Limit = 500
	items := make([]any, 0, min(handlers.ExportMaxRows, 500))
	for len(items) < handlers.ExportMaxRows {
		page, err := h.repo.ListAdminWithdrawalRequestsPage(c.Request.Context(), filter, "all")
		if err != nil {
			h.Error(c, err)
			return
		}
		for _, item := range page.Items {
			row, err := handlers.DTOToMap(item)
			if err != nil {
				h.Error(c, apperror.NewInternal(err))
				return
			}
			amount, err := decimal.NewFromString(item.Amount)
			if err != nil || item.DecimalPlaces < 0 || item.DecimalPlaces > 18 {
				h.Error(c, apperror.NewInternal(errors.New("invalid withdrawal request amount")))
				return
			}
			row["amount"] = amount.Shift(-int32(item.DecimalPlaces)).String() + " " + item.Symbol
			items = append(items, row)
			if len(items) == handlers.ExportMaxRows {
				break
			}
		}
		if !page.HasMore || page.NextCursor == "" || len(page.Items) == 0 {
			break
		}
		filter.CursorReq = &cursor.Request{Direction: cursor.DirAfter, Token: page.NextCursor}
	}
	handlers.WriteExportXLSX(c, "WithdrawalRequest", req, items)
}

func normalizeWithdrawalRequestStatus(raw string) (string, error) {
	status := strings.ToLower(strings.TrimSpace(raw))
	if status == "" || status == "all" {
		return "all", nil
	}
	switch status {
	case "active",
		string(withdrawal_request.StatusPendingSignature),
		string(withdrawal_request.StatusPendingApproval),
		string(withdrawal_request.StatusApproved),
		string(withdrawal_request.StatusSigning),
		string(withdrawal_request.StatusBroadcast),
		string(withdrawal_request.StatusConfirmed),
		string(withdrawal_request.StatusHeld),
		string(withdrawal_request.StatusRejected),
		string(withdrawal_request.StatusFailed), string(withdrawal_request.StatusPartiallyCompleted):
		return status, nil
	default:
		return "", apperror.NewValidation("invalid withdrawal request status").WithDetail("status", raw)
	}
}

// GetRelatedDocuments handles GET /api/v1/document/withdrawal-request/:id/related-documents.
func (h *WithdrawalRequestWorkflowHandler) GetRelatedDocuments(c *gin.Context) {
	requestID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid withdrawal request id"))
		return
	}
	if err := h.checkRequestScope(c.Request.Context(), requestID, false); err != nil {
		h.Error(c, err)
		return
	}
	if h.relatedDocsHandler == nil {
		h.Error(c, apperror.NewInternal(errors.New("related documents finder is not configured")))
		return
	}
	h.relatedDocsHandler.GetRelatedDocuments(c)
}

// GetMovements handles GET /api/v1/document/withdrawal-request/:id/movements.
func (h *WithdrawalRequestWorkflowHandler) GetMovements(c *gin.Context) {
	requestID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid withdrawal request id"))
		return
	}
	ctx := c.Request.Context()
	reads, ok := readaccess.FromContext(ctx)
	if !ok {
		h.Error(c, apperror.NewForbidden("authorized reader is unavailable"))
		return
	}
	allMovements, err := reads.ReadMovements(ctx, "withdrawal_request", requestID, postgres.ReadSnapshot,
		func(ctx context.Context, uid id.ID) error {
			return h.checkRequestScope(ctx, uid, false)
		},
		h.movementProviders, h.movementRefResolver)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.DocumentMovementsResponse{Movements: allMovements, Count: len(allMovements)})
}

// Get handles GET /api/v1/document/withdrawal-request/:id.
func (h *WithdrawalRequestWorkflowHandler) Get(c *gin.Context) {
	requestID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid withdrawal request id").WithDetail("id", c.Param("id")))
		return
	}

	item, err := h.repo.GetAdminWithdrawalRequest(c.Request.Context(), requestID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.Error(c, apperror.NewNotFound("withdrawal request", requestID.String()))
			return
		}
		h.Error(c, apperror.NewInternal(err))
		return
	}
	if !security.GetDataScope(c.Request.Context()).CanAccessRecord("withdrawal_request", map[string]string{"merchant": item.MerchantID}) {
		h.Error(c, apperror.NewNotFound("withdrawal request", requestID.String()))
		return
	}
	item.Valuation = h.valuation.Evaluate(c.Request.Context(), item)

	c.JSON(http.StatusOK, item)
}

// Approve handles POST /api/v1/document/withdrawal-request/:id/approve.
func (h *WithdrawalRequestWorkflowHandler) Approve(c *gin.Context) {
	requestID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid withdrawal request id").WithDetail("id", c.Param("id")))
		return
	}
	user := appctx.GetUser(c.Request.Context())
	if user == nil || user.UserID == "" {
		h.Error(c, apperror.NewUnauthorized("user context is required"))
		return
	}

	approvedBy, err := id.Parse(user.UserID)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid user id").WithDetail("userId", user.UserID))
		return
	}

	if err := h.approvalService.Approve(c.Request.Context(), requestID, approvedBy); err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, cryptodto.AdminApproveWithdrawalResponse{
		Status: string(withdrawal_request.StatusApproved),
	})
}

// Reject restores the debit-first reservation before the request reaches execution.
func (h *WithdrawalRequestWorkflowHandler) Reject(c *gin.Context) {
	requestID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid withdrawal request id"))
		return
	}
	user := appctx.GetUser(c.Request.Context())
	if user == nil || user.UserID == "" {
		h.Error(c, apperror.NewUnauthorized("user context is required"))
		return
	}
	actorID, err := id.Parse(user.UserID)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid user id"))
		return
	}
	var body cryptodto.ReasonRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		h.Error(c, apperror.NewValidation("invalid rejection request"))
		return
	}
	if err := h.rejectionService.Reject(c.Request.Context(), requestID, actorID, body.Reason); err != nil {
		h.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, cryptodto.StatusResponse{Status: string(withdrawal_request.StatusRejected)})
}

func (h *WithdrawalRequestWorkflowHandler) checkRequestScope(ctx context.Context, requestID id.ID, mutate bool) error {
	item, err := h.repo.GetAdminWithdrawalRequest(ctx, requestID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NewNotFound("withdrawal request", requestID.String())
		}
		return apperror.NewInternal(err)
	}
	scope := security.GetDataScope(ctx)
	if !scope.CanAccessRecord("withdrawal_request", map[string]string{"merchant": item.MerchantID}) {
		return apperror.NewNotFound("withdrawal request", requestID.String())
	}
	if mutate {
		return scope.CanMutate()
	}
	return nil
}
