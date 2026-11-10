package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/command"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

// UnidentifiedReceiptHandler serves the journal of unidentified custody
// receipts and the commands that credit them to merchants.
type UnidentifiedReceiptHandler struct {
	base     *BaseHandler
	journal  *crypto_repo.UnidentifiedReceiptRepo
	commands *cryptoapp.MerchantCreditCommands
}

func NewUnidentifiedReceiptHandler(base *BaseHandler, journal *crypto_repo.UnidentifiedReceiptRepo, commands *cryptoapp.MerchantCreditCommands) *UnidentifiedReceiptHandler {
	return &UnidentifiedReceiptHandler{base: base, journal: journal, commands: commands}
}

// RegisterRoutes mounts the journal. stepUp guards every money command.
func (h *UnidentifiedReceiptHandler) RegisterRoutes(rg *gin.RouterGroup, stepUp gin.HandlerFunc) {
	receipts := rg.Group("/crypto/unidentified-receipts")
	receipts.GET("", middleware.RequirePermission("crypto_receipt:read"), h.List)
	receipts.POST("/:id/credits", middleware.RequirePermission("crypto_receipt:credit"), stepUp, h.Prepare)
	credits := rg.Group("/crypto/merchant-credits")
	credits.POST("/:id/approve", middleware.RequirePermission("crypto_receipt:approve"), stepUp, h.Approve)
	credits.POST("/:id/reject", middleware.RequirePermission("crypto_receipt:credit"), stepUp, h.Reject)
}

func (h *UnidentifiedReceiptHandler) List(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || limit < 1 || limit > 100 {
		h.base.Error(c, apperror.NewValidation("limit must be between 1 and 100"))
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		h.base.Error(c, apperror.NewValidation("offset must be non-negative"))
		return
	}
	list, err := h.journal.List(c.Request.Context(), c.DefaultQuery("state", crypto_repo.ReceiptJournalOpen),
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

func unidentifiedReceiptItem(row crypto_repo.UnidentifiedReceiptRow) dto.UnidentifiedReceiptItem {
	item := dto.UnidentifiedReceiptItem{
		ReceiptID: optionalText(row.ReceiptID), InboxEventID: optionalText(row.InboxEventID), Number: row.Number, ReceivedAt: row.ReceivedAt,
		NetworkID: row.NetworkID, NetworkName: row.NetworkName, TokenID: row.TokenID, TokenSymbol: row.TokenSymbol,
		DecimalPlaces: row.DecimalPlaces, WalletID: row.WalletID, ToAddress: row.ToAddress, FromAddress: row.FromAddress,
		TxHash: row.TxHash, Amount: row.Amount, Remainder: row.Remainder, Reason: row.Reason, State: row.State,
		CustodyMerchantID: optionalText(row.CustodyMerchantID), InvoiceID: optionalText(row.InvoiceID), PendingError: row.PendingError,
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
	return item
}

func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
