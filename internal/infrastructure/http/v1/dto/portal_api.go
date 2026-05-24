// Package dto provides data transfer objects for HTTP API.
package dto

// PortalSummaryResponse contains dashboard summary for a merchant.
type PortalSummaryResponse struct {
	TotalInvoices   int    `json:"totalInvoices"`
	PaidInvoices    int    `json:"paidInvoices"`
	PendingInvoices int    `json:"pendingInvoices"`
	TotalMinorUnits string `json:"totalMinorUnits"` // string for precision
	Change24hPct    string `json:"change24hPct"`    // "+2.34" / "-1.22"
}

// PortalCurrencyItem represents currency breakdown for a merchant.
type PortalCurrencyItem struct {
	TokenID       string `json:"tokenId"`
	Symbol        string `json:"symbol"`
	NetworkID     string `json:"networkId"`
	Network       string `json:"network"`
	Count         int    `json:"count"`
	TotalMinor    string `json:"totalMinor"`
	SharePct      string `json:"sharePct"`      // "41.27"
	DecimalPlaces int    `json:"decimalPlaces"`
}

// PortalChartPoint is a single data point for the volume chart.
type PortalChartPoint struct {
	Day      string `json:"day"`      // "2026-05-01"
	Deposits string `json:"deposits"` // minor units as string
}

// PortalMerchantItem is a lightweight merchant item for the portal switcher.
type PortalMerchantItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

// PortalInvoiceItem represents an invoice in the portal invoice list.
type PortalInvoiceItem struct {
	ID             string `json:"id"`
	Number         string `json:"number"`
	Status         string `json:"status"`
	Amount         string `json:"amount"`
	ReceivedAmount string `json:"receivedAmount"`
	Symbol         string `json:"symbol"`
	Network        string `json:"network"`
	DecimalPlaces  int    `json:"decimalPlaces"`
	CreatedAt      string `json:"createdAt"`

	// Payment details (from joined CryptoPayment, if exists)
	TxHash        string  `json:"txHash,omitempty"`
	FromAddress   string  `json:"fromAddress,omitempty"`
	ProcessingFee string  `json:"processingFee,omitempty"` // minor units
	NetAmount     string  `json:"netAmount,omitempty"`     // minor units
	ConfirmedAt   *string `json:"confirmedAt,omitempty"`   // ISO 8601

	// Invoice metadata
	ExternalID    string  `json:"externalId,omitempty"`
	CustomerEmail string  `json:"customerEmail,omitempty"`
}

// PortalInvoiceListResponse wraps a paginated invoice list.
type PortalInvoiceListResponse struct {
	Items []PortalInvoiceItem `json:"items"`
	Total int                 `json:"total"`
}

// ── Conversion Funnel ──────────────────────────────────────────────────────

// PortalFunnelResponse contains invoice conversion funnel metrics.
type PortalFunnelResponse struct {
	Total       int `json:"total"`
	ReceivedAny int `json:"receivedAny"` // at least one payment detected
	FullyPaid   int `json:"fullyPaid"`   // expected amount reached
	Confirmed   int `json:"confirmed"`   // blockchain-confirmed
	Expired     int `json:"expired"`     // timed out
}

// ── Payment Links ──────────────────────────────────────────────────────────

// CreatePaymentLinkRequest is the request body for creating a payment link.
type CreatePaymentLinkRequest struct {
	TokenID     string `json:"tokenId"     binding:"required,uuid"` // UUID of the token from cat_tokens
	Amount      string `json:"amount"      binding:"required"`      // minor units as string
	Description string `json:"description"`
	Reusable    bool   `json:"reusable"`
	MaxUses     int    `json:"maxUses"`     // 0 = unlimited (if reusable)
	TTLMinutes  int    `json:"ttlMinutes"`  // 0 = use merchant default
}

