// Package crypto provides the core business logic for cryptocurrency processing:
// blockchain event types, payment FSM, and event processor.
package crypto

import (
	"context"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// EventType defines the type of blockchain event.
type EventType int

const (
	_                      EventType = iota
	EventTypeTransfer                // token transfer detected
	EventTypeConfirmation            // additional confirmation received
	EventTypeReorg                   // chain reorganization detected
)

// BlockchainEvent is a normalized blockchain event from any chain watcher.
// Chain-specific adapters convert their native events into this format.
type BlockchainEvent struct {
	// Network identifier (matches BlockchainNetwork.Code, e.g., "tron_mainnet")
	Network string `json:"network"`

	// NetworkID is the resolved BlockchainNetwork UUID
	NetworkID id.ID `json:"networkId"`

	// TxHash is the blockchain transaction hash (unique per network)
	TxHash string `json:"txHash"`

	// FromAddress is the sender's address
	FromAddress string `json:"fromAddress"`

	// ToAddress is the recipient's address (matched against wallets)
	ToAddress string `json:"toAddress"`

	// TokenContract is the token contract address ("" for native token)
	TokenContract string `json:"tokenContract"`

	// Amount received in token minor units
	Amount types.CryptoAmount `json:"amount"`

	// BlockNumber where the transaction was included
	BlockNumber int64 `json:"blockNumber"`

	// Confirmations is the current confirmation count
	Confirmations int `json:"confirmations"`

	// RequiredConfs is the number of confirmations required for this network.
	// Set by the chain watcher from BlockchainNetwork.ConfirmationsNeeded.
	RequiredConfs int `json:"requiredConfs"`

	// EventType classifies the event
	EventType EventType `json:"eventType"`

	// Timestamp of the blockchain event
	Timestamp time.Time `json:"timestamp"`
}

// ChainWatcher defines the adapter interface for blockchain-specific watchers.
// Each supported blockchain (TRON, ETH, TON) implements this interface.
// The EventProcessor is chain-agnostic — it only consumes BlockchainEvent.
type ChainWatcher interface {
	// NetworkCode returns the identifier matching BlockchainNetwork.Code.
	NetworkCode() string

	// Start begins watching the given addresses for incoming transactions.
	// Emits events to the provided channel. Blocks until ctx is cancelled.
	// Must be safe for concurrent use — one goroutine per watcher.
	Start(ctx context.Context, addresses []string, events chan<- BlockchainEvent) error

	// GetConfirmations returns the current confirmation count for a tx.
	GetConfirmations(ctx context.Context, txHash string) (int, error)
}
