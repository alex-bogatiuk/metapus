// Package crypto_sweep provides the CryptoSweep document.
// CryptoSweep consolidates funds from pool wallets to the hot wallet.
// Auto-created by the Worker after invoice payments are confirmed.
// System document — visible only to administrators.
package crypto_sweep

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
)

// SweepStatus defines the lifecycle state of a sweep.
type SweepStatus string

const (
	SweepStatusCreated       SweepStatus = "created"        // batch created, waiting for signing
	SweepStatusSigned        SweepStatus = "signed"         // all transactions signed
	SweepStatusBroadcast     SweepStatus = "broadcast"      // transactions broadcast
	SweepStatusConfirmed     SweepStatus = "confirmed"      // all transactions confirmed
	SweepStatusPartialFailed SweepStatus = "partial_failed" // some transactions failed
)

// CryptoSweep represents a batch consolidation of pool wallet funds to hot wallet.
type CryptoSweep struct {
	entity.Document

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
}

// CryptoSweepLine represents a single pool wallet sweep.
type CryptoSweepLine struct {
	LineID       id.ID              `db:"line_id" json:"lineId"`
	LineNo       int                `db:"line_no" json:"lineNo" meta:"label:№"`
	WalletID     id.ID              `db:"wallet_id" json:"walletId" meta:"label:Кошелёк,ref:wallet"`
	Amount       types.CryptoAmount `db:"amount" json:"amount" meta:"label:Сумма"`
	NetworkFee   types.CryptoAmount `db:"network_fee" json:"networkFee" meta:"label:Комиссия"`
	TxHash       string             `db:"tx_hash" json:"txHash,omitempty" meta:"label:TX Hash"`
	Confirmed    bool               `db:"confirmed" json:"confirmed" meta:"label:Подтверждён"`
}

// NewCryptoSweep creates a new CryptoSweep in Created state.
func NewCryptoSweep(organizationID, tokenID, hotWalletID id.ID) *CryptoSweep {
	return &CryptoSweep{
		Document:    entity.NewDocument(organizationID),
		TokenID:     tokenID,
		HotWalletID: hotWalletID,
		TotalAmount: types.ZeroCryptoAmount(),
		TotalFee:    types.ZeroCryptoAmount(),
		Status:      SweepStatusCreated,
		Lines:       make([]CryptoSweepLine, 0),
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

// --- CurrencyAwareDoc stubs ---

func (s *CryptoSweep) GetCurrencyID() id.ID                    { return id.ID{} }
func (s *CryptoSweep) SetCurrencyID(_ id.ID)                    {}
func (s *CryptoSweep) ValidateCurrency(_ context.Context) error { return nil }
func (s *CryptoSweep) GetContractID() *id.ID                    { return nil }

// --- Postable interface ---

func (s *CryptoSweep) GetDocumentType() string { return "CryptoSweep" }

// GenerateCryptoBalanceMovements:
// - EXPENSE from each pool wallet (per line)
// - RECEIPT to hot wallet (total)
func (s *CryptoSweep) GenerateCryptoBalanceMovements(ctx context.Context) ([]entity.CryptoBalanceMovement, error) {
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

// GenerateCryptoFeeMovements creates fee movements for sweep network costs.
func (s *CryptoSweep) GenerateCryptoFeeMovements(ctx context.Context) ([]entity.CryptoFeeMovement, error) {
	if s.TotalFee.IsZero() {
		return nil, nil
	}

	newVersion := s.PostedVersion + 1

	// Sweep fees are system-level (no merchant). Use zero-value merchant_id.
	fee := entity.NewCryptoFeeMovement(
		s.ID,
		s.GetDocumentType(),
		newVersion,
		s.Date,
		entity.RecordTypeReceipt,
		id.ID{}, // system fee — no merchant
		s.TokenID,
		entity.FeeTypeSweep,
		s.TotalFee,
	)

	return []entity.CryptoFeeMovement{fee}, nil
}

func (s *CryptoSweep) GetLineCount() int { return len(s.Lines) }

// Compile-time interface checks.
var _ posting.Postable = (*CryptoSweep)(nil)
var _ posting.CryptoBalanceMovementSource = (*CryptoSweep)(nil)
var _ posting.CryptoFeeMovementSource = (*CryptoSweep)(nil)
var _ posting.LineCounter = (*CryptoSweep)(nil)