// PortalPaymentLinkItem represents a payment link in the list.
type PortalPaymentLinkItem struct {
	ID          string `json:"id"`
	ShortCode   string `json:"shortCode"`
	Amount      string `json:"amount"`
	Symbol      string `json:"symbol"`
	Network     string `json:"network"`
	Description string `json:"description"`
	Reusable    bool   `json:"reusable"`
	MaxUses     int    `json:"maxUses"`
	CurrentUses int    `json:"currentUses"`
	Status      string `json:"status"`
	TTLMinutes  int    `json:"ttlMinutes"`
	PayURL      string `json:"payUrl"` // full URL: /pay/link/<shortCode>
	CreatedAt   string `json:"createdAt"`
}

// PortalPaymentLinkListResponse wraps a paginated payment link list.
type PortalPaymentLinkListResponse struct {
	Items []PortalPaymentLinkItem `json:"items"`
	Total int                     `json:"total"`
}

// PortalPaymentLinkCreateResponse is returned after creating a payment link.
type PortalPaymentLinkCreateResponse struct {
	ID        string `json:"id"`
	ShortCode string `json:"shortCode"`
	PayURL    string `json:"payUrl"`
}

// ── Merchant Settings ──────────────────────────────────────────────────────

// PortalSettingsResponse contains self-service merchant settings.
type PortalSettingsResponse struct {
	WebhookURL        string `json:"webhookUrl"`
	DefaultTTLMinutes int    `json:"defaultTtlMinutes"`
}

// UpdatePortalSettingsRequest is the request body for updating settings.
type UpdatePortalSettingsRequest struct {
	WebhookURL        *string `json:"webhookUrl"`        // nil = don't change
	DefaultTTLMinutes *int    `json:"defaultTtlMinutes"` // nil = don't change
}

// ── Balance (Fiat Valuation) ───────────────────────────────────────────────

// PortalBalanceResponse contains merchant balance in reporting (base) currency.
type PortalBalanceResponse struct {
	TotalBase    string              `json:"totalBase"`    // total in base currency, e.g. "1234.56"
	BaseCurrency string             `json:"baseCurrency"` // e.g. "USD"
	RateSource   string             `json:"rateSource"`   // e.g. "coingecko"
	ByToken      []PortalTokenBalance `json:"byToken"`
}

// PortalTokenBalance represents a single token's balance with fiat valuation.
type PortalTokenBalance struct {
	TokenID      string `json:"tokenId"`
	TokenSymbol  string `json:"tokenSymbol"`  // e.g. "USDT"
	CurrencyCode string `json:"currencyCode"` // from Token→Currency, e.g. "USDT"
	RawAmount    string `json:"rawAmount"`     // minor units as string
	HumanAmount  string `json:"humanAmount"`   // human-readable, e.g. "100.50"
	Rate         string `json:"rate"`          // exchange rate to base, e.g. "0.9997"
	Multiplier   int    `json:"multiplier"`    // rate multiplier, e.g. 1
	BaseAmount   string `json:"baseAmount"`    // fiat equivalent, e.g. "100.47"
	HasRate      bool   `json:"hasRate"`       // false if no exchange rate found
}

// ── Invoice Detail ─────────────────────────────────────────────────────

// PortalInvoiceDetailResponse is the full invoice detail with timeline and webhook history.
type PortalInvoiceDetailResponse struct {
	PortalInvoiceItem                                    // embed list fields
	WalletAddress     string                             `json:"walletAddress"`
	ExpiresAt         string                             `json:"expiresAt"` // RFC3339
	Description       string                             `json:"description,omitempty"`
	OrderID           string                             `json:"orderId,omitempty"`
	Timeline          []PortalTimelineEvent              `json:"timeline"`
	WebhookDeliveries []PortalWebhookDeliveryItem        `json:"webhookDeliveries"`
}

// PortalTimelineEvent is a single FSM transition event in the invoice/payment lifecycle.
type PortalTimelineEvent struct {
	ID         string                       `json:"id"`
	EventType  string                       `json:"eventType"`
	FromStatus string                       `json:"fromStatus"`
	ToStatus   string                       `json:"toStatus"`
	Metadata   PortalTimelineEventMetadata  `json:"metadata"`
	CreatedAt  string                       `json:"createdAt"`
}

