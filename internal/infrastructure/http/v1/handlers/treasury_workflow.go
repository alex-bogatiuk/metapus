package handlers

import (
	"context"
	"slices"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/treasury_withdrawal"
	"metapus/internal/infrastructure/crypto_worker"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/money_movement_repo"
)

// TreasuryWorkflowHandler exposes treasury withdrawal document workflow operations.
type TreasuryWorkflowHandler struct {
	base            *BaseHandler
	stepUpChecker   middleware.StepUpChecker
	outflows        *cryptoapp.ExternalOutflowService
	adjustments     *cryptoapp.FinancialAdjustmentService
	treasuryService *cryptoapp.TreasuryWorkflowService
	approvals       *cryptoapp.MoneyMovementApprovalService
	signingJobs     *money_movement_repo.Repo
}

// NewTreasuryWorkflowHandler creates a treasury workflow handler.
func NewTreasuryWorkflowHandler(
	base *BaseHandler,
	service domain.DocumentService[*treasury_withdrawal.TreasuryWithdrawal],
	outboundRepo *crypto_worker.OutboundRepo,
	approvalRepo *money_movement_repo.Repo,
	approvalSigners crypto.MoneyMovementApprovalSignerProvider,
	stepUpChecker middleware.StepUpChecker,
	postingEngine cryptoapp.FinancialPostingEngine,
) *TreasuryWorkflowHandler {
	approvals := cryptoapp.NewMoneyMovementApprovalService(approvalRepo, approvalSigners, stepUpChecker)
	requestTx := postgres.NewContextTxManager()
	holds := cryptoapp.NewOperationalHoldService(requestTx, crypto_repo.NewOperationalHoldRepo())
	return &TreasuryWorkflowHandler{
		base:          base,
		stepUpChecker: stepUpChecker,
		outflows:      cryptoapp.NewExternalOutflowService(requestTx, crypto_repo.NewExternalOutflowWorkflowRepo(), holds, postingEngine),
		adjustments:   cryptoapp.NewFinancialAdjustmentService(requestTx, crypto_repo.NewFinancialAdjustmentRepo()),
		approvals:     approvals,
		signingJobs:   approvalRepo,
		treasuryService: cryptoapp.NewTreasuryWorkflowService(
			service, crypto_repo.NewTreasuryWorkflowRepo(), approvalRepo,
			outboundRepo, approvals, crypto_repo.NewChainAddressPolicy(),
		),
	}
}

func (h *TreasuryWorkflowHandler) GetSigningJob(c *gin.Context) {
	withdrawalID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid withdrawal id"))
		return
	}
	status, err := h.signingJobs.GetSigningJobStatus(c.Request.Context(), treasury_withdrawal.DocumentType, withdrawalID)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	c.JSON(200, dto.SigningJobStatusResponse{
		Status: status.Status, Attempts: status.Attempts, LastError: status.LastError,
	})
}

func (h *TreasuryWorkflowHandler) RetrySigning(c *gin.Context) {
	withdrawalID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid withdrawal id"))
		return
	}
	if err := h.signingJobs.ReplaySigningJob(c.Request.Context(), treasury_withdrawal.DocumentType, withdrawalID, parseContextUserID(c.Request.Context())); err != nil {
		h.base.Error(c, err)
		return
	}
	c.JSON(200, dto.StatusResponse{Status: "pending"})
}

func (h *TreasuryWorkflowHandler) requireIdempotency() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader(middleware.HeaderIdempotencyKey) == "" {
			h.base.Error(c, apperror.NewValidation("Idempotency-Key header is required"))
			return
		}
		c.Next()
	}
}

func (h *TreasuryWorkflowHandler) CreateAddress(c *gin.Context) {
	var req dto.CreateTreasuryAddressRequest
	if !h.base.BindJSON(c, &req) {
		return
	}
	networkID, err := id.Parse(req.NetworkID)
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid networkId"))
		return
	}
	addressID, err := h.treasuryService.CreateAddress(c.Request.Context(), networkID, req.Address, req.Label, parseContextUserID(c.Request.Context()))
	if err != nil {
		h.base.Error(c, err)
		return
	}
	h.base.Created(c, addressID.String())
}

func (h *TreasuryWorkflowHandler) ListAddresses(c *gin.Context) {
	addresses, err := h.treasuryService.ListAddresses(c.Request.Context())
	if err != nil {
		h.base.Error(c, err)
		return
	}
	items := make([]dto.TreasuryAddressResponse, 0, len(addresses))
	for _, address := range addresses {
		items = append(items, dto.TreasuryAddressResponse{
			ID: address.ID.String(), NetworkID: address.NetworkID.String(), Address: address.Address,
			Label: address.Label, IsActive: address.IsActive,
		})
	}
	h.base.OK(c, dto.TreasuryAddressListResponse{Items: items})
}

