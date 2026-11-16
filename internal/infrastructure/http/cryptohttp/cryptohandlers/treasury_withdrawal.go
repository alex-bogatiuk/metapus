package cryptohandlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/treasury_withdrawal"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/crypto_worker"
	"metapus/internal/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/money_movement_repo"
	"metapus/internal/infrastructure/tenantdb"
)

// TreasuryWithdrawalHandler handles HTTP requests for TreasuryWithdrawal documents.
type TreasuryWithdrawalHandler struct {
	*handlers.BaseDocumentHandler[*treasury_withdrawal.TreasuryWithdrawal, cryptodto.CreateTreasuryWithdrawalRequest, cryptodto.UpdateTreasuryWithdrawalRequest]
	relatedDocsHandler *handlers.RelatedDocumentsHandler
	workflowHandler    *TreasuryWorkflowHandler
	stepUpGuard        func(actionCode string) gin.HandlerFunc
}

func (h *TreasuryWithdrawalHandler) RequiresCommandIdempotency() bool { return true }

func resolveTreasuryWithdrawalRefs(ctx context.Context, docs ...*treasury_withdrawal.TreasuryWithdrawal) (any, error) {
	resolver := postgres.NewReferenceResolver()
	for _, doc := range docs {
		cryptodto.CollectTreasuryWithdrawalRefs(resolver, doc)
	}
	pool := tenantdb.MustGetPool(ctx)
	return resolver.Resolve(ctx, pool)
}

// NewTreasuryWithdrawalHandler creates a treasury withdrawal handler.
func NewTreasuryWithdrawalHandler(
	base *handlers.BaseHandler,
	service domain.DocumentService[*treasury_withdrawal.TreasuryWithdrawal],
	relatedDocFinder domain.RelatedDocFinder,
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
	settingsRepo settings.Repository,
	approvalRepo *money_movement_repo.Repo,
	approvalSigners crypto.MoneyMovementApprovalSignerProvider,
	stepUpChecker middleware.StepUpChecker,
	postingEngine *posting.Engine,
	stepUpGuard ...func(actionCode string) gin.HandlerFunc,
) *TreasuryWithdrawalHandler {
	cfg := handlers.BaseDocumentHandlerConfig[*treasury_withdrawal.TreasuryWithdrawal, cryptodto.CreateTreasuryWithdrawalRequest, cryptodto.UpdateTreasuryWithdrawalRequest]{
		Service:    service,
		EntityName: "treasury_withdrawal",
		MapCreateDTO: func(req cryptodto.CreateTreasuryWithdrawalRequest) (*treasury_withdrawal.TreasuryWithdrawal, error) {
			return req.ToEntity(), nil
		},
		MapUpdateDTO: func(req cryptodto.UpdateTreasuryWithdrawalRequest, existing *treasury_withdrawal.TreasuryWithdrawal) (*treasury_withdrawal.TreasuryWithdrawal, error) {
			req.ApplyTo(existing)
			return existing, nil
		},
		MapToDTO: func(entity *treasury_withdrawal.TreasuryWithdrawal) any {
			return cryptodto.FromTreasuryWithdrawal(entity)
		},
		ResolveRefs: resolveTreasuryWithdrawalRefs,
		MapToDTOWithRefs: func(entity *treasury_withdrawal.TreasuryWithdrawal, refs any) any {
			resolvedRefs, _ := refs.(postgres.ResolvedRefs)
			return cryptodto.FromTreasuryWithdrawal(entity, resolvedRefs)
		},
		MovementProviders:   movementProviders,
		MovementRefResolver: movementRefResolver,
		SettingsRepo:        settingsRepo,
	}

	h := &TreasuryWithdrawalHandler{BaseDocumentHandler: handlers.NewBaseDocumentHandler(base, cfg)}
	if len(stepUpGuard) > 0 {
		h.stepUpGuard = stepUpGuard[0]
	}
	h.workflowHandler = NewTreasuryWorkflowHandler(base, service, crypto_worker.NewOutboundRepo(), approvalRepo, approvalSigners, stepUpChecker, postingEngine)
	if relatedDocFinder != nil {
		h.relatedDocsHandler = handlers.NewRelatedDocumentsHandler(relatedDocFinder, treasury_withdrawal.DocumentType)
	}
	return h
}

