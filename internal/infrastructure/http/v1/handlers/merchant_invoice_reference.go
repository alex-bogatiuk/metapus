package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/http/v1/dto"
	mw "metapus/internal/infrastructure/http/v1/middleware"
)

// AuthorizeInvoiceReference must precede CommandIdempotency, including replay.
func (h *MerchantInvoiceHandler) AuthorizeInvoiceReference(c *gin.Context) {
	mc := mw.GetMerchant(c.Request.Context())
	if mc == nil {
		_ = c.Error(apperror.NewUnauthorized("merchant context missing"))
		c.Abort()
		return
	}
	invoiceID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid invoice id"))
		c.Abort()
		return
	}
	if err := h.merchantInvoiceService.CheckInvoiceReferenceAccess(c.Request.Context(), mc.MerchantID, invoiceID); err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.Next()
}

func (h *MerchantInvoiceHandler) BindInvoiceReference(c *gin.Context) {
	mc := mw.GetMerchant(c.Request.Context())
	if mc == nil {
		_ = c.Error(apperror.NewUnauthorized("merchant context missing"))
		c.Abort()
		return
	}
	invoiceID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid invoice id"))
		c.Abort()
		return
	}
	var req dto.BindMerchantInvoiceReferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}
	invoice, err := h.merchantInvoiceService.BindMerchantInvoiceReference(c.Request.Context(), cryptoapp.BindMerchantInvoiceReferenceInput{
		MerchantID: mc.MerchantID, InvoiceID: invoiceID, APIKeyID: mc.KeyID,
		OrderID: string(req.OrderID), ExternalID: string(req.ExternalID), Metadata: map[string]string(req.Metadata),
	})
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	h.writeMerchantInvoiceResponse(c, invoice, http.StatusOK)
}
