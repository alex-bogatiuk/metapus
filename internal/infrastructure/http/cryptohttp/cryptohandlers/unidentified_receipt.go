package cryptohandlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/command"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
)

// UnidentifiedReceiptHandler serves the journal of unidentified custody
// receipts and the commands that credit them to merchants or take them as
// platform funding.
type UnidentifiedReceiptHandler struct {
	base     *handlers.BaseHandler
	journal  *cryptoapp.ReceiptJournal
	commands *cryptoapp.MerchantCreditCommands
	fundings *cryptoapp.PlatformFundingCommands
}

func NewUnidentifiedReceiptHandler(base *handlers.BaseHandler, journal *cryptoapp.ReceiptJournal,
	commands *cryptoapp.MerchantCreditCommands, fundings *cryptoapp.PlatformFundingCommands) *UnidentifiedReceiptHandler {
	if journal == nil || commands == nil || fundings == nil {
		panic("unidentified receipts require the journal and the attribution commands")
	}
	return &UnidentifiedReceiptHandler{base: base, journal: journal, commands: commands, fundings: fundings}
}

// page reads limit and offset; their range is the journal's rule.
func (h *UnidentifiedReceiptHandler) page(c *gin.Context) (limit, offset int, ok bool) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("limit must be a number"))
		return 0, 0, false
	}
	if offset, err = strconv.Atoi(c.DefaultQuery("offset", "0")); err != nil {
		h.base.Error(c, apperror.NewValidation("offset must be a number"))
		return 0, 0, false
	}
	return limit, offset, true
}

// RegisterRoutes mounts the journal. stepUp guards every money command.
func (h *UnidentifiedReceiptHandler) RegisterRoutes(rg *gin.RouterGroup, stepUp gin.HandlerFunc) {
	receipts := rg.Group("/crypto/unidentified-receipts")
	receipts.GET("", middleware.RequirePermission("crypto_receipt:read"), h.List)
	receipts.GET("/:id", middleware.RequirePermission("crypto_receipt:read"), h.Detail)
	receipts.POST("/:id/credits", middleware.RequirePermission("crypto_receipt:credit"), stepUp, h.Prepare)
	credits := rg.Group("/crypto/merchant-credits")
	credits.POST("/:id/approve", middleware.RequirePermission("crypto_receipt:approve"), stepUp, h.Approve)
	credits.POST("/:id/reject", middleware.RequirePermission("crypto_receipt:credit"), stepUp, h.Reject)
	receipts.POST("/:id/platform-fundings", middleware.RequirePermission("crypto_receipt:credit"), stepUp, h.PreparePlatformFunding)
	rg.GET("/crypto/wallet-fundings", middleware.RequirePermission("crypto_receipt:read"), h.WalletFundings)
	fundings := rg.Group("/crypto/platform-fundings")
	fundings.POST("/:id/approve", middleware.RequirePermission("crypto_receipt:approve"), stepUp, h.ApprovePlatformFunding)
	fundings.POST("/:id/reject", middleware.RequirePermission("crypto_receipt:credit"), stepUp, h.RejectPlatformFunding)
}

func (h *UnidentifiedReceiptHandler) List(c *gin.Context) {
	limit, offset, ok := h.page(c)
	if !ok {
		return
	}
	list, err := h.journal.List(c.Request.Context(), c.DefaultQuery("state", cryptoapp.ReceiptJournalOpen),
		c.Query("includeDust") == "true", limit, offset)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	response := dto.UnidentifiedReceiptListResponse{Items: make([]dto.UnidentifiedReceiptItem, 0, len(list.Items)), Total: list.Total}
	for _, row := range list.Items {
		response.Items = append(response.Items, unidentifiedReceiptItem(row))
	}
	c.JSON(http.StatusOK, response)
}

