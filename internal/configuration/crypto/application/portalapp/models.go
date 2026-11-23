// Package portalapp defines transport-independent portal query and command models.
package portalapp

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/aml"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"time"
)

// PortalSummaryResponse contains dashboard summary for a merchant.
// PortalReceiptTotal retains each asset's raw unit and network identity.
type PortalReceiptTotal struct {
	TokenID          string `json:"tokenId"`
	NetworkID        string `json:"networkId"`
	Symbol           string `json:"symbol"`
	DecimalPlaces    int    `json:"decimalPlaces"`
	TotalMinor       string `json:"totalMinor"`
	Received24hMinor string `json:"received24hMinor"`
	Change24hPct     string `json:"change24hPct"`
}
type PortalSummaryResponse struct {
	TotalInvoices   int                  `json:"totalInvoices"`
	PaidInvoices    int                  `json:"paidInvoices"`
	PendingInvoices int                  `json:"pendingInvoices"`
	ReceiptTotals   []PortalReceiptTotal `json:"receiptTotals"`
}

// PortalCurrencyItem represents currency breakdown for a merchant.
type PortalCurrencyItem struct {
	TokenID         string `json:"tokenId"`
	Symbol          string `json:"symbol"`
	NetworkID       string `json:"networkId"`
	Network         string `json:"network"`
	Count           int    `json:"count"`
	TotalMinor      string `json:"totalMinor"`
	InvoiceSharePct string `json:"invoiceSharePct"` // share of invoice count, never mixed raw amounts
	DecimalPlaces   int    `json:"decimalPlaces"`
}

// PortalChartPoint is a single data point for the volume chart.
type PortalChartPoint struct {
	Day           string `json:"day"`      // "2026-05-01"
	Deposits      string `json:"deposits"` // minor units as string
	TokenID       string `json:"tokenId"`
	Symbol        string `json:"symbol"`
	Network       string `json:"network"`
	DecimalPlaces int    `json:"decimalPlaces"`
}

// PortalMerchantItem is a lightweight merchant item for the portal switcher.
type PortalMerchantItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Code          string `json:"code"`
	Role          int    `json:"role"`
	NetworkAccess string `json:"networkAccess"`
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
	ExplorerURL    string `json:"explorerUrl,omitempty"`
	DecimalPlaces  int    `json:"decimalPlaces"`
	CreatedAt      string `json:"createdAt"`

	// Totals across all current canonical payments, in token minor units.
	PaymentCount  int    `json:"paymentCount"`
	ProcessingFee string `json:"processingFee"`
	NetAmount     string `json:"netAmount"`

	// Invoice metadata
	ExternalID  string `json:"externalId,omitempty"`
	OrderID     string `json:"orderId,omitempty"`
	CustomerID  string `json:"customerId,omitempty"`
	InvoiceKind string `json:"invoiceKind,omitempty"`
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
	MaxUses     int    `json:"maxUses"`    // 0 = unlimited (if reusable)
	TTLMinutes  int    `json:"ttlMinutes"` // 0 = use merchant default
}

// PortalPaymentLinkItem represents a payment link in the list.
type PortalPaymentLinkItem struct {
	ID          string `json:"id"`
	PublicRef   string `json:"publicRef"`
	Amount      string `json:"amount"`
	Symbol      string `json:"symbol"`
	Network     string `json:"network"`
	Description string `json:"description"`
	Reusable    bool   `json:"reusable"`
	MaxUses     int    `json:"maxUses"`
	CurrentUses int    `json:"currentUses"`
	Status      string `json:"status"`
	TTLMinutes  int    `json:"ttlMinutes"`
	PayURL      string `json:"payUrl"`
	CreatedAt   string `json:"createdAt"`
}

// PortalPaymentLinkListResponse wraps a paginated payment link list.
type PortalPaymentLinkListResponse struct {
	Items []PortalPaymentLinkItem `json:"items"`
	Total int                     `json:"total"`
}

// PortalPaymentLinkCreateResponse is returned after creating a payment link.
type PortalPaymentLinkCreateResponse struct {
	Object    string `json:"object"`
	ID        string `json:"id"`
	PublicRef string `json:"publicRef"`
	PayURL    string `json:"payUrl"`
}

// ── Merchant Settings ──────────────────────────────────────────────────────

// PortalSettingsResponse contains self-service merchant settings.
type PortalSettingsResponse struct {
	DefaultTTLMinutes       int  `json:"defaultTtlMinutes"`
	SinglePaymentPerInvoice bool `json:"singlePaymentPerInvoice"`
}