// PortalTimelineEventMetadata contains blockchain-specific data for a timeline event.
type PortalTimelineEventMetadata struct {
	Confirmations int    `json:"confirmations,omitempty"`
	RequiredConfs int    `json:"requiredConfs,omitempty"`
	BlockNumber   int64  `json:"blockNumber,omitempty"`
	TxHash        string `json:"txHash,omitempty"`
}

// PortalWebhookDeliveryItem is a single webhook delivery attempt for the portal UI.
type PortalWebhookDeliveryItem struct {
	ID             string  `json:"id"`
	EventType      string  `json:"eventType"`
	DeliveryID     string  `json:"deliveryId"`
	StatusCode     *int    `json:"statusCode"`
	ResponseTimeMs *int    `json:"responseTimeMs"`
	Attempt        int     `json:"attempt"`
	ErrorMessage   *string `json:"errorMessage"`
	CreatedAt      string  `json:"createdAt"`
}

// ── Withdrawals ────────────────────────────────────────────────────────

// PortalWithdrawalItem represents a withdrawal in the portal withdrawal list.
type PortalWithdrawalItem struct {
	ID            string  `json:"id"`
	Number        string  `json:"number"`
	Status        string  `json:"status"` // created, signed, broadcast, confirmed, failed
	Amount        string  `json:"amount"` // minor units
	Symbol        string  `json:"symbol"`
	Network       string  `json:"network"`
	DecimalPlaces int     `json:"decimalPlaces"`
	DestAddress   string  `json:"destAddress"`
	TxHash        string  `json:"txHash,omitempty"`
	NetworkFee    string  `json:"networkFee,omitempty"`
	CreatedAt     string  `json:"createdAt"`
	ConfirmedAt   *string `json:"confirmedAt,omitempty"`
}

// PortalWithdrawalListResponse wraps a paginated withdrawal list.
type PortalWithdrawalListResponse struct {
	Items []PortalWithdrawalItem `json:"items"`
	Total int                    `json:"total"`
}

// ── Test Webhook ───────────────────────────────────────────────────────

// PortalTestWebhookResponse is returned after sending a test webhook.
type PortalTestWebhookResponse struct {
	Success        bool    `json:"success"`
	StatusCode     *int    `json:"statusCode"`
	ResponseTimeMs *int    `json:"responseTimeMs"`
	Error          *string `json:"error"`
}

// ── Webhook Secret ────────────────────────────────────────────────────

// PortalWebhookSecretResponse is returned when revealing or rotating a webhook secret.
type PortalWebhookSecretResponse struct {
	Secret string `json:"secret"`
}

// ── Fee Schedule ──────────────────────────────────────────────────────

// PortalFeeItem represents a single fee configuration entry visible to the merchant.
type PortalFeeItem struct {
	TokenSymbol   string `json:"tokenSymbol"`
	Network       string `json:"network"`
	Direction     string `json:"direction"`     // "processing" | "withdrawal"
	FixedFee      string `json:"fixedFee"`      // minor units as string
	PercentBP     int    `json:"percentBp"`     // basis points [0..10000]
	MinFee        string `json:"minFee"`        // minor units, "0" if no floor
	MaxFee        string `json:"maxFee"`        // minor units, "0" if no cap
	DecimalPlaces int    `json:"decimalPlaces"`
	IsCustom      bool   `json:"isCustom"`      // true if merchant-specific override
}

// PortalFeeScheduleResponse wraps the fee schedule list.
type PortalFeeScheduleResponse struct {
	Items []PortalFeeItem `json:"items"`
}

// ── Portal Create Invoice ─────────────────────────────────────────────

// PortalCreateInvoiceRequest is the portal form submission for creating an invoice.
// Amount is human-readable (e.g. "10.5"), converted to minor units server-side.
type PortalCreateInvoiceRequest struct {
	TokenID       string  `json:"tokenId" binding:"required"`
	Amount        string  `json:"amount" binding:"required"` // human-readable, e.g. "10.5"
	TTLMinutes    *int    `json:"ttlMinutes"`
	Description   *string `json:"description"`
	OrderID       *string `json:"orderId"`
	CustomerEmail *string `json:"customerEmail"`
}

