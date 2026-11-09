// Package crypto_sweep provides the CryptoSweep document.
// CryptoSweep consolidates funds from pool wallets to the hot wallet.
// Auto-created by the Worker after invoice payments are confirmed.
// System document — visible only to administrators.
package crypto_sweep

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/outbound_settlement"
	"metapus/internal/domain/posting"
)

// SweepStatus defines the lifecycle state of a sweep.
type SweepStatus string

const DocumentType = "CryptoSweep"

const (
	SweepStatusCreated       SweepStatus = "created"        // batch created, waiting for signing
	SweepStatusSigned        SweepStatus = "signed"         // all transactions signed
	SweepStatusBroadcast     SweepStatus = "broadcast"      // transactions broadcast
	SweepStatusConfirmed     SweepStatus = "confirmed"      // all transactions confirmed
	SweepStatusPartialFailed SweepStatus = "partial_failed" // some transactions failed
	SweepStatusHeld          SweepStatus = "held"           // execution needs retry or manual reconciliation
)

// CryptoSweep represents a batch consolidation of pool wallet funds to hot wallet.
type CryptoSweep struct {
	entity.Document

	// Version 2 recognizes each principal only at final settlement. Version 1
	// retains historical broadcast-time movements while its attempts drain.
	AccountingModelVersion int `db:"accounting_model_version" json:"accountingModelVersion"`

	// TokenID — token being swept
	TokenID id.ID `db:"token_id" json:"tokenId" meta:"label:Токен,ref:token"`

	// HotWalletID — destination hot wallet
	HotWalletID id.ID `db:"hot_wallet_id" json:"hotWalletId" meta:"label:Hot Wallet,ref:wallet"`

	// TotalAmount is the sum of all sweep line amounts
	TotalAmount types.CryptoAmount `db:"total_amount" json:"totalAmount" meta:"label:Итого"`

	// TotalFee is the sum of all network fees
	TotalFee types.CryptoAmount `db:"total_fee" json:"totalFee" meta:"label:Комиссия"`

	// Status is the current lifecycle state
	Status SweepStatus `db:"status" json:"status" meta:"label:Статус"`

	// Lines — one per pool wallet being swept
	Lines []CryptoSweepLine `db:"-" json:"lines" meta:"label:Кошельки"`

	// PlatformCostLines are platform-borne resource costs recorded during posting.
	PlatformCostLines []CryptoSweepPlatformCostLine `db:"-" json:"platformCostLines,omitempty" meta:"label:Расходы платформы"`
}

// CryptoSweepLine represents a single pool wallet sweep.
type CryptoSweepLine struct {
	LineID                   id.ID                           `db:"line_id" json:"lineId"`
	LineNo                   int                             `db:"line_no" json:"lineNo" meta:"label:№"`
	WalletID                 id.ID                           `db:"wallet_id" json:"walletId" meta:"label:Кошелёк,ref:wallet"`
	Amount                   types.CryptoAmount              `db:"amount" json:"amount" meta:"label:Сумма"`
	NetworkFee               types.CryptoAmount              `db:"network_fee" json:"networkFee" meta:"label:Комиссия"`
	TxHash                   string                          `db:"tx_hash" json:"txHash,omitempty" meta:"label:TX Hash"`
	Confirmed                bool                            `db:"confirmed" json:"confirmed" meta:"label:Подтверждён"`
	NetworkFeeTokenID        *id.ID                          `db:"network_fee_token_id" json:"networkFeeTokenId,omitempty"`
	SettlementStatus         outbound_settlement.Status      `db:"settlement_status" json:"settlementStatus"`
	SettlementCheckedAt      *time.Time                      `db:"settlement_checked_at" json:"settlementCheckedAt,omitempty"`
	SettlementError          string                          `db:"settlement_error" json:"settlementError,omitempty"`
	SettlementClaimedBy      string                          `db:"settlement_claimed_by" json:"-"`
	SettlementClaimedAt      *time.Time                      `db:"settlement_claimed_at" json:"-"`
	SettlementLeaseExpiresAt *time.Time                      `db:"settlement_lease_expires_at" json:"-"`
	ChainResult              outbound_settlement.ChainResult `db:"chain_result" json:"chainResult,omitempty"`
	EnergyUsed               int64                           `db:"energy_used" json:"energyUsed"`
	NetFee                   types.CryptoAmount              `db:"net_fee" json:"netFee"`
	EnergyFee                types.CryptoAmount              `db:"energy_fee" json:"energyFee"`
	NetUsage                 int64                           `db:"net_usage" json:"netUsage"`
}

// CryptoSweepPlatformCostLine represents one platform-borne resource cost.
type CryptoSweepPlatformCostLine struct {
	NetworkID       id.ID                   `json:"networkId"`
	CostTokenID     id.ID                   `json:"costTokenId"`
	CostType        entity.PlatformCostType `json:"costType"`
	Provider        string                  `json:"provider"`
	ExternalOrderID string                  `json:"externalOrderId"`
	Amount          types.CryptoAmount      `json:"amount"`
}

