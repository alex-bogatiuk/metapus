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