// Detail is the receipt card: proof, audit, remainder and the documents that
// distributed the receipt.
func (h *UnidentifiedReceiptHandler) Detail(c *gin.Context) {
	receiptID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid receipt id"))
		return
	}
	detail, err := h.journal.Detail(c.Request.Context(), receiptID)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	response := dto.CustodyReceiptDetailResponse{
		Receipt: unidentifiedReceiptItem(detail.Row),
		Proof: dto.CustodyReceiptProof{
			VerificationID: detail.Proof.VerificationID, Original: detail.Proof.Original, TxHash: detail.Proof.TxHash,
			EventKind: detail.Proof.EventKind, EventIndex: detail.Proof.EventIndex, BlockNumber: detail.Proof.BlockNumber,
			BlockHash: detail.Proof.BlockHash, Confirmations: detail.Proof.Confirmations, VerifiedAt: detail.Proof.VerifiedAt,
			Sources: make([]dto.CustodyReceiptProofSource, 0, len(detail.Proof.Sources)),
		},
		Audit: dto.CustodyReceiptAudit{
			Status: detail.Audit.Status, LastCheckedAt: detail.Audit.LastCheckedAt, NextCheckAt: detail.Audit.NextCheckAt,
			Attempts: detail.Audit.Attempts, LastError: detail.Audit.LastError, OrphanedAt: detail.Audit.OrphanedAt,
			IncidentID: optionalText(detail.Audit.IncidentID),
		},
		Attributions: make([]dto.CustodyReceiptAttribution, 0, len(detail.Attributions)),
	}
	for _, source := range detail.Proof.Sources {
		response.Proof.Sources = append(response.Proof.Sources, dto.CustodyReceiptProofSource{
			ProviderID: source.ProviderID, TrustGroup: source.TrustGroup, HeadHeight: source.HeadHeight})
	}
	for _, a := range detail.Attributions {
		response.Attributions = append(response.Attributions, dto.CustodyReceiptAttribution{
			DocumentType: a.DocumentType, DocumentID: a.DocumentID, Number: a.Number, Date: a.Date, Status: a.Status,
			Posted: a.Posted, MerchantID: a.MerchantID, MerchantName: a.MerchantName, Amount: a.Amount,
			NetAmount: a.NetAmount, ProcessingFee: a.ProcessingFee, Outstanding: a.Outstanding,
		})
	}
	h.base.OK(c, response)
}

func (h *UnidentifiedReceiptHandler) Prepare(c *gin.Context) {
	receiptID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid receipt id"))
		return
	}
	var req dto.PrepareMerchantCreditRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.base.Error(c, apperror.NewValidation("merchantId is required"))
		return
	}
	merchantID, err := id.Parse(req.MerchantID)
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid merchant id"))
		return
	}
	var invoiceID *id.ID
	if req.InvoiceID != nil && *req.InvoiceID != "" {
		parsed, err := id.Parse(*req.InvoiceID)
		if err != nil {
			h.base.Error(c, apperror.NewValidation("invalid invoice id"))
			return
		}
		invoiceID = &parsed
	}
	result, err := h.commands.Prepare(c.Request.Context(), c.GetHeader("Idempotency-Key"), receiptID, merchantID, invoiceID)
	h.writeCommandResult(c, result, err)
}

func (h *UnidentifiedReceiptHandler) Approve(c *gin.Context) {
	creditID, req, ok := h.decision(c)
	if !ok {
		return
	}
	result, err := h.commands.Approve(c.Request.Context(), c.GetHeader("Idempotency-Key"), creditID, req.Version)
	h.writeCommandResult(c, result, err)
}

func (h *UnidentifiedReceiptHandler) Reject(c *gin.Context) {
	creditID, req, ok := h.decision(c)
	if !ok {
		return
	}
	result, err := h.commands.Reject(c.Request.Context(), c.GetHeader("Idempotency-Key"), creditID, req.Version, req.Reason)
	h.writeCommandResult(c, result, err)
}

func (h *UnidentifiedReceiptHandler) decision(c *gin.Context) (id.ID, dto.DecideMerchantCreditRequest, bool) {
	var req dto.DecideMerchantCreditRequest
	creditID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid merchant credit id"))
		return id.Nil(), req, false
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.base.Error(c, apperror.NewValidation("version is required"))
		return id.Nil(), req, false
	}
	return creditID, req, true
}

// WalletFundings lists recorded external fundings of platform wallets, newest first.
func (h *UnidentifiedReceiptHandler) WalletFundings(c *gin.Context) {
	limit, offset, ok := h.page(c)
	if !ok {
		return
	}
	list, err := h.journal.WalletFundings(c.Request.Context(), limit, offset)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	response := dto.WalletFundingListResponse{Items: make([]dto.WalletFundingItem, 0, len(list.Items)), Total: list.Total}
	for _, row := range list.Items {
		response.Items = append(response.Items, dto.WalletFundingItem(row))
	}
	c.JSON(http.StatusOK, response)
}

func (h *UnidentifiedReceiptHandler) PreparePlatformFunding(c *gin.Context) {
	receiptID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid receipt id"))
		return
	}
	var req dto.PreparePlatformFundingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.base.Error(c, apperror.NewValidation("reason is required"))
		return
	}
	result, err := h.fundings.Prepare(c.Request.Context(), c.GetHeader("Idempotency-Key"), receiptID, req.Reason, req.AcknowledgeMerchantTie)
	h.writeCommandResult(c, result, err)
}

