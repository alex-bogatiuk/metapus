package dto

import "time"

// UnidentifiedReceiptCredit is the latest merchant credit of a receipt.
type UnidentifiedReceiptCredit struct {
	ID               string     `json:"id"`
	Number           string     `json:"number"`
	Status           string     `json:"status"`
	Version          int        `json:"version"`
	MerchantID       string     `json:"merchantId"`
	MerchantName     string     `json:"merchantName"`
	NetAmount        string     `json:"netAmount"`
	ProcessingFee    string     `json:"processingFee"`
	ApprovalRequired bool       `json:"approvalRequired"`
	PreparedBy       string     `json:"preparedBy"`
	DecidedAt        *time.Time `json:"decidedAt"`
	DecisionReason   string     `json:"decisionReason"`
}

type UnidentifiedReceiptItem struct {
	// ReceiptID is empty while the transfer is observed but not accounted yet.
	ReceiptID         string                     `json:"receiptId"`
	InboxEventID      string                     `json:"inboxEventId"`
	Number            string                     `json:"number"`
	ReceivedAt        time.Time                  `json:"receivedAt"`
	NetworkID         string                     `json:"networkId"`
	NetworkName       string                     `json:"networkName"`
	TokenID           string                     `json:"tokenId"`
	TokenSymbol       string                     `json:"tokenSymbol"`
	DecimalPlaces     int                        `json:"decimalPlaces"`
	WalletID          string                     `json:"walletId"`
	ToAddress         string                     `json:"toAddress"`
	FromAddress       string                     `json:"fromAddress"`
	TxHash            string                     `json:"txHash"`
	Amount            string                     `json:"amount"`
	Remainder         string                     `json:"remainder"`
	Reason            string                     `json:"reason"`
	State             string                     `json:"state"`
	CustodyMerchantID string                     `json:"custodyMerchantId"`
	InvoiceID         string                     `json:"invoiceId"`
	PendingError      string                     `json:"pendingError"`
	Credit            *UnidentifiedReceiptCredit `json:"credit"`
	// PaymentID is the invoice payment of a matched receipt. While that payment
	// is unposted after a reorg the state is payment_reorged and no manual
	// credit is possible.
	PaymentID     string `json:"paymentId"`
	PaymentNumber string `json:"paymentNumber"`
	PaymentStatus string `json:"paymentStatus"`
}

type CustodyReceiptProofSource struct {
	ProviderID string `json:"providerId"`
	TrustGroup string `json:"trustGroup"`
	HeadHeight int64  `json:"headHeight"`
}

// CustodyReceiptProof is the canonical proof the receipt audit rests on.
// Original is false after a re-included transfer moved the audit to a new block.
type CustodyReceiptProof struct {
	VerificationID string                      `json:"verificationId"`
	Original       bool                        `json:"original"`
	TxHash         string                      `json:"txHash"`
	EventKind      string                      `json:"eventKind"`
	EventIndex     int64                       `json:"eventIndex"`
	BlockNumber    int64                       `json:"blockNumber"`
	BlockHash      string                      `json:"blockHash"`
	Confirmations  int64                       `json:"confirmations"`
	VerifiedAt     time.Time                   `json:"verifiedAt"`
	Sources        []CustodyReceiptProofSource `json:"sources"`
}

type CustodyReceiptAudit struct {
	Status        string     `json:"status"`
	LastCheckedAt *time.Time `json:"lastCheckedAt"`
	NextCheckAt   time.Time  `json:"nextCheckAt"`
	Attempts      int        `json:"attempts"`
	LastError     string     `json:"lastError"`
	OrphanedAt    *time.Time `json:"orphanedAt"`
	IncidentID    string     `json:"incidentId"`
}

// CustodyReceiptAttribution is a payment or merchant credit of the receipt.
// Outstanding is what it still holds after reorg and reversal compensations.
type CustodyReceiptAttribution struct {
	DocumentType  string    `json:"documentType"`
	DocumentID    string    `json:"documentId"`
	Number        string    `json:"number"`
	Date          time.Time `json:"date"`
	Status        string    `json:"status"`
	Posted        bool      `json:"posted"`
	MerchantID    string    `json:"merchantId"`
	MerchantName  string    `json:"merchantName"`
	Amount        string    `json:"amount"`
	NetAmount     string    `json:"netAmount"`
	ProcessingFee string    `json:"processingFee"`
	Outstanding   string    `json:"outstanding"`
}

type CustodyReceiptDetailResponse struct {
	Receipt      UnidentifiedReceiptItem     `json:"receipt"`
	Proof        CustodyReceiptProof         `json:"proof"`
	Audit        CustodyReceiptAudit         `json:"audit"`
	Attributions []CustodyReceiptAttribution `json:"attributions"`
}

type UnidentifiedReceiptListResponse struct {
	Items []UnidentifiedReceiptItem `json:"items"`
	Total int                       `json:"total"`
}

type PrepareMerchantCreditRequest struct {
	MerchantID string  `json:"merchantId" binding:"required"`
	InvoiceID  *string `json:"invoiceId"`
}

type DecideMerchantCreditRequest struct {
	Version int    `json:"version" binding:"required"`
	Reason  string `json:"reason"`
}

type MerchantCreditResponse struct {
	ID               string `json:"id"`
	Number           string `json:"number"`
	ReceiptID        string `json:"receiptId"`
	MerchantID       string `json:"merchantId"`
	Status           string `json:"status"`
	Version          int    `json:"version"`
	ApprovalRequired bool   `json:"approvalRequired"`
	Amount           string `json:"amount"`
	NetAmount        string `json:"netAmount"`
	ProcessingFee    string `json:"processingFee"`
}