// UpdatePortalSettingsRequest is the request body for updating settings.
type UpdatePortalSettingsRequest struct {
	DefaultTTLMinutes       *int  `json:"defaultTtlMinutes"` // nil = don't change
	SinglePaymentPerInvoice *bool `json:"singlePaymentPerInvoice"`
}

// ── Balance (Fiat Valuation) ───────────────────────────────────────────────

// PortalBalanceResponse contains merchant balance in reporting (base) currency.
type PortalBalanceResponse = crypto.MerchantBalance
type PortalTokenBalance = crypto.TokenBalance

// ── Invoice Detail ─────────────────────────────────────────────────────

// PortalInvoiceDetailResponse is the full invoice detail with timeline and webhook history.
type PortalInvoiceDetailResponse struct {
	PortalInvoiceItem                                     // embed list fields
	Payments                []PortalInvoicePayment        `json:"payments"`
	CheckoutRef             string                        `json:"checkoutRef"`
	PayURL                  string                        `json:"payUrl"`
	WalletAddress           string                        `json:"walletAddress"`
	AcceptsPayments         bool                          `json:"acceptsPayments"`
	ExpiresAt               string                        `json:"expiresAt"` // RFC3339
	Description             string                        `json:"description,omitempty"`
	Timeline                []PortalTimelineEvent         `json:"timeline"`
	WebhookDeliveries       []PortalWebhookDeliveryItem   `json:"webhookDeliveries"`
	ObservedAt              string                        `json:"observedAt"`
	FinancialSummary        PortalInvoiceFinancialSummary `json:"financialSummary"`
	PaymentAcceptanceReason string                        `json:"paymentAcceptanceReason"`
	CanViewWebhooks         bool                          `json:"canViewWebhooks"`
	WebhookDeliveryTotal    int                           `json:"webhookDeliveryTotal"`
}

// PortalInvoiceLedger reports immutable movements, including linked reversals.
// Unknown amounts are null, never a fabricated zero.
type PortalInvoiceLedger struct {
	Status              string  `json:"status"`
	CreditedNetAmount   *string `json:"creditedNetAmount"`
	ProcessingFeeAmount *string `json:"processingFeeAmount"`
	ReversedNetAmount   *string `json:"reversedNetAmount"`
}

type PortalInvoiceFinancialSummary struct {
	ConfirmedAmount  string              `json:"confirmedAmount"`
	PendingNetAmount string              `json:"pendingNetAmount"`
	Ledger           PortalInvoiceLedger `json:"ledger"`
}

// PortalInvoicePayment is one blockchain receipt associated with an invoice.
type PortalInvoicePayment struct {
	ID            string              `json:"id"`
	Amount        string              `json:"amount"`
	ProcessingFee string              `json:"processingFee"`
	NetAmount     string              `json:"netAmount"`
	Status        string              `json:"status"`
	Canonicality  string              `json:"canonicality"`
	Posted        bool                `json:"posted"`
	TxHash        string              `json:"txHash"`
	FromAddress   string              `json:"fromAddress"`
	ConfirmedAt   *string             `json:"confirmedAt,omitempty"`
	CreatedAt     string              `json:"createdAt"`
	Confirmations int                 `json:"confirmations"`
	RequiredConfs int                 `json:"requiredConfs"`
	Ledger        PortalInvoiceLedger `json:"ledger"`
	// Refunds of this payment, in token minor units: paid out to the payer,
	// held by refund requests still in progress, and what is left to refund.
	RefundedAmount      string `json:"refundedAmount"`
	RefundPendingAmount string `json:"refundPendingAmount"`
	RefundableAmount    string `json:"refundableAmount"`
}

// PortalTimelineEvent is a single FSM transition event in the invoice/payment lifecycle.
type PortalTimelineEvent struct {
	ID         string                      `json:"id"`
	PaymentID  string                      `json:"paymentId"`
	EventType  string                      `json:"eventType"`
	FromStatus string                      `json:"fromStatus"`
	ToStatus   string                      `json:"toStatus"`
	Metadata   PortalTimelineEventMetadata `json:"metadata"`
	CreatedAt  string                      `json:"createdAt"`
}

