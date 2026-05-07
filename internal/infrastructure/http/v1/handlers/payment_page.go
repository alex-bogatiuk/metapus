package handlers

import (
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	qrcode "github.com/skip2/go-qrcode"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/storage/postgres"
)

// PaymentPageResponse is the public-facing response for the payment widget.
// Contains only the information needed for the customer to make a payment.
// No internal IDs, merchant secrets, or sensitive data exposed.
type PaymentPageResponse struct {
	InvoiceID      string    `json:"invoiceId"`
	Status         string    `json:"status"`
	WalletAddress  string    `json:"walletAddress"`
	ExpectedAmount string    `json:"expectedAmount"`   // human-readable (e.g. "5.000000")
	ReceivedAmount string    `json:"receivedAmount"`    // human-readable
	TokenSymbol    string    `json:"tokenSymbol"`       // e.g. "USDT"
	TokenName      string    `json:"tokenName"`         // e.g. "Tether USD (TRC-20)"
	NetworkName    string    `json:"networkName"`       // e.g. "TRON Shasta Testnet"
	ExplorerURL    string    `json:"explorerUrl"`       // e.g. "https://shasta.tronscan.org"
	DecimalPlaces  int       `json:"decimalPlaces"`     // e.g. 6 for USDT
	ExpiresAt      time.Time `json:"expiresAt"`
	MerchantName   string    `json:"merchantName"`
	OrderID        string    `json:"orderId,omitempty"`
	Description    string    `json:"description,omitempty"`
	Confirmations  int       `json:"confirmations"`     // current confirmations (from payments)
	RequiredConfs  int       `json:"requiredConfs"`     // required confirmations
	QRCode         string    `json:"qrCode,omitempty"` // base64 data URL (PNG)
}

// paymentPageData holds all data needed to construct PaymentPageResponse.
// Fetched in a single DB round-trip.
type paymentPageData struct {
	// From crypto invoice
	Status         string
	WalletAddress  string
	ExpectedAmount types.CryptoAmount
	ReceivedAmount types.CryptoAmount
	ExpiresAt      time.Time
	OrderID        string
	Description    string

	// From token
	TokenSymbol   string
	TokenName     string
	DecimalPlaces int

	// From network
	NetworkName       string
	ExplorerURL       string
	ConfirmationsNeeded int

	// From merchant
	MerchantName string

	// Aggregated from payments
	Confirmations int
}

// PaymentPageHandler handles the public payment page API.
type PaymentPageHandler struct{}

// NewPaymentPageHandler creates a new payment page handler.
func NewPaymentPageHandler() *PaymentPageHandler {
	return &PaymentPageHandler{}
}

// GetPaymentInfo handles GET /api/v1/pay/:invoiceId.
// Public endpoint — no authentication required.
func (h *PaymentPageHandler) GetPaymentInfo(c *gin.Context) {
	invoiceID, err := id.Parse(c.Param("invoiceId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid invoice ID"})
		return
	}

	ctx := c.Request.Context()
	pool := tenant.MustGetPool(ctx)

	data, err := h.fetchPaymentData(ctx, pool, invoiceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invoice not found"})
		return
	}

	resp := PaymentPageResponse{
		InvoiceID:      invoiceID.String(),
		Status:         data.Status,
		WalletAddress:  data.WalletAddress,
		ExpectedAmount: formatCryptoAmount(data.ExpectedAmount, data.DecimalPlaces),
		ReceivedAmount: formatCryptoAmount(data.ReceivedAmount, data.DecimalPlaces),
		TokenSymbol:    data.TokenSymbol,
		TokenName:      data.TokenName,
		NetworkName:    data.NetworkName,
		ExplorerURL:    data.ExplorerURL,
		DecimalPlaces:  data.DecimalPlaces,
		ExpiresAt:      data.ExpiresAt,
		MerchantName:   data.MerchantName,
		OrderID:        data.OrderID,
		Description:    data.Description,
		Confirmations:  data.Confirmations,
		RequiredConfs:  data.ConfirmationsNeeded,
		QRCode:         generateQRDataURL(data.WalletAddress),
	}

	c.JSON(http.StatusOK, resp)
}

