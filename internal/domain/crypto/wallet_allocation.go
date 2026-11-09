package crypto

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/documents/crypto_invoice"
)

// AllocationCloseReason records why a transient wallet allocation ended.
type AllocationCloseReason string

const (
	AllocationCloseExpired       AllocationCloseReason = "expired"
	AllocationCloseCancelled     AllocationCloseReason = "cancelled"
	AllocationCloseConfirmed     AllocationCloseReason = "confirmed"
	AllocationCloseOrphanedLease AllocationCloseReason = "orphaned_lease"
)

func (r AllocationCloseReason) Valid() bool {
	switch r {
	case AllocationCloseExpired, AllocationCloseCancelled, AllocationCloseConfirmed, AllocationCloseOrphanedLease:
		return true
	default:
		return false
	}
}

// InboxMatchStatus is the durable matching state of an inbound transfer event.
type InboxMatchStatus string

const (
	InboxReceived    InboxMatchStatus = "received"
	InboxMatched     InboxMatchStatus = "matched"
	InboxUnmatched   InboxMatchStatus = "unmatched"
	InboxNeedsReview InboxMatchStatus = "needs_review"
	InboxResolved    InboxMatchStatus = "resolved"
	InboxIgnored     InboxMatchStatus = "ignored"
	InboxPoison      InboxMatchStatus = "poison"
)

const (
	MatchReasonNoAllocation            = "no_allocation"
	MatchReasonWrongToken              = "wrong_token"
	MatchReasonOverlappingWindows      = "overlapping_windows"
	MatchReasonTimestampBeforeLease    = "timestamp_before_lease"
	MatchReasonDust                    = "dust"
	MatchReasonNonPositive             = "non_positive"
	MatchReasonUnknownToken            = "unknown_token"
	MatchReasonUnknownWallet           = "unknown_wallet"
	MatchReasonAdditionalSinglePayment = "additional_single_payment"
	MatchReasonRetiredDepositAddress   = "retired_deposit_address"
)

const (
	LeaseBlockedReasonWatcherLag = "watcher_lag"
)

// WalletAllocation is one transient lease of a pool address to an invoice.
type WalletAllocation struct {
	ID            id.ID                 `db:"id" json:"id"`
	WalletID      id.ID                 `db:"wallet_id" json:"walletId"`
	InvoiceID     id.ID                 `db:"invoice_id" json:"invoiceId"`
	MerchantID    id.ID                 `db:"merchant_id" json:"merchantId"`
	TokenID       id.ID                 `db:"token_id" json:"tokenId"`
	NetworkID     id.ID                 `db:"network_id" json:"networkId"`
	LeasedAt      time.Time             `db:"leased_at" json:"leasedAt"`
	ClosedAt      *time.Time            `db:"closed_at" json:"closedAt,omitempty"`
	CooldownUntil *time.Time            `db:"cooldown_until" json:"cooldownUntil,omitempty"`
	CloseReason   AllocationCloseReason `db:"close_reason" json:"closeReason"`
	Version       int                   `db:"version" json:"version"`
	CreatedAt     time.Time             `db:"created_at" json:"createdAt"`
	UpdatedAt     time.Time             `db:"updated_at" json:"updatedAt"`
}

// Covers reports whether a blockchain timestamp belongs to this allocation window.
// Open rows are [leased_at, +inf). Closed rows are [leased_at, cooldown_until] inclusive.
// There is no matcher skew before leased_at.
func (a WalletAllocation) Covers(ts time.Time) bool {
	if a.ClosedAt == nil {
		return !ts.Before(a.LeasedAt)
	}
	if ts.Before(a.LeasedAt) {
		return false
	}
	if a.CooldownUntil == nil {
		return false
	}
	return !ts.After(*a.CooldownUntil)
}

func (a WalletAllocation) IsOpen() bool {
	return a.ClosedAt == nil
}

// MatchStatus is the result of DecideMatch.
type MatchStatus string

const (
	MatchMatched     MatchStatus = "matched"
	MatchUnmatched   MatchStatus = "unmatched"
	MatchNeedsReview MatchStatus = "needs_review"
)

// MatchDecision is the pure attribution outcome for one inbound transfer.
type MatchDecision struct {
	Status     MatchStatus
	Reason     string
	Allocation *WalletAllocation
}