func (h *TreasuryWithdrawalHandler) RegisterCustomDocumentRoutes(group *gin.RouterGroup, permission string) {
	requireStepUp := func(actionCode string) gin.HandlerFunc {
		if h.stepUpGuard == nil {
			return func(c *gin.Context) { c.Next() }
		}
		return h.stepUpGuard(actionCode)
	}
	group.GET("/addresses", middleware.RequirePermission(permission+":read"), h.workflowHandler.ListAddresses)
	group.POST("/addresses", middleware.RequirePermission(permission+":create"), requireStepUp("treasury.addresses.write"), h.workflowHandler.requireIdempotency(), h.workflowHandler.CreateAddress)
	group.GET("/availability", middleware.RequirePermission(permission+":read"), h.workflowHandler.GetAvailability)
	group.GET("/source-wallets", middleware.RequirePermission(permission+":read"), h.workflowHandler.ListSourceWallets)
	group.POST("/receipt-refunds", middleware.RequirePermission(permission+":create"), requireStepUp("treasury.withdrawals.create"),
		h.workflowHandler.requireIdempotency(), h.CreateReceiptRefund)
	group.GET("/reconciliation/outflows", middleware.RequirePermission("crypto_reconciliation:read"), h.workflowHandler.ListExternalOutflows)
	group.GET("/reconciliation/outflows/:outflowId", middleware.RequirePermission("crypto_reconciliation:read"), h.workflowHandler.GetExternalOutflow)
	group.POST("/reconciliation/outflows/:outflowId/classify", middleware.RequirePermission("crypto_reconciliation:classify"), h.workflowHandler.ClassifyExternalOutflow)
	group.POST("/reconciliation/outflows/:outflowId/resolve", middleware.RequirePermission("crypto_reconciliation:resolve"), h.workflowHandler.ResolveExternalOutflow)
	group.POST("/reconciliation/custody-adjustments/:adjustmentId/approve", middleware.RequirePermission("crypto_custody_adjustment:approve"), h.workflowHandler.ApproveCustodyAdjustment)
	group.POST("/reconciliation/custody-adjustments/:adjustmentId/reject", middleware.RequirePermission("crypto_custody_adjustment:approve"), h.workflowHandler.RejectCustodyAdjustment)
	group.POST("/reconciliation/financial-adjustments", middleware.RequirePermission("crypto_reconciliation:classify"),
		h.workflowHandler.requireIdempotency(), h.workflowHandler.ProposeFinancialAdjustment)
	group.GET("/reconciliation/financial-adjustments", middleware.RequirePermission("crypto_reconciliation:read"),
		h.workflowHandler.ListFinancialAdjustments)
	group.GET("/reconciliation/financial-adjustments/payment/:paymentId/sources", middleware.RequirePermission("crypto_reconciliation:read"),
		h.workflowHandler.FinancialAdjustmentPaymentSources)
	group.GET("/reconciliation/financial-adjustments/:adjustmentId", middleware.RequirePermission("crypto_reconciliation:read"),
		h.workflowHandler.GetFinancialAdjustment)
	group.POST("/reconciliation/financial-adjustments/:adjustmentId/approve", middleware.RequirePermission("crypto_reconciliation:resolve"),
		h.workflowHandler.requireIdempotency(), h.workflowHandler.ApproveFinancialAdjustment)
	group.POST("/reconciliation/financial-adjustments/:adjustmentId/reject", middleware.RequirePermission("crypto_reconciliation:resolve"),
		h.workflowHandler.requireIdempotency(), h.workflowHandler.RejectFinancialAdjustment)
	group.POST("/:id/approve", middleware.RequirePermission(permission+":post"), h.workflowHandler.requireIdempotency(), h.workflowHandler.ApproveWithdrawal)
	group.POST("/:id/reject", middleware.RequirePermission(permission+":post"), requireStepUp("treasury.withdrawals.reject"), h.workflowHandler.requireIdempotency(), h.workflowHandler.RejectWithdrawal)
	group.GET("/:id/signing-job", middleware.RequirePermission(permission+":read"), h.workflowHandler.GetSigningJob)
	group.POST("/:id/retry-signing", middleware.RequirePermission(permission+":post"),
		requireStepUp("treasury.withdrawals.retry_signing"), h.workflowHandler.requireIdempotency(), h.workflowHandler.RetrySigning)
}

