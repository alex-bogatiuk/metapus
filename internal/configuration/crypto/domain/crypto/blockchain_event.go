// Package crypto provides the core business logic for cryptocurrency processing:
// blockchain event types, payment FSM, and event processor.
package crypto

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// EventType defines the type of blockchain event.
type EventType int

const (
	_                     EventType = iota
	EventTypeTransfer               // token transfer detected
	EventTypeConfirmation           // additional confirmation received
	EventTypeReorg                  // chain reorganization detected
)

// TransferDirection describes how a chain transfer relates to managed wallets.
type TransferDirection string

const (
	TransferDirectionUnknown  TransferDirection = ""
	TransferDirectionIncoming TransferDirection = "incoming"
	TransferDirectionOutgoing TransferDirection = "outgoing"
	TransferDirectionInternal TransferDirection = "internal"
)

// EventKind separates transaction-value transfers from contract logs. Both
// can legitimately have event index zero in the same transaction.
type EventKind string

const (
	EventKindNativeValue   EventKind = "native_value"
	EventKindContractLog   EventKind = "contract_log"
	EventKindInternalValue EventKind = "internal_value"
)

func (k EventKind) Valid() bool {
	return k == EventKindNativeValue || k == EventKindContractLog || k == EventKindInternalValue
}

// BlockchainEvent is a normalized blockchain event from any chain watcher.
// Chain-specific adapters convert their native events into this format.
type BlockchainEvent struct {
	// NetworkID is the only runtime routing key. Display codes and
	// chain-specific identifiers must be resolved before an event is emitted.
	NetworkID id.ID `json:"networkId"`

	// TxHash is the blockchain transaction hash (unique per network)
	TxHash string `json:"txHash"`

	// EventIndex preserves the source's legacy index: transaction-local on
	// TRON, block-global for EVM logs. AccountingEffectIdentity carries the
	// verified transaction-local ordinal without rewriting this wire contract.
	EventIndex int64 `json:"eventIndex"`

	EventKind EventKind `json:"eventKind"`
	// DecoderVersion stores the evidence schema under its frozen JSON name.
	// TRON adapters set it explicitly; zero is the EVM watcher form and reads
	// as schema 1 (see EffectiveEvidenceSchema).
	DecoderVersion int `json:"decoderVersion,omitempty"`
	// Trace metadata is audit evidence, independent of the stable event index.
	TraceHash      string `json:"traceHash,omitempty"`
	TraceIndex     *int64 `json:"traceIndex,omitempty"`
	CallValueIndex *int64 `json:"callValueIndex,omitempty"`
	VerificationID *id.ID `json:"verificationId,omitempty"`

	// FromAddress is the sender's address
	FromAddress string `json:"fromAddress"`

	// SourceAddress/SourceNonce identify the EVM transaction origin. For token
	// logs SourceAddress can differ from the Transfer event's FromAddress.
	SourceAddress string `json:"sourceAddress,omitempty"`
	SourceNonce   *int64 `json:"sourceNonce,omitempty"`

	// ToAddress is the recipient's address (matched against wallets)
	ToAddress string `json:"toAddress"`

	// TokenContract is the token contract address ("" for native token)
	TokenContract string `json:"tokenContract"`

	// Amount received in token minor units
	Amount types.RawAmount `json:"amount"`

	// BlockNumber where the transaction was included
	BlockNumber int64 `json:"blockNumber"`

	BlockHash string `json:"blockHash,omitempty"`

	Finality FinalityStatus `json:"finality"`

	Canonicality      CanonicalityStatus   `json:"canonicality"`
	ObservationMode   string               `json:"observationMode,omitempty"`
	ComparisonSources []VerificationSource `json:"comparisonSources,omitempty"`

	// Confirmations is the current confirmation count
	Confirmations int `json:"confirmations"`

	// RequiredConfs is the number of confirmations required for this network.
	// Set by the chain watcher from BlockchainNetwork.ConfirmationsNeeded.
	RequiredConfs int `json:"requiredConfs"`

	// EventType classifies the event
	EventType EventType `json:"eventType"`

	// Direction classifies whether managed wallets are sender, recipient, or both.
	Direction TransferDirection `json:"direction,omitempty"`

	// Timestamp of the blockchain event
	Timestamp time.Time `json:"timestamp"`
}

// EffectiveEventKind keeps old adapters and persisted replay payloads
// deterministic while new adapters set EventKind explicitly.
func (e BlockchainEvent) EffectiveEventKind() EventKind {
	if e.EventKind.Valid() {
		return e.EventKind
	}
	if e.TokenContract == "" {
		return EventKindNativeValue
	}
	return EventKindContractLog
}