// DecideMatch attributes an event to at most one allocation.
// covering must be FindCovering(wallet, ts) with no token filter.
func DecideMatch(open *WalletAllocation, covering []WalletAllocation, tokenID id.ID, ts time.Time) MatchDecision {
	same := make([]WalletAllocation, 0, len(covering))
	for i := range covering {
		if covering[i].TokenID == tokenID {
			same = append(same, covering[i])
		}
	}
	if len(same) > 1 {
		return MatchDecision{Status: MatchNeedsReview, Reason: MatchReasonOverlappingWindows}
	}
	if len(same) == 1 {
		alloc := same[0]
		return MatchDecision{Status: MatchMatched, Allocation: &alloc}
	}
	if len(covering) > 0 {
		return MatchDecision{Status: MatchUnmatched, Reason: MatchReasonWrongToken}
	}
	if open != nil && ts.Before(open.LeasedAt) {
		return MatchDecision{Status: MatchNeedsReview, Reason: MatchReasonTimestampBeforeLease}
	}
	return MatchDecision{Status: MatchUnmatched, Reason: MatchReasonNoAllocation}
}

func InvoiceIsLateTerminal(status crypto_invoice.InvoiceStatus) bool {
	switch status {
	case crypto_invoice.InvoiceStatusExpired,
		crypto_invoice.InvoiceStatusCancelled,
		crypto_invoice.InvoiceStatusUnderpaid:
		return true
	default:
		return false
	}
}

// WalletAllocationRepository persists transient lease history.
type WalletAllocationRepository interface {
	Open(ctx context.Context, alloc WalletAllocation) error
	CloseOpenForWallet(ctx context.Context, walletID id.ID, reason AllocationCloseReason, closedAt, cooldownUntil time.Time) (*WalletAllocation, error)
	GetOpenByWallet(ctx context.Context, walletID id.ID) (*WalletAllocation, error)
	FindCovering(ctx context.Context, walletID id.ID, ts time.Time) ([]WalletAllocation, error)
}

// LeaseCloser closes a transient allocation and transitions wallet occupancy.
type LeaseCloser interface {
	CloseTransientLease(ctx context.Context, walletID id.ID, reason AllocationCloseReason, nextStatus wallet.WalletStatus) error
}