// PortalTimelineEventMetadata contains blockchain-specific data for a timeline event.
type PortalTimelineEventMetadata struct {
	Confirmations int    `json:"confirmations,omitzero"`
	RequiredConfs int    `json:"requiredConfs,omitzero"`
	BlockNumber   int64  `json:"blockNumber,omitzero"`
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
	RiskClass     *string `json:"riskClass,omitempty"`
	ID            string  `json:"id"`
	Number        string  `json:"number"`
	Status        string  `json:"status"` // created, signed, broadcast, confirmed, failed
	Amount        string  `json:"amount"` // minor units
	Symbol        string  `json:"symbol"`
	Network       string  `json:"network"`
	ExplorerURL   string  `json:"explorerUrl,omitempty"`
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

// ── Webhook Secret ────────────────────────────────────────────────────

// ── Fee Schedule ──────────────────────────────────────────────────────

// PortalFeeItem represents a single fee configuration entry visible to the merchant.
type PortalFeeItem struct {
	TokenSymbol   string `json:"tokenSymbol"`
	Network       string `json:"network"`
	Direction     string `json:"direction"` // "processing" | "withdrawal"
	FixedFee      string `json:"fixedFee"`  // minor units as string
	PercentBP     int    `json:"percentBp"` // basis points [0..10000]
	MinFee        string `json:"minFee"`    // minor units, "0" if no floor
	MaxFee        string `json:"maxFee"`    // minor units, "0" if no cap
	DecimalPlaces int    `json:"decimalPlaces"`
	IsCustom      bool   `json:"isCustom"` // true if merchant-specific override
}

// PortalFeeScheduleResponse wraps the fee schedule list.
type PortalFeeScheduleResponse struct {
	Items []PortalFeeItem `json:"items"`
}

// ── Portal Create Invoice ─────────────────────────────────────────────

// PortalCreateInvoiceRequest is the portal form submission for creating an invoice.
// Amount is human-readable (e.g. "10.5"), converted to minor units server-side.
type PortalCreateInvoiceRequest struct {
	TokenID     string  `json:"tokenId" binding:"required"`
	Amount      string  `json:"amount" binding:"required"` // human-readable, e.g. "10.5"
	TTLMinutes  *int    `json:"ttlMinutes"`
	Description *string `json:"description"`
	OrderID     *string `json:"orderId"`
	CustomerID  *string `json:"customerId"`
}

// PortalCreateInvoiceResponse is the response after creating an invoice from the portal.
type PortalCreateInvoiceResponse struct {
	Object        string `json:"object"`
	ID            string `json:"id"`
	Version       int    `json:"version"`
	CheckoutRef   string `json:"checkoutRef"`
	PayURL        string `json:"payUrl"`
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
type PortalDetailedBalanceResponse = crypto.MerchantBalance
type PortalTokenDetailed = crypto.TokenBalance

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
	TokenID      string `json:"tokenId" binding:"required"`
	Amount       string `json:"amount" binding:"required"`    // human-readable
	AddressID    string `json:"addressId" binding:"required"` // from whitelist
	RiskClass    string `json:"riskClass,omitempty" binding:"omitempty,oneof=low medium high unknown"`
	RiskAccepted bool   `json:"riskAccepted"`
	// RefundPaymentID makes the request a refund of that credited payment;
	// RefundReason is then required and kept with the request.
	RefundPaymentID string `json:"refundPaymentId,omitempty" binding:"omitempty,uuid"`
	RefundReason    string `json:"refundReason,omitempty" binding:"omitempty,max=500"`
}

// PortalRefundablePaymentResponse is what is left to refund of a credited
// payment. Committed covers refunds in progress and paid out; a rejected or
// failed refund restores its amount. Amounts are token minor units.
type PortalRefundablePaymentResponse struct {
	PaymentID        string `json:"paymentId"`
	InvoiceID        string `json:"invoiceId"`
	InvoiceNumber    string `json:"invoiceNumber"`
	TokenID          string `json:"tokenId"`
	FromAddress      string `json:"fromAddress"`
	Amount           string `json:"amount"`
	CommittedAmount  string `json:"committedAmount"`
	RefundableAmount string `json:"refundableAmount"`
}

// PortalWithdrawalRequestItem represents a withdrawal request in the list.
type PortalWithdrawalRequestItem struct {
	RiskClass       *string                  `json:"riskClass,omitempty"`
	RiskAccepted    bool                     `json:"riskAccepted"`
	Sources         []PortalWithdrawalSource `json:"sources,omitempty"`
	ID              string                   `json:"id"`
	Number          string                   `json:"number"`
	Status          string                   `json:"status"` // pending_approval, approved, signing, broadcast, confirmed, rejected, failed
	Amount          string                   `json:"amount"` // minor units
	Symbol          string                   `json:"symbol"`
	Network         string                   `json:"network"`
	DecimalPlaces   int                      `json:"decimalPlaces"`
	DestAddress     string                   `json:"destAddress"`
	RejectionReason *string                  `json:"rejectionReason,omitempty"`
	CreatedAt       string                   `json:"createdAt"`
	ApprovedAt      *string                  `json:"approvedAt,omitempty"`
	// RefundPaymentID is set when the request refunds that payment.
	RefundPaymentID *string `json:"refundPaymentId,omitempty"`
	RefundReason    *string `json:"refundReason,omitempty"`
}

type PortalWithdrawalSource struct {
	Cancellable     bool    `json:"cancellable"`
	ID              string  `json:"id"`
	WalletID        string  `json:"walletId"`
	Address         string  `json:"address"`
	Amount          string  `json:"amount"`
	Status          string  `json:"status"`
	TxHash          string  `json:"txHash"`
	RequestedClass  string  `json:"requestedClass"`
	DispatchedClass *string `json:"dispatchedClass"`
	CurrentClass    *string `json:"currentClass"`
	DispatchedAt    *string `json:"dispatchedAt"`
}

// PortalWithdrawalRequestListResponse wraps the withdrawal request list.
type PortalWithdrawalRequestListResponse struct {
	Items []PortalWithdrawalRequestItem `json:"items"`
	Total int                           `json:"total"`
}

// AdminWithdrawalRequestItem represents a withdrawal request in ERP/admin.
type AdminWithdrawalRequestItem struct {
	RiskClass       *string                  `json:"riskClass,omitempty"`
	RiskAccepted    bool                     `json:"riskAccepted"`
	Sources         []PortalWithdrawalSource `json:"sources,omitempty"`
	ID              string                   `json:"id"`
	Number          string                   `json:"number"`
	Date            string                   `json:"date"`
	Status          string                   `json:"status"`
	MerchantID      string                   `json:"merchantId"`
	MerchantName    string                   `json:"merchantName"`
	TokenID         string                   `json:"tokenId"`
	Symbol          string                   `json:"symbol"`
	Network         string                   `json:"network"`
	Amount          string                   `json:"amount"` // minor units
	DecimalPlaces   int                      `json:"decimalPlaces"`
	DestAddress     string                   `json:"destAddress"`
	ApprovedByID    *string                  `json:"approvedById,omitempty"`
	ApprovedByName  *string                  `json:"approvedByName,omitempty"`
	ApprovedAt      *string                  `json:"approvedAt,omitempty"`
	RejectionReason *string                  `json:"rejectionReason,omitempty"`
	WithdrawalID    *string                  `json:"withdrawalId,omitempty"`
	CreatedAt       string                   `json:"createdAt"`
}

// AdminWithdrawalRequestDetail represents the complete read-only document form.
type AdminWithdrawalRequestDetail struct {
	RiskClass       *string                        `json:"riskClass,omitempty"`
	RiskAccepted    bool                           `json:"riskAccepted"`
	Sources         []PortalWithdrawalSource       `json:"sources,omitempty"`
	ID              string                         `json:"id"`
	DeletionMark    bool                           `json:"deletionMark"`
	Version         int                            `json:"version"`
	Attributes      json.RawMessage                `json:"attributes"`
	CreatedAt       string                         `json:"createdAt"`
	UpdatedAt       string                         `json:"updatedAt"`
	CreatedBy       *string                        `json:"createdBy,omitempty"`
	UpdatedBy       *string                        `json:"updatedBy,omitempty"`
	Number          string                         `json:"number"`
	Date            string                         `json:"date"`
	Posted          bool                           `json:"posted"`
	PostedVersion   int                            `json:"postedVersion"`
	BasisType       string                         `json:"basisType,omitempty"`
	BasisID         *string                        `json:"basisId,omitempty"`
	Description     string                         `json:"description,omitempty"`
	Status          string                         `json:"status"`
	MerchantID      string                         `json:"merchantId"`
	MerchantName    string                         `json:"merchantName"`
	TokenID         string                         `json:"tokenId"`
	Symbol          string                         `json:"symbol"`
	Network         string                         `json:"network"`
	Amount          string                         `json:"amount"`
	DecimalPlaces   int                            `json:"decimalPlaces"`
	DestAddress     string                         `json:"destAddress"`
	AddressID       *string                        `json:"addressId,omitempty"`
	ApprovedBy      *string                        `json:"approvedBy,omitempty"`
	ApprovedByID    *string                        `json:"approvedById,omitempty"`
	ApprovedByName  *string                        `json:"approvedByName,omitempty"`
	ApprovedAt      *string                        `json:"approvedAt,omitempty"`
	RejectionReason *string                        `json:"rejectionReason,omitempty"`
	WithdrawalID    *string                        `json:"withdrawalId,omitempty"`
	Valuation       *WithdrawalRequestUSDValuation `json:"valuation,omitempty"`
}

// WithdrawalRequestUSDValuation is indicative and never changes the payout amount.
type WithdrawalRequestUSDValuation struct {
	Status      string  `json:"status"`
	Amount      *string `json:"amount"`
	Rate        *string `json:"rate,omitempty"`
	Multiplier  int     `json:"multiplier"`
	Source      *string `json:"source,omitempty"`
	RateAsOf    *string `json:"rateAsOf,omitempty"`
	EvaluatedAt string  `json:"evaluatedAt"`
}

// AdminWithdrawalRequestListResponse wraps the admin withdrawal request list.
type AdminWithdrawalRequestListResponse struct {
	Items       []AdminWithdrawalRequestItem `json:"items"`
	Total       int                          `json:"total"`
	NextCursor  string                       `json:"nextCursor,omitempty"`
	PrevCursor  string                       `json:"prevCursor,omitempty"`
	HasMore     bool                         `json:"hasMore"`
	HasPrev     bool                         `json:"hasPrev"`
	TargetIndex *int                         `json:"targetIndex,omitempty"`
	TotalCount  *int64                       `json:"totalCount"`
}

// AdminApproveWithdrawalResponse is returned after admin approval.
type AdminApproveWithdrawalResponse struct {
	Status string `json:"status"`
}

// ── Persistent Customer Deposit Addresses ─────────────────────────────

// PortalDepositAddressQuotaItem represents per-network persistent address quota and usage for a merchant.
type PortalDepositAddressQuotaItem struct {
	NetworkID   string   `json:"networkId"`
	NetworkCode string   `json:"networkCode"`
	NetworkName string   `json:"networkName"`
	Used        int      `json:"used"`
	Max         int      `json:"max"`
	Tokens      []string `json:"tokens"`
}

// PortalDepositAddressItem represents a persistent deposit address bound to a merchant customer.
type PortalDepositAddressItem struct {
	ID                    string                   `json:"id"`
	CustomerID            string                   `json:"customerId"`
	Address               string                   `json:"address"`
	NetworkID             string                   `json:"networkId"`
	NetworkCode           string                   `json:"networkCode"`
	NetworkName           string                   `json:"networkName"`
	Tokens                []string                 `json:"tokens"`
	TopUpCount            int                      `json:"topUpCount"`
	LastTopUpAt           *string                  `json:"lastTopUpAt,omitempty"`
	CreatedAt             string                   `json:"createdAt"`
	RiskClass             string                   `json:"riskClass"`
	RiskEnabled           bool                     `json:"riskEnabled"`
	RiskExpiresAt         *time.Time               `json:"riskExpiresAt,omitempty"`
	RiskAssets            []aml.WalletAssetDisplay `json:"riskAssets"`
	CanRotate             bool                     `json:"canRotate"`
	RotationBlockedReason string                   `json:"rotationBlockedReason"`
	NextRotationAt        *time.Time               `json:"nextRotationAt,omitempty"`
}

type PortalRotateDepositAddressRequest struct {
	Reason string `json:"reason" binding:"required,oneof=aml_high_risk customer_request"`
}

type PortalRotateDepositAddressResponse struct {
	PortalCreateDepositAddressResponse
	RotationID      string    `json:"rotationId"`
	OldWalletID     string    `json:"oldWalletId"`
	OldAddress      string    `json:"oldAddress"`
	ResourceVersion int       `json:"resourceVersion"`
	NextRotationAt  time.Time `json:"nextRotationAt"`
}

// PortalDepositAddressListResponse wraps the persistent deposit address list and network quotas.
type PortalDepositAddressListResponse struct {
	Items  []PortalDepositAddressItem      `json:"items"`
	Total  int                             `json:"total"`
	Quotas []PortalDepositAddressQuotaItem `json:"quotas"`
}

// PortalCreateDepositAddressRequest assigns or retrieves a persistent deposit address for a customer on a network.
type PortalCreateDepositAddressRequest struct {
	NetworkID  string `json:"networkId" binding:"required"`
	CustomerID string `json:"customerId" binding:"required"`
}

// PortalCreateDepositAddressResponse is returned after assigning or resolving a persistent deposit address.
type PortalCreateDepositAddressResponse struct {
	Object      string   `json:"object"`
	ID          string   `json:"id"`
	CustomerID  string   `json:"customerId"`
	Address     string   `json:"address"`
	NetworkID   string   `json:"networkId"`
	NetworkCode string   `json:"networkCode"`
	NetworkName string   `json:"networkName"`
	Tokens      []string `json:"tokens"`
	Reused      bool     `json:"reused"`
	CreatedAt   string   `json:"createdAt"`
	QRCode      string   `json:"qrCode,omitempty"`
}
