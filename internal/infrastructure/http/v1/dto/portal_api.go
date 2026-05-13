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
	Symbol        string `json:"symbol"`
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