// InboundChainEvent is a durable inbound transfer before or after attribution.
type InboundChainEvent struct {
	ID             id.ID              `db:"id" json:"id"`
	NetworkID      id.ID              `db:"network_id" json:"networkId"`
	TxHash         string             `db:"tx_hash" json:"txHash"`
	EventKind      EventKind          `db:"event_kind" json:"eventKind"`
	EventIndex     int64              `db:"event_index" json:"eventIndex"`
	FromAddress    string             `db:"from_address" json:"fromAddress"`
	ToAddress      string             `db:"to_address" json:"toAddress"`
	TokenContract  string             `db:"token_contract" json:"tokenContract"`
	TokenID        *id.ID             `db:"token_id" json:"tokenId,omitempty"`
	WalletID       *id.ID             `db:"wallet_id" json:"walletId,omitempty"`
	Amount         types.CryptoAmount `db:"amount" json:"amount"`
	BlockNumber    int64              `db:"block_number" json:"blockNumber"`
	BlockHash      string             `db:"block_hash" json:"blockHash"`
	BlockTimestamp time.Time          `db:"block_timestamp" json:"blockTimestamp"`
	Confirmations  int                `db:"confirmations" json:"confirmations"`
	RequiredConfs  int                `db:"required_confs" json:"requiredConfs"`
	FinalityStatus string             `db:"finality_status" json:"finalityStatus"`
	Canonicality   string             `db:"canonicality" json:"canonicality"`
	EventType      string             `db:"event_type" json:"eventType"`
	Direction      string             `db:"direction" json:"direction"`
	Payload        json.RawMessage    `db:"payload" json:"payload"`
	MatchStatus    InboxMatchStatus   `db:"match_status" json:"matchStatus"`
	MatchReason    string             `db:"match_reason" json:"matchReason"`
	InvoiceID      *id.ID             `db:"invoice_id" json:"invoiceId,omitempty"`
	PaymentID      *id.ID             `db:"payment_id" json:"paymentId,omitempty"`
	AllocationID   *id.ID             `db:"allocation_id" json:"allocationId,omitempty"`
	ReceiptID      *id.ID             `db:"receipt_id" json:"receiptId,omitempty"`
	Details        json.RawMessage    `db:"details" json:"details"`
	RetryCount     int                `db:"retry_count" json:"retryCount"`
	NextRetryAt    *time.Time         `db:"next_retry_at" json:"nextRetryAt,omitempty"`
	Version        int                `db:"version" json:"version"`
	CreatedAt      time.Time          `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time          `db:"updated_at" json:"updatedAt"`
}

// ChainEventInbox persists inbound transfers before business matching.
type ChainEventInbox interface {
	Upsert(ctx context.Context, ev InboundChainEvent) (stored InboundChainEvent, inserted bool, err error)
	MarkMatched(ctx context.Context, eventID, invoiceID, paymentID, allocationID id.ID) error
	MarkSinglePaymentReview(ctx context.Context, eventID, invoiceID, allocationID id.ID) error
	NextSinglePaymentReview(ctx context.Context, invoiceID id.ID) (*InboundChainEvent, error)
	MarkTerminal(ctx context.Context, eventID id.ID, status InboxMatchStatus, reason string, details map[string]any) error
	ClaimReceivedForRetry(ctx context.Context, limit int, lease time.Duration) ([]InboundChainEvent, error)
	ListUnmatchedForReplay(ctx context.Context, reason string, limit int) ([]InboundChainEvent, error)
}

// RebuildBlockchainEvent restores the original watcher event from the inbox payload.
func RebuildBlockchainEvent(row InboundChainEvent) (BlockchainEvent, error) {
	if len(row.Payload) > 0 && string(row.Payload) != "{}" && string(row.Payload) != "null" {
		var event BlockchainEvent
		if err := json.Unmarshal(row.Payload, &event); err != nil {
			return BlockchainEvent{}, fmt.Errorf("decode inbound chain event payload: %w", err)
		}
		if event.TxHash != "" {
			event.Confirmations = row.Confirmations
			event.RequiredConfs = row.RequiredConfs
			event.Finality = FinalityStatus(row.FinalityStatus)
			event.Canonicality = CanonicalityStatus(row.Canonicality)
			return event, nil
		}
	}
	return BlockchainEvent{
		NetworkID:     row.NetworkID,
		TxHash:        row.TxHash,
		EventIndex:    row.EventIndex,
		EventKind:     row.EventKind,
		FromAddress:   row.FromAddress,
		ToAddress:     row.ToAddress,
		TokenContract: row.TokenContract,
		Amount:        row.Amount,
		BlockNumber:   row.BlockNumber,
		BlockHash:     row.BlockHash,
		Finality:      FinalityStatus(row.FinalityStatus),
		Canonicality:  CanonicalityStatus(row.Canonicality),
		Confirmations: row.Confirmations,
		RequiredConfs: row.RequiredConfs,
		EventType:     EventTypeTransfer,
		Direction:     TransferDirection(row.Direction),
		Timestamp:     row.BlockTimestamp,
	}, nil
}

// NewInboundChainEvent builds an inbox row from a watcher transfer event.
func NewInboundChainEvent(event BlockchainEvent, walletID *id.ID, tokenID *id.ID) (InboundChainEvent, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return InboundChainEvent{}, fmt.Errorf("marshal blockchain event: %w", err)
	}
	kind := event.EffectiveEventKind()
	return InboundChainEvent{
		NetworkID:      event.NetworkID,
		TxHash:         event.TxHash,
		EventKind:      kind,
		EventIndex:     event.EventIndex,
		FromAddress:    event.FromAddress,
		ToAddress:      event.ToAddress,
		TokenContract:  event.TokenContract,
		TokenID:        tokenID,
		WalletID:       walletID,
		Amount:         event.Amount,
		BlockNumber:    event.BlockNumber,
		BlockHash:      event.BlockHash,
		BlockTimestamp: event.Timestamp,
		Confirmations:  event.Confirmations,
		RequiredConfs:  event.RequiredConfs,
		FinalityStatus: string(event.Finality),
		Canonicality:   string(event.Canonicality),
		EventType:      "transfer",
		Direction:      string(event.Direction),
		Payload:        payload,
		MatchStatus:    InboxReceived,
		Details:        json.RawMessage(`{}`),
		Version:        1,
	}, nil
}
