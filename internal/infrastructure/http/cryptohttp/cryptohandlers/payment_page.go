package cryptohandlers

import (
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	qrcode "github.com/skip2/go-qrcode"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto/publicref"
)

// PaymentPageResponse is the public-facing response for the payment widget.
// Contains only the information needed for the customer to make a payment.
type PaymentPageResponse struct {
	CheckoutRef     string    `json:"checkoutRef"`
	Status          string    `json:"status"`
	AcceptsPayments bool      `json:"acceptsPayments"`
	WalletAddress   string    `json:"walletAddress"`
	ExpectedAmount  string    `json:"expectedAmount"`
	ReceivedAmount  string    `json:"receivedAmount"`
	TokenSymbol     string    `json:"tokenSymbol"`
	TokenName       string    `json:"tokenName"`
	NetworkName     string    `json:"networkName"`
	ExplorerURL     string    `json:"explorerUrl"`
	DecimalPlaces   int       `json:"decimalPlaces"`
	ExpiresAt       time.Time `json:"expiresAt"`
	MerchantName    string    `json:"merchantName"`
	OrderID         string    `json:"orderId,omitempty"`
	Description     string    `json:"description,omitempty"`
	Confirmations   int       `json:"confirmations"`
	RequiredConfs   int       `json:"requiredConfs"`
	OverpaidAmount  string    `json:"overpaidAmount"`
	QRCode          string    `json:"qrCode,omitempty"`
}

type PaymentPageHandler struct {
	service *cryptoapp.CheckoutPageService
}

func NewPaymentPageHandler(service *cryptoapp.CheckoutPageService) *PaymentPageHandler {
	if service == nil {
		panic("handlers.NewPaymentPageHandler: checkout page service is required")
	}
	return &PaymentPageHandler{service: service}
}

// GetPaymentInfo handles GET /api/v1/checkout/invoices/:publicRef.
func (h *PaymentPageHandler) GetPaymentInfo(c *gin.Context) {
	publicRef := c.Param("publicRef")
	if !isRefWithPrefix(publicRef, publicref.InvoicePrefix) {
		_ = c.Error(apperror.NewNotFound("checkout", "reference"))
		c.Abort()
		return
	}

	data, err := h.service.GetInvoice(c.Request.Context(), publicRef)
	if err != nil {
		_ = c.Error(apperror.NewNotFound("checkout", "reference"))
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, buildPaymentPageResponse(publicRef, &data))
}

// RedeemPaymentLink handles POST /api/v1/checkout/links/:publicRef/redeem.
func (h *PaymentPageHandler) RedeemPaymentLink(c *gin.Context) {
	linkRef := c.Param("publicRef")
	if !isRefWithPrefix(linkRef, publicref.PaymentLinkPrefix) {
		_ = c.Error(apperror.NewNotFound("checkout", "reference"))
		c.Abort()
		return
	}

	invoiceRef, data, err := h.service.RedeemPaymentLink(c.Request.Context(), linkRef)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, buildPaymentPageResponse(invoiceRef, &data))
}

func buildPaymentPageResponse(publicRef string, data *cryptoapp.CheckoutPageData) PaymentPageResponse {
	address := data.WalletAddress
	if !data.AcceptsPayments {
		address = ""
	}
	return PaymentPageResponse{
		CheckoutRef:     publicRef,
		Status:          data.Status,
		AcceptsPayments: data.AcceptsPayments,
		WalletAddress:   address,
		ExpectedAmount:  formatRawAmount(data.ExpectedAmount, data.DecimalPlaces),
		ReceivedAmount:  formatRawAmount(data.ReceivedAmount, data.DecimalPlaces),
		OverpaidAmount:  formatRawAmount(data.OverpaidAmount, data.DecimalPlaces),
		TokenSymbol:     data.TokenSymbol,
		TokenName:       data.TokenName,
		NetworkName:     data.NetworkName,
		ExplorerURL:     data.ExplorerURL,
		DecimalPlaces:   data.DecimalPlaces,
		ExpiresAt:       data.ExpiresAt,
		MerchantName:    data.MerchantName,
		OrderID:         data.OrderID,
		Description:     data.Description,
		Confirmations:   data.Confirmations,
		RequiredConfs:   data.ConfirmationsNeeded,
		QRCode:          generateQRDataURL(address),
	}
}

func formatRawAmount(amount types.RawAmount, decimalPlaces int) string {
	raw := amount.String()
	if decimalPlaces <= 0 {
		return raw
	}
	if len(raw) <= decimalPlaces {
		return "0." + strings.Repeat("0", decimalPlaces-len(raw)) + raw
	}
	return raw[:len(raw)-decimalPlaces] + "." + raw[len(raw)-decimalPlaces:]
}

func generateQRDataURL(content string) string {
	if content == "" {
		return ""
	}
	png, err := qrcode.Encode(content, qrcode.Medium, 256)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// GetInvoiceStatus handles GET /api/v1/checkout/invoices/:publicRef/status.
func (h *PaymentPageHandler) GetInvoiceStatus(c *gin.Context) {
	publicRef := c.Param("publicRef")
	if !isRefWithPrefix(publicRef, publicref.InvoicePrefix) {
		_ = c.Error(apperror.NewNotFound("checkout", "reference"))
		c.Abort()
		return
	}

	status, err := h.service.GetStatus(c.Request.Context(), publicRef)
	if err != nil {
		_ = c.Error(apperror.NewNotFound("checkout", "reference"))
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":          status.Status,
		"acceptsPayments": status.AcceptsPayments,
		"receivedAmount":  formatRawAmount(status.ReceivedAmount, status.DecimalPlaces),
		"overpaidAmount":  formatRawAmount(status.OverpaidAmount, status.DecimalPlaces),
		"confirmations":   status.Confirmations,
	})
}

func isRefWithPrefix(ref, wantPrefix string) bool {
	if err := publicref.Validate(ref); err != nil {
		return false
	}
	prefix, err := publicref.Prefix(ref)
	return err == nil && prefix == wantPrefix
}
