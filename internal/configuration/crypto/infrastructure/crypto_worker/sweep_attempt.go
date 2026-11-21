package crypto_worker

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// CryptoSweepAttempt is worker control-plane state for one wallet/token sweep.
type CryptoSweepAttempt struct {
	ID                 id.ID           `db:"id"`
	WalletID           id.ID           `db:"wallet_id"`
	TokenID            id.ID           `db:"token_id"`
	NetworkID          id.ID           `db:"network_id"`
	HotWalletID        id.ID           `db:"hot_wallet_id"`
	SourceAddress      string          `db:"source_address"`
	DestinationAddress string          `db:"destination_address"`
	Amount             types.RawAmount `db:"amount"`
	Status             string          `db:"status"`
	ErrorMessage       string          `db:"error_message"`
	ClaimedAt          time.Time       `db:"claimed_at"`
	BroadcastAt        *time.Time      `db:"broadcast_at"`
	PostedAt           *time.Time      `db:"posted_at"`
	ExpiresAt          time.Time       `db:"expires_at"`
	CreatedAt          time.Time       `db:"created_at"`
	UpdatedAt          time.Time       `db:"updated_at"`
}
