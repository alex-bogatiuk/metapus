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