// NewCryptoSweep creates a new CryptoSweep in Created state.
func NewCryptoSweep(tokenID, hotWalletID id.ID) *CryptoSweep {
	return &CryptoSweep{
		Document:               entity.NewDocument(),
		AccountingModelVersion: 2,
		TokenID:                tokenID,
		HotWalletID:            hotWalletID,
		TotalAmount:            types.ZeroCryptoAmount(),
		TotalFee:               types.ZeroCryptoAmount(),
		Status:                 SweepStatusCreated,
		Lines:                  make([]CryptoSweepLine, 0),
	}
}

// Validate implements entity.Validatable.
func (s *CryptoSweep) Validate(ctx context.Context) error {
	if err := s.Document.Validate(ctx); err != nil {
		return err
	}

	if id.IsNil(s.TokenID) {
		return apperror.NewValidation("token is required").WithDetail("field", "tokenId")
	}
	if id.IsNil(s.HotWalletID) {
		return apperror.NewValidation("hot wallet is required").WithDetail("field", "hotWalletId")
	}
	if len(s.Lines) == 0 {
		return apperror.NewValidation("at least one sweep line is required")
	}
	return nil
}

// --- LinesAccessor ---

func (s *CryptoSweep) GetLines() []CryptoSweepLine {
	out := make([]CryptoSweepLine, len(s.Lines))
	copy(out, s.Lines)
	return out
}

func (s *CryptoSweep) SetLines(lines []CryptoSweepLine) {
	s.Lines = make([]CryptoSweepLine, len(lines))
	copy(s.Lines, lines)
}

// --- Postable interface ---

func (s *CryptoSweep) GetDocumentType() string { return DocumentType }

// GenerateCryptoBalanceMovements:
// - EXPENSE from each pool wallet (per line)
// - RECEIPT to hot wallet (total)
func (s *CryptoSweep) GenerateCryptoBalanceMovements(ctx context.Context) ([]entity.CryptoBalanceMovement, error) {
	if s.AccountingModelVersion == 2 {
		return nil, nil
	}
	if s.TotalAmount.IsZero() {
		return nil, nil
	}

	newVersion := s.PostedVersion + 1
	movements := make([]entity.CryptoBalanceMovement, 0, len(s.Lines)+1)

	// Expense from each pool wallet
	for _, line := range s.Lines {
		if line.Amount.IsZero() {
			continue
		}
		movements = append(movements, entity.NewCryptoBalanceMovement(
			s.ID,
			s.GetDocumentType(),
			newVersion,
			s.Date,
			entity.RecordTypeExpense,
			line.WalletID,
			s.TokenID,
			line.Amount,
		))
	}

	// Receipt to hot wallet
	movements = append(movements, entity.NewCryptoBalanceMovement(
		s.ID,
		s.GetDocumentType(),
		newVersion,
		s.Date,
		entity.RecordTypeReceipt,
		s.HotWalletID,
		s.TokenID,
		s.TotalAmount,
	))

	return movements, nil
}

// GenerateCryptoFeeMovements intentionally does not record sweep resource costs
// into CryptoFee: that register represents platform fee revenue/balances.
func (s *CryptoSweep) GenerateCryptoFeeMovements(ctx context.Context) ([]entity.CryptoFeeMovement, error) {
	return nil, nil
}

// GenerateCryptoPlatformCostMovements records platform-borne sweep resource costs.
func (s *CryptoSweep) GenerateCryptoPlatformCostMovements(ctx context.Context) ([]entity.CryptoPlatformCostMovement, error) {
	if len(s.PlatformCostLines) == 0 {
		return nil, nil
	}

	newVersion := s.PostedVersion + 1
	movements := make([]entity.CryptoPlatformCostMovement, 0, len(s.PlatformCostLines))
	for _, line := range s.PlatformCostLines {
		if line.Amount.IsZero() {
			continue
		}
		movements = append(movements, entity.NewCryptoPlatformCostMovement(
			s.ID,
			s.GetDocumentType(),
			newVersion,
			s.Date,
			entity.RecordTypeExpense,
			line.NetworkID,
			s.TokenID,
			line.CostTokenID,
			line.CostType,
			entity.PlatformCostPurposeSweep,
			line.Provider,
			line.ExternalOrderID,
			line.Amount,
		))
	}
	return movements, nil
}

func (s *CryptoSweep) GetLineCount() int { return len(s.Lines) }

// Chain-owned financial effects may only be reversed by a compensating event.
func (s *CryptoSweep) CanUnpost(context.Context) error {
	return apperror.NewConflict("crypto sweep cannot be unposted; create a compensating adjustment")
}

// Compile-time interface checks.
var _ posting.Postable = (*CryptoSweep)(nil)
var _ posting.UnpostGuard = (*CryptoSweep)(nil)
var _ posting.CryptoBalanceMovementSource = (*CryptoSweep)(nil)
var _ posting.CryptoFeeMovementSource = (*CryptoSweep)(nil)
var _ posting.CryptoPlatformCostMovementSource = (*CryptoSweep)(nil)
var _ posting.LineCounter = (*CryptoSweep)(nil)