func (h *TreasuryWorkflowHandler) ApproveWithdrawal(c *gin.Context) {
	withdrawalID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid withdrawal id"))
		return
	}
	approverID := parseContextUserID(c.Request.Context())
	if id.IsNil(approverID) {
		h.base.Error(c, apperror.NewValidation("authenticated user id is required"))
		return
	}

	err = h.treasuryService.Approve(c.Request.Context(), withdrawalID, approverID)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	h.base.Success(c, "treasury withdrawal approved")
}

func (h *TreasuryWorkflowHandler) RejectWithdrawal(c *gin.Context) {
	withdrawalID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid withdrawal id"))
		return
	}
	var req dto.ApproveRejectRequest
	_ = c.ShouldBindJSON(&req)
	userID := parseContextUserID(c.Request.Context())

	err = h.treasuryService.Reject(c.Request.Context(), withdrawalID, userID, req.Reason)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	h.base.Success(c, "treasury withdrawal rejected")
}

func (h *TreasuryWorkflowHandler) GetAvailability(c *gin.Context) {
	tokenID, err := id.Parse(c.Query("tokenId"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("tokenId query parameter is required"))
		return
	}
	availability, err := h.treasuryService.GetAvailability(c.Request.Context(), tokenID)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	h.base.OK(c, dto.TreasuryAvailabilityResponse{
		TokenID:   tokenID.String(),
		Amount:    availability.Amount,
		Reserved:  availability.Reserved,
		Available: availability.Available,
	})
}

func (h *TreasuryWorkflowHandler) ListSourceWallets(c *gin.Context) {
	tokenID, err := id.Parse(c.Query("tokenId"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("tokenId query parameter is required"))
		return
	}
	wallets, err := h.treasuryService.ListSourceWallets(c.Request.Context(), tokenID)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	items := make([]dto.TreasurySourceWalletResponse, 0, len(wallets))
	for _, wallet := range wallets {
		item := dto.TreasurySourceWalletResponse{
			ID: wallet.ID.String(), NetworkID: wallet.NetworkID.String(), Name: wallet.Name,
			Address: wallet.Address, LedgerAmount: wallet.LedgerAmount,
			ReservedAmount: wallet.ReservedAmount, AvailableAmount: wallet.AvailableAmount,
			LastChainAmount: wallet.LastChainAmount,
		}
		if wallet.LastReconciliationAt != nil {
			item.LastReconciliationAt = wallet.LastReconciliationAt.Format(time.RFC3339)
		}
		items = append(items, item)
	}
	h.base.OK(c, dto.TreasurySourceWalletListResponse{Items: items})
}

func (h *TreasuryWorkflowHandler) ListExternalOutflows(c *gin.Context) {
	limit := h.base.ParseIntQuery(c, "limit", 100)
	status := c.DefaultQuery("status", "pending_review")
	items, err := h.outflows.List(c.Request.Context(), status, limit)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	response := make([]dto.ExternalOutflowResponse, 0, len(items))
	for index := range items {
		response = append(response, mapExternalOutflowResponse(&items[index]))
	}
	h.base.OK(c, dto.ExternalOutflowListResponse{Items: response})
}