func (h *UnidentifiedReceiptHandler) ApprovePlatformFunding(c *gin.Context) {
	fundingID, req, ok := h.fundingDecision(c)
	if !ok {
		return
	}
	result, err := h.fundings.Approve(c.Request.Context(), c.GetHeader("Idempotency-Key"), fundingID, req.Version)
	h.writeCommandResult(c, result, err)
}

func (h *UnidentifiedReceiptHandler) RejectPlatformFunding(c *gin.Context) {
	fundingID, req, ok := h.fundingDecision(c)
	if !ok {
		return
	}
	result, err := h.fundings.Reject(c.Request.Context(), c.GetHeader("Idempotency-Key"), fundingID, req.Version, req.Reason)
	h.writeCommandResult(c, result, err)
}

func (h *UnidentifiedReceiptHandler) fundingDecision(c *gin.Context) (id.ID, dto.DecidePlatformFundingRequest, bool) {
	var req dto.DecidePlatformFundingRequest
	fundingID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid platform funding id"))
		return id.Nil(), req, false
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.base.Error(c, apperror.NewValidation("version is required"))
		return id.Nil(), req, false
	}
	return fundingID, req, true
}

func (h *UnidentifiedReceiptHandler) writeCommandResult(c *gin.Context, result command.Execution, err error) {
	if err != nil {
		h.base.Error(c, err)
		return
	}
	if result.Replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	h.base.OK(c, result.Result.Payload)
}

func unidentifiedReceiptItem(row cryptoapp.UnidentifiedReceiptRow) dto.UnidentifiedReceiptItem {
	item := dto.UnidentifiedReceiptItem{
		ReceiptID: optionalText(row.ReceiptID), InboxEventID: optionalText(row.InboxEventID), Number: row.Number, ReceivedAt: row.ReceivedAt,
		NetworkID: row.NetworkID, NetworkName: row.NetworkName, TokenID: row.TokenID, TokenSymbol: row.TokenSymbol,
		DecimalPlaces: row.DecimalPlaces, WalletID: row.WalletID, ToAddress: row.ToAddress, FromAddress: row.FromAddress,
		TxHash: row.TxHash, Amount: row.Amount, Remainder: row.Remainder, Reason: row.Reason, State: row.State,
		CustodyMerchantID: optionalText(row.CustodyMerchantID), InvoiceID: optionalText(row.InvoiceID), PendingError: row.PendingError,
		PaymentID: optionalText(row.PaymentID), PaymentNumber: optionalText(row.PaymentNumber), PaymentStatus: optionalText(row.PaymentStatus),
	}
	if row.CreditID != nil {
		item.Credit = &dto.UnidentifiedReceiptCredit{
			ID: *row.CreditID, Number: optionalText(row.CreditNumber), Status: optionalText(row.CreditStatus), MerchantID: optionalText(row.CreditMerchantID),
			MerchantName: optionalText(row.CreditMerchant), NetAmount: optionalText(row.CreditNet), ProcessingFee: optionalText(row.CreditFee),
			PreparedBy: optionalText(row.CreditPreparedBy), DecidedAt: row.CreditDecidedAt, DecisionReason: optionalText(row.CreditReason),
		}
		if row.CreditVersion != nil {
			item.Credit.Version = *row.CreditVersion
		}
		if row.CreditApproval != nil {
			item.Credit.ApprovalRequired = *row.CreditApproval
		}
	}
	if row.FundingID != nil {
		item.PlatformFunding = &dto.UnidentifiedReceiptPlatformFunding{
			ID: *row.FundingID, Number: optionalText(row.FundingNumber), Status: optionalText(row.FundingStatus),
			Reason: optionalText(row.FundingReason), MerchantName: optionalText(row.FundingMerchant),
			MerchantLink: optionalText(row.FundingMerchantLink), PreparedBy: optionalText(row.FundingPreparedBy),
			DecidedAt: row.FundingDecidedAt, DecisionReason: optionalText(row.FundingDecisionReason),
		}
		if row.FundingVersion != nil {
			item.PlatformFunding.Version = *row.FundingVersion
		}
	}
	if row.RefundID != nil {
		item.Refund = &dto.UnidentifiedReceiptRefund{ID: *row.RefundID, Number: optionalText(row.RefundNumber), Status: optionalText(row.RefundStatus),
			Amount: optionalText(row.RefundAmount), Withheld: optionalText(row.RefundWithheld), Destination: optionalText(row.RefundDestination),
			PreparedBy: optionalText(row.RefundPreparedBy)}
	}
	if row.TieMerchantID != nil {
		item.MerchantTie = &dto.ReceiptMerchantTie{MerchantID: *row.TieMerchantID,
			MerchantName: optionalText(row.TieMerchantName), Link: optionalText(row.TieLink)}
	}
	return item
}

func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
