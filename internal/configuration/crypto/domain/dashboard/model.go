// Package dashboard defines tenant-wide, read-only administration projections.
package dashboard

import (
	"context"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

type QueueItem struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Number        string    `json:"number"`
	Status        string    `json:"status"`
	Merchant      string    `json:"merchant"`
	TokenID       string    `json:"tokenId"`
	Symbol        string    `json:"symbol"`
	Network       string    `json:"network"`
	DecimalPlaces int       `json:"decimalPlaces"`
	Amount        string    `json:"amount"`
	CreatedAt     time.Time `json:"createdAt"`
}

type Queues struct {
	Withdrawals int         `json:"withdrawals"`
	Inbound     int         `json:"inbound"`
	Outflows    int         `json:"outflows"`
	Total       int         `json:"total"`
	Items       []QueueItem `json:"items"`
	ObservedAt  time.Time   `json:"observedAt"`
}

type Asset struct {
	TokenID       string `json:"tokenId"`
	NetworkID     string `json:"networkId"`
	Symbol        string `json:"symbol"`
	Network       string `json:"network"`
	NetworkColor  string `json:"networkColor"`
	DecimalPlaces int    `json:"decimalPlaces"`
	// MerchantMinor is what merchants can still withdraw. PayoutPendingMinor is
	// what open withdrawal requests already took from it but no wallet has paid
	// out yet. The platform owes both: ObligationMinor is their sum.
	MerchantMinor         string     `json:"merchantMinor"`
	PayoutPendingMinor    string     `json:"payoutPendingMinor"`
	ObligationMinor       string     `json:"obligationMinor"`
	PayoutPendingCount    int        `json:"payoutPendingCount"`
	WalletMinor           string     `json:"walletMinor"`
	ReconciledLedgerMinor *string    `json:"reconciledLedgerMinor"`
	ChainMinor            *string    `json:"chainMinor"`
	DeltaMinor            *string    `json:"deltaMinor"`
	WalletCount           int        `json:"walletCount"`
	CheckedWalletCount    int        `json:"checkedWalletCount"`
	MismatchedWalletCount int        `json:"mismatchedWalletCount"`
	ReconciledAt          *time.Time `json:"reconciledAt"`
	CurrencyID            *id.ID     `json:"-"`
	CurrencyCode          string     `json:"-"`
}

type Finance struct {
	Assets      []Asset                `json:"assets"`
	Obligations crypto.MerchantBalance `json:"obligations"`
	Wallets     crypto.MerchantBalance `json:"wallets"`
	ObservedAt  time.Time              `json:"observedAt"`
}

type ReceiptPoint struct {
	Day           string `json:"day"`
	TokenID       string `json:"tokenId"`
	Symbol        string `json:"symbol"`
	Network       string `json:"network"`
	DecimalPlaces int    `json:"decimalPlaces"`
	Deposits      string `json:"deposits"`
}

type AnalyticsAsset struct {
	TokenID              string `json:"tokenId"`
	Symbol               string `json:"symbol"`
	Network              string `json:"network"`
	DecimalPlaces        int    `json:"decimalPlaces"`
	FeesMinor            string `json:"feesMinor"`
	Created              int    `json:"created"`
	Paid                 int    `json:"paid"`
	Underpaid            int    `json:"underpaid"`
	Expired              int    `json:"expired"`
	Pending              int    `json:"pending"`
	AwaitingConfirmation int    `json:"awaitingConfirmation"`
	Reversed             int    `json:"reversed"`
	Cancelled            int    `json:"cancelled"`
}

type Analytics struct {
	Period     string           `json:"period"`
	FromDay    string           `json:"fromDay"`
	ToDay      string           `json:"toDay"`
	Assets     []AnalyticsAsset `json:"assets"`
	Receipts   []ReceiptPoint   `json:"receipts"`
	ObservedAt time.Time        `json:"observedAt"`
}

type Repository interface {
	Queues(context.Context, string, int) (Queues, error)
	Assets(context.Context) ([]Asset, error)
	Analytics(context.Context, string) (Analytics, error)
}
