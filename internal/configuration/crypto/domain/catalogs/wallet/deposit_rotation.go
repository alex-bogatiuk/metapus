package wallet

import (
	"context"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

const DepositRotationCooldown = 24 * time.Hour

// DepositRotationCommand rotates one network's customer address. The key belongs
// to the merchant, and retries must carry the same wallet and reason.
type DepositRotationCommand struct {
	MerchantID     id.ID
	WalletID       id.ID
	ActorID        id.ID
	Reason         string
	IdempotencyKey string
}

func (c DepositRotationCommand) Validate() error {
	if id.IsNil(c.MerchantID) || id.IsNil(c.WalletID) || id.IsNil(c.ActorID) {
		return apperror.NewValidation("merchant, address and actor are required")
	}
	if c.Reason != "aml_high_risk" && c.Reason != "customer_request" {
		return apperror.NewValidation("invalid deposit address rotation reason").WithDetail("field", "reason")
	}
	if len(c.IdempotencyKey) < 1 || len(c.IdempotencyKey) > 128 || strings.TrimSpace(c.IdempotencyKey) != c.IdempotencyKey {
		return apperror.NewValidation("Idempotency-Key must contain between 1 and 128 characters")
	}
	return nil
}

// DepositRotation is an immutable assignment snapshot. Later wallet updates do
// not change the response returned to a retry or the merchant webhook payload.
type DepositRotation struct {
	ID             id.ID     `db:"id"`
	MerchantID     id.ID     `db:"merchant_id"`
	NetworkID      id.ID     `db:"network_id"`
	NetworkCode    string    `db:"network_code"`
	NetworkName    string    `db:"network_name"`
	Tokens         []string  `db:"tokens"`
	CustomerID     string    `db:"customer_id"`
	OldWalletID    id.ID     `db:"old_wallet_id"`
	NewWalletID    id.ID     `db:"new_wallet_id"`
	OldAddress     string    `db:"old_address"`
	NewAddress     string    `db:"new_address"`
	Reason         string    `db:"reason"`
	ActorID        id.ID     `db:"actor_id"`
	IdempotencyKey string    `db:"idempotency_key"`
	Version        int       `db:"version"`
	CreatedAt      time.Time `db:"created_at"`
	Replayed       bool      `db:"-"`
}

type DepositRotationRepository interface {
	RotatePersistentAddress(context.Context, DepositRotationCommand) (*DepositRotation, error)
}