// fetchPaymentData fetches all data needed for the payment page in one query.
func (h *PaymentPageHandler) fetchPaymentData(ctx interface{ Deadline() (time.Time, bool); Done() <-chan struct{}; Err() error; Value(any) any }, pool postgres.Querier, invoiceID id.ID) (*paymentPageData, error) {
	const query = `
		SELECT
			ci.status,
			COALESCE(w.address, '') AS wallet_address,
			ci.expected_amount,
			ci.received_amount,
			ci.expires_at,
			COALESCE(ci.order_id, '') AS order_id,
			COALESCE(ci.description, '') AS description,
			COALESCE(t.symbol, t.code, '') AS token_symbol,
			COALESCE(t.name, '') AS token_name,
			COALESCE(t.decimal_places, 6) AS decimal_places,
			COALESCE(n.name, '') AS network_name,
			COALESCE(n.explorer_url, '') AS explorer_url,
			COALESCE(n.confirmations_needed, 19) AS confirmations_needed,
			COALESCE(m.name, '') AS merchant_name,
			COALESCE((
				SELECT MAX(cp.confirmations)
				FROM doc_crypto_payments cp
				WHERE cp.invoice_id = ci.id
			), 0) AS max_confirmations
		FROM doc_crypto_invoices ci
		LEFT JOIN cat_wallets w ON w.id = ci.wallet_id
		LEFT JOIN cat_tokens t ON t.id = ci.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		LEFT JOIN cat_merchants m ON m.id = ci.merchant_id
		WHERE ci.id = $1
	`

	var data paymentPageData
	var expectedAmountStr, receivedAmountStr string
	var status string

	err := pool.QueryRow(ctx, query, invoiceID).Scan(
		&status,
		&data.WalletAddress,
		&expectedAmountStr,
		&receivedAmountStr,
		&data.ExpiresAt,
		&data.OrderID,
		&data.Description,
		&data.TokenSymbol,
		&data.TokenName,
		&data.DecimalPlaces,
		&data.NetworkName,
		&data.ExplorerURL,
		&data.ConfirmationsNeeded,
		&data.MerchantName,
		&data.Confirmations,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch payment page data: %w", err)
	}

	data.Status = status
	data.ExpectedAmount, _ = types.NewCryptoAmountFromString(expectedAmountStr)
	data.ReceivedAmount, _ = types.NewCryptoAmountFromString(receivedAmountStr)

	return &data, nil
}

// formatCryptoAmount converts minor units to human-readable format.
// E.g. 5000000 with 6 decimal places → "5.000000"
func formatCryptoAmount(amount types.CryptoAmount, decimalPlaces int) string {
	raw := amount.BigInt()
	if raw == nil {
		return "0"
	}

	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimalPlaces)), nil)
	whole := new(big.Int).Div(raw, divisor)
	remainder := new(big.Int).Mod(raw, divisor)

	// Ensure remainder has leading zeros
	format := fmt.Sprintf("%%s.%%0%dd", decimalPlaces)
	return fmt.Sprintf(format, whole.String(), remainder.Int64())
}

// generateQRDataURL renders a QR code as a base64-encoded PNG data URL.
// Returns empty string on error (graceful degradation).
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

// GetInvoiceStatus handles GET /api/v1/pay/:invoiceId/status.
// Lightweight endpoint for polling — returns only status + confirmations.
func (h *PaymentPageHandler) GetInvoiceStatus(c *gin.Context) {
	invoiceID, err := id.Parse(c.Param("invoiceId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid invoice ID"})
		return
	}

	ctx := c.Request.Context()
	pool := tenant.MustGetPool(ctx)

	var status string
	var receivedAmountStr string
	var confirmations int
	var decimalPlaces int

	err = pool.QueryRow(ctx, `
		SELECT
			ci.status,
			ci.received_amount,
			COALESCE((
				SELECT MAX(cp.confirmations) FROM doc_crypto_payments cp WHERE cp.invoice_id = ci.id
			), 0),
			COALESCE(t.decimal_places, 6)
		FROM doc_crypto_invoices ci
		LEFT JOIN cat_tokens t ON t.id = ci.token_id
		WHERE ci.id = $1
	`, invoiceID).Scan(&status, &receivedAmountStr, &confirmations, &decimalPlaces)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invoice not found"})
		return
	}

	receivedAmount, _ := types.NewCryptoAmountFromString(receivedAmountStr)

	// Check expiration client-side — return current status from DB
	// Status is already managed by the expiration loop in crypto_worker

	c.JSON(http.StatusOK, gin.H{
		"status":         status,
		"receivedAmount": formatCryptoAmount(receivedAmount, decimalPlaces),
		"confirmations":  confirmations,
	})
}