func (h *TreasuryWorkflowHandler) GetExternalOutflow(c *gin.Context) {
	outflowID, err := id.Parse(c.Param("outflowId"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid outflow id"))
		return
	}
	outflow, err := h.outflows.Get(c.Request.Context(), outflowID)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	readiness, err := h.outflows.ResolutionReadiness(c.Request.Context(), outflow, parseContextUserID(c.Request.Context()))
	if err != nil {
		h.base.Error(c, err)
		return
	}
	h.base.OK(c, mapExternalOutflowDetail(outflow, readiness))
}

func (h *TreasuryWorkflowHandler) ClassifyExternalOutflow(c *gin.Context) {
	outflowID, err := id.Parse(c.Param("outflowId"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid outflow id"))
		return
	}
	actorID, _, _, err := h.approvals.RequireStepUp(c.Request.Context(), "crypto.reconciliation.classify:"+outflowID.String())
	if err != nil {
		h.base.Error(c, err)
		return
	}
	var req dto.ClassifyExternalOutflowRequest
	if !h.base.BindJSON(c, &req) {
		return
	}
	var basisID *id.ID
	if req.MatchedBasisID != "" {
		parsed, parseErr := id.Parse(req.MatchedBasisID)
		if parseErr != nil {
			h.base.Error(c, apperror.NewValidation("invalid matchedBasisId"))
			return
		}
		basisID = &parsed
	}
	outflow, err := h.outflows.Classify(c.Request.Context(), cryptoapp.ClassifyExternalOutflowCommand{
		OutflowID: outflowID, Classification: req.Classification, MatchedBasisType: req.MatchedBasisType,
		MatchedBasisID: basisID, Reason: req.Reason, Evidence: req.Evidence,
	}, actorID)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	readiness, err := h.outflows.ResolutionReadiness(c.Request.Context(), outflow, actorID)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	h.base.OK(c, mapExternalOutflowDetail(outflow, readiness))
}

func (h *TreasuryWorkflowHandler) ResolveExternalOutflow(c *gin.Context) {
	outflowID, err := id.Parse(c.Param("outflowId"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid outflow id"))
		return
	}
	actorID, _, _, err := h.approvals.RequireStepUp(c.Request.Context(), "crypto.reconciliation.resolve:"+outflowID.String())
	if err != nil {
		h.base.Error(c, err)
		return
	}
	var req dto.ApproveRejectRequest
	if !h.base.BindJSON(c, &req) {
		return
	}
	outflow, err := h.outflows.Resolve(c.Request.Context(), outflowID, actorID, req.Reason)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	readiness, err := h.outflows.ResolutionReadiness(c.Request.Context(), outflow, actorID)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	h.base.OK(c, mapExternalOutflowDetail(outflow, readiness))
}

func (h *TreasuryWorkflowHandler) ApproveCustodyAdjustment(c *gin.Context) {
	adjustmentID, err := id.Parse(c.Param("adjustmentId"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid custody adjustment id"))
		return
	}
	actorID, _, _, err := h.approvals.RequireStepUp(c.Request.Context(), "crypto.custody_adjustment.approve:"+adjustmentID.String())
	if err != nil {
		h.base.Error(c, err)
		return
	}
	var req dto.ApproveRejectRequest
	if !h.base.BindJSON(c, &req) {
		return
	}
	adjustment, err := h.outflows.ApproveAdjustment(c.Request.Context(), adjustmentID, actorID, req.Reason)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	h.base.OK(c, dto.FromCustodyAdjustment(adjustment))
}

func (h *TreasuryWorkflowHandler) RejectCustodyAdjustment(c *gin.Context) {
	adjustmentID, err := id.Parse(c.Param("adjustmentId"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid custody adjustment id"))
		return
	}
	actorID, _, _, err := h.approvals.RequireStepUp(c.Request.Context(), "crypto.custody_adjustment.reject:"+adjustmentID.String())
	if err != nil {
		h.base.Error(c, err)
		return
	}
	var req dto.ApproveRejectRequest
	if !h.base.BindJSON(c, &req) {
		return
	}
	adjustment, err := h.outflows.RejectAdjustment(c.Request.Context(), adjustmentID, actorID, req.Reason)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	h.base.OK(c, dto.FromCustodyAdjustment(adjustment))
}

func (h *TreasuryWorkflowHandler) ProposeFinancialAdjustment(c *gin.Context) {
	var request dto.ProposeFinancialAdjustmentRequest
	if !h.base.BindJSON(c, &request) {
		return
	}
	var paymentID, walletID, tokenID id.ID
	var err error
	if request.PaymentID != "" {
		paymentID, err = id.Parse(request.PaymentID)
		if err != nil {
			h.base.Error(c, apperror.NewValidation("invalid paymentId"))
			return
		}
	}
	if request.WalletID != "" {
		walletID, err = id.Parse(request.WalletID)
		if err != nil {
			h.base.Error(c, apperror.NewValidation("invalid walletId"))
			return
		}
	}
	if request.TokenID != "" {
		tokenID, err = id.Parse(request.TokenID)
		if err != nil {
			h.base.Error(c, apperror.NewValidation("invalid tokenId"))
			return
		}
	}
	selections := make([]cryptoapp.FinancialAdjustmentSourceSelection, 0, len(request.SourceLines))
	for _, line := range request.SourceLines {
		lineID, parseErr := id.Parse(line.LineID)
		if parseErr != nil {
			h.base.Error(c, apperror.NewValidation("invalid source lineId"))
			return
		}
		selections = append(selections, cryptoapp.FinancialAdjustmentSourceSelection{
			Register: line.Register, LineID: lineID, Amount: line.Amount,
		})
	}
	subject := request.PaymentID
	if request.Mode == "wallet_balance" {
		subject = request.WalletID + ":" + request.TokenID
	}
	makerID, _, _, err := h.approvals.RequireStepUp(c.Request.Context(),
		"crypto.financial_adjustment.propose:"+subject)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	adjustment, err := h.adjustments.Propose(c.Request.Context(), cryptoapp.ProposeFinancialAdjustmentCommand{
		Mode: request.Mode, PaymentID: paymentID, PostedVersion: request.ExpectedPostedVersion,
		WalletID: walletID, TokenID: tokenID,
		Direction: request.Direction, Amount: request.Amount, SourceLines: selections,
		Direct: appctx.IsSystemAdmin(appctx.GetUser(c.Request.Context())) ||
			slices.Contains(appctx.GetUser(c.Request.Context()).Permissions, "crypto_adjustment:post_direct"),
		ReasonCode: request.ReasonCode, Evidence: request.Evidence,
		IdempotencyKey: c.GetHeader(middleware.HeaderIdempotencyKey), MakerID: makerID,
	})
	if err != nil {
		h.base.Error(c, err)
		return
	}
	h.base.OK(c, dto.FromFinancialAdjustment(adjustment))
}

func (h *TreasuryWorkflowHandler) FinancialAdjustmentPaymentSources(c *gin.Context) {
	paymentID, err := id.Parse(c.Param("paymentId"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid paymentId"))
		return
	}
	sources, err := h.adjustments.PaymentSources(c.Request.Context(), paymentID)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	response := dto.FinancialAdjustmentPaymentSourcesResponse{
		PaymentID: paymentID.String(), PostedVersion: sources.PostedVersion,
		Lines: make([]dto.FinancialAdjustmentSourceResponse, 0, len(sources.Lines)),
	}
	for _, line := range sources.Lines {
		response.Lines = append(response.Lines, dto.FinancialAdjustmentSourceResponse{
			Register: line.Register, LineID: line.LineID.String(), Amount: line.Amount,
			Reversed: line.Reversed, Available: line.Available,
			TokenID: line.TokenID.String(), Dimension: line.Dimension,
		})
	}
	h.base.OK(c, response)
}

func (h *TreasuryWorkflowHandler) ListFinancialAdjustments(c *gin.Context) {
	limit := h.base.ParseIntQuery(c, "limit", 50)
	var items []cryptoapp.FinancialAdjustment
	var err error
	if c.Query("paymentId") != "" {
		paymentID, parseErr := id.Parse(c.Query("paymentId"))
		if parseErr != nil {
			h.base.Error(c, apperror.NewValidation("invalid paymentId"))
			return
		}
		items, err = h.adjustments.ListForPayment(c.Request.Context(), paymentID, c.Query("status"), limit)
	} else {
		items, err = h.adjustments.List(c.Request.Context(), c.Query("status"), limit)
	}
	if err != nil {
		h.base.Error(c, err)
		return
	}
	response := make([]dto.FinancialAdjustmentResponse, 0, len(items))
	for index := range items {
		response = append(response, dto.FromFinancialAdjustment(&items[index]))
	}
	h.base.OK(c, dto.FinancialAdjustmentListResponse{Items: response})
}

func (h *TreasuryWorkflowHandler) GetFinancialAdjustment(c *gin.Context) {
	adjustmentID, err := id.Parse(c.Param("adjustmentId"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid adjustment id"))
		return
	}
	adjustment, err := h.adjustments.Get(c.Request.Context(), adjustmentID)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	response := dto.FromFinancialAdjustment(adjustment)
	if adjustment.Mode == "payment_reversal" {
		lines, err := h.adjustments.Sources(c.Request.Context(), adjustmentID)
		if err != nil {
			h.base.Error(c, err)
			return
		}
		response.SourceLines = make([]dto.FinancialAdjustmentSourceRequest, 0, len(lines))
		for _, line := range lines {
			response.SourceLines = append(response.SourceLines, dto.FinancialAdjustmentSourceRequest{
				Register: line.Register, LineID: line.LineID.String(), Amount: line.Amount,
			})
		}
	}
	h.base.OK(c, response)
}

func (h *TreasuryWorkflowHandler) ApproveFinancialAdjustment(c *gin.Context) {
	h.decideFinancialAdjustment(c, true)
}

func (h *TreasuryWorkflowHandler) RejectFinancialAdjustment(c *gin.Context) {
	h.decideFinancialAdjustment(c, false)
}

func (h *TreasuryWorkflowHandler) decideFinancialAdjustment(c *gin.Context, approve bool) {
	adjustmentID, err := id.Parse(c.Param("adjustmentId"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid adjustment id"))
		return
	}
	action := "reject"
	if approve {
		action = "approve"
	}
	checkerID, _, _, err := h.approvals.RequireStepUp(c.Request.Context(),
		"crypto.financial_adjustment."+action+":"+adjustmentID.String())
	if err != nil {
		h.base.Error(c, err)
		return
	}
	var request dto.ApproveRejectRequest
	if !h.base.BindJSON(c, &request) {
		return
	}
	var adjustment *cryptoapp.FinancialAdjustment
	if approve {
		adjustment, err = h.adjustments.Approve(c.Request.Context(), adjustmentID, checkerID, request.Reason)
	} else {
		adjustment, err = h.adjustments.Reject(c.Request.Context(), adjustmentID, checkerID, request.Reason)
	}
	if err != nil {
		h.base.Error(c, err)
		return
	}
	h.base.OK(c, dto.FromFinancialAdjustment(adjustment))
}

func mapExternalOutflowResponse(outflow *crypto.ExternalOutflow) dto.ExternalOutflowResponse {
	item := dto.ExternalOutflowResponse{
		ID: outflow.ID.String(), NetworkID: outflow.NetworkID.String(), TxHash: outflow.TxHash,
		EventKind: string(outflow.EventKind), EventIndex: outflow.EventIndex, FromAddress: outflow.FromAddress,
		ToAddress: outflow.ToAddress, ContractAddress: outflow.ContractAddress, Amount: outflow.Amount,
		BlockNumber: outflow.BlockNumber, BlockHash: outflow.BlockHash, Confirmations: outflow.Confirmations,
		RequiredConfs: outflow.RequiredConfirmations,
		Status:        string(outflow.Status), FinalityStatus: outflow.FinalityStatus,
		Classification: string(outflow.Classification), DetectedAt: outflow.DetectedAt.Format(time.RFC3339),
	}
	if outflow.TokenID != nil {
		item.TokenID = outflow.TokenID.String()
	}
	if outflow.SourceWalletID != nil {
		item.SourceWalletID = outflow.SourceWalletID.String()
	}
	return item
}

func mapExternalOutflowDetail(outflow *crypto.ExternalOutflow, readiness cryptoapp.ExternalOutflowResolutionReadiness) dto.ExternalOutflowDetailResponse {
	actions := make([]dto.ExternalOutflowActionResponse, 0, len(outflow.Actions))
	for _, action := range outflow.Actions {
		actions = append(actions, dto.ExternalOutflowActionResponse{
			ActorType: action.ActorType, ActorID: action.ActorID, Action: action.Action,
			OldState: action.OldState, NewState: action.NewState, Reason: action.Reason,
			Evidence: action.Evidence, CreatedAt: action.CreatedAt.Format(time.RFC3339),
		})
	}
	holds := make([]dto.OperationalHoldResponse, 0, len(outflow.OpenHolds))
	for _, hold := range outflow.OpenHolds {
		holds = append(holds, dto.OperationalHoldResponse{
			Scope: hold.Scope, Reason: hold.Reason, OwnerType: hold.OwnerType,
			OwnerID: hold.OwnerID, HeldAt: hold.HeldAt.Format(time.RFC3339),
		})
	}
	adjustment := dto.FromCustodyAdjustment(outflow.Adjustment)
	var matchedBasisID string
	if outflow.MatchedBasisID != nil {
		matchedBasisID = outflow.MatchedBasisID.String()
	}
	return dto.ExternalOutflowDetailResponse{Outflow: mapExternalOutflowResponse(outflow), ClassificationReason: outflow.ClassificationReason,
		ClassificationEvidence: outflow.ClassificationEvidence, MatchedBasisType: outflow.MatchedBasisType,
		MatchedBasisID: matchedBasisID, Adjustment: adjustment, OpenHolds: holds, Actions: actions,
		Resolution: outflow.Resolution, CanResolve: readiness.CanResolve, ResolutionBlockers: readiness.Blockers}
}

func parseContextUserID(ctx context.Context) id.ID {
	user := appctx.GetUser(ctx)
	if user == nil || user.UserID == "" {
		return id.Nil()
	}
	parsed, err := id.Parse(user.UserID)
	if err != nil {
		return id.Nil()
	}
	return parsed
}