// Create handles POST /document/treasury-withdrawal with the same operational
// validation as the treasury workflow endpoint.
func (h *TreasuryWithdrawalHandler) Create(c *gin.Context) {
	middleware.SensitiveActionGuard(h.workflowHandler.stepUpChecker, "treasury.withdrawals.create", nil)(c)
	if c.IsAborted() {
		return
	}
	if c.GetHeader(middleware.HeaderIdempotencyKey) == "" {
		h.Error(c, apperror.NewValidation("Idempotency-Key header is required"))
		return
	}

	var req cryptodto.CreateTreasuryWithdrawalRequest
	if !h.BindJSON(c, &req) {
		return
	}
	doc := req.ToEntity()
	userID := parseContextUserID(c.Request.Context())
	if !id.IsNil(userID) {
		doc.CreatedBy = userID
		doc.UpdatedBy = userID
	}
	if err := h.workflowHandler.treasuryService.Create(c.Request.Context(), doc, userID); err != nil {
		h.Error(c, err)
		return
	}

	var refs any
	refs, _ = h.ResolveRefs(c.Request.Context(), doc)
	h.ApplyFLSRead(c, doc)
	response := h.ToDTO(doc, refs)
	h.CompleteIdempotency(c, http.StatusAccepted, "application/json", response)
	c.JSON(http.StatusAccepted, response)
}

// CreateReceiptRefund handles POST /document/treasury-withdrawal/receipt-refunds:
// it prepares the return of an unidentified receipt; another operator approves it.
func (h *TreasuryWithdrawalHandler) CreateReceiptRefund(c *gin.Context) {
	var req cryptodto.CreateReceiptRefundRequest
	if !h.BindJSON(c, &req) {
		return
	}
	receiptID, receiptErr := id.Parse(req.ReceiptID)
	walletID, walletErr := id.Parse(req.SourceWalletID)
	if receiptErr != nil || walletErr != nil {
		h.Error(c, apperror.NewValidation("invalid receiptId or sourceWalletId"))
		return
	}
	withheld := req.WithheldAmount
	if withheld.IsZero() {
		withheld = types.ZeroRawAmount()
	}
	userID := parseContextUserID(c.Request.Context())
	doc, err := h.workflowHandler.treasuryService.CreateReceiptRefund(c.Request.Context(), cryptoapp.CreateReceiptRefundInput{
		ReceiptID: receiptID, SourceWalletID: walletID, DestAddress: req.DestAddress, Evidence: req.Evidence, Reason: req.Reason,
		Total: req.Amount, GasPayer: treasury_withdrawal.GasPayer(req.GasPayer), Withheld: withheld,
	}, userID)
	if err != nil {
		h.Error(c, err)
		return
	}
	var refs any
	refs, _ = h.ResolveRefs(c.Request.Context(), doc)
	h.ApplyFLSRead(c, doc)
	response := h.ToDTO(doc, refs)
	h.CompleteIdempotency(c, http.StatusAccepted, "application/json", response)
	c.JSON(http.StatusAccepted, response)
}

// Unpost is intentionally blocked for treasury withdrawals because posted
// movements mirror an on-chain transaction and must not be manually reversed.
func (h *TreasuryWithdrawalHandler) Unpost(c *gin.Context) {
	h.Error(c, apperror.NewValidation("treasury withdrawals cannot be manually unposted"))
}

func (h *TreasuryWithdrawalHandler) GetRelatedDocuments(c *gin.Context) {
	if h.relatedDocsHandler == nil {
		c.JSON(http.StatusOK, gin.H{"groups": []any{}})
		return
	}
	h.relatedDocsHandler.GetRelatedDocuments(c)
}