// PortalCreateInvoiceResponse is the response after creating an invoice from the portal.
type PortalCreateInvoiceResponse struct {
	ID            string `json:"id"`
	Number        string `json:"number"`
	Status        string `json:"status"`
	Amount        string `json:"amount"` // minor units as string
	Symbol        string `json:"symbol"`
	Network       string `json:"network"`
	DecimalPlaces int    `json:"decimalPlaces"`
	WalletAddress string `json:"walletAddress"`
	ExpiresAt     string `json:"expiresAt"` // RFC3339
	CreatedAt     string `json:"createdAt"` // RFC3339
}

// ── Detailed Balance (three-bucket) ───────────────────────────────────

// PortalDetailedBalanceResponse extends the balance with pending/available breakdown.
type PortalDetailedBalanceResponse struct {
	TotalBase     string                `json:"totalBase"`     // total fiat in base currency
	PendingBase   string                `json:"pendingBase"`   // pending fiat
	AvailableBase string                `json:"availableBase"` // available fiat
	BaseCurrency  string                `json:"baseCurrency"`  // e.g. "USD"
	RateSource    string                `json:"rateSource"`    // e.g. "coingecko"
	ByToken       []PortalTokenDetailed `json:"byToken"`
}

// PortalTokenDetailed extends PortalTokenBalance with pending/available split.
type PortalTokenDetailed struct {
	PortalTokenBalance                        // embed existing fields
	PendingRaw         string `json:"pendingRaw"`     // minor units
	PendingHuman       string `json:"pendingHuman"`   // human-readable
	AvailableRaw       string `json:"availableRaw"`   // minor units
	AvailableHuman     string `json:"availableHuman"` // human-readable
}

// ── Withdrawal Address Whitelist ──────────────────────────────────────

// PortalWithdrawalAddress represents a whitelisted withdrawal destination.
type PortalWithdrawalAddress struct {
	ID        string `json:"id"`
	NetworkID string `json:"networkId"`
	Network   string `json:"network"`
	Address   string `json:"address"`
	Label     string `json:"label"`
	CreatedAt string `json:"createdAt"`
}

// PortalAddWithdrawalAddressRequest is the request to whitelist a new address.
type PortalAddWithdrawalAddressRequest struct {
	NetworkID string `json:"networkId" binding:"required"`
	Address   string `json:"address" binding:"required"`
	Label     string `json:"label"`
}

// PortalWithdrawalAddressListResponse wraps the whitelist.
type PortalWithdrawalAddressListResponse struct {
	Items []PortalWithdrawalAddress `json:"items"`
}

// ── Withdrawal Requests ───────────────────────────────────────────────

// PortalCreateWithdrawalRequest is the portal form to request a withdrawal.
type PortalCreateWithdrawalRequest struct {
	TokenID    string `json:"tokenId" binding:"required"`
	Amount     string `json:"amount" binding:"required"`    // human-readable
	AddressID  string `json:"addressId" binding:"required"` // from whitelist
}

// PortalWithdrawalRequestItem represents a withdrawal request in the list.
type PortalWithdrawalRequestItem struct {
	ID              string  `json:"id"`
	Number          string  `json:"number"`
	Status          string  `json:"status"` // pending_approval, approved, signing, broadcast, confirmed, rejected, failed
	Amount          string  `json:"amount"` // minor units
	Symbol          string  `json:"symbol"`
	Network         string  `json:"network"`
	DecimalPlaces   int     `json:"decimalPlaces"`
	DestAddress     string  `json:"destAddress"`
	RejectionReason *string `json:"rejectionReason,omitempty"`
	CreatedAt       string  `json:"createdAt"`
	ApprovedAt      *string `json:"approvedAt,omitempty"`
}

// PortalWithdrawalRequestListResponse wraps the withdrawal request list.
type PortalWithdrawalRequestListResponse struct {
	Items []PortalWithdrawalRequestItem `json:"items"`
	Total int                           `json:"total"`
}
