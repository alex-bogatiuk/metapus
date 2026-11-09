package cryptoapp

import (
	"context"
	"strings"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/tx"
	"metapus/internal/domain/crypto"
)

type FinancialAdjustment = crypto.FinancialAdjustment

type ProposeFinancialAdjustmentCommand struct {
	PaymentID      id.ID
	Mode           string
	WalletID       id.ID
	TokenID        id.ID
	Direction      string
	Amount         string
	SourceLines    []FinancialAdjustmentSourceSelection
	Direct         bool
	PostedVersion  int // Optional expected generation; captured under lock before persistence.
	ReasonCode     string
	Evidence       json.RawMessage
	IdempotencyKey string
	MakerID        id.ID
}

type FinancialAdjustmentSourceSelection struct {
	Register string `json:"register"`
	LineID   id.ID  `json:"lineId"`
	Amount   string `json:"amount"`
}

type FinancialAdjustmentSourceLine struct {
	Register  string `json:"register"`
	LineID    id.ID  `json:"lineId"`
	Amount    string `json:"amount"`
	Reversed  string `json:"reversed"`
	Available string `json:"available"`
	TokenID   id.ID  `json:"tokenId"`
	Dimension string `json:"dimension"`
}

type FinancialAdjustmentPaymentSources struct {
	PaymentID     id.ID                           `json:"paymentId"`
	PostedVersion int                             `json:"postedVersion"`
	Lines         []FinancialAdjustmentSourceLine `json:"lines"`
}

type FinancialAdjustmentRepository interface {
	PaymentBasis(context.Context, id.ID) (crypto.AdjustmentPaymentBasis, error)
	WalletTokenCompatible(context.Context, id.ID, id.ID) (bool, error)
	Propose(context.Context, ProposeFinancialAdjustmentCommand) (*FinancialAdjustment, error)
	LockForDecision(context.Context, id.ID) (*FinancialAdjustment, error)
	PostingBasis(context.Context, *FinancialAdjustment) (crypto.AdjustmentPaymentBasis, error)
	ApplyPosting(context.Context, crypto.AdjustmentPostingPlan) (*FinancialAdjustment, error)
	RejectPending(context.Context, id.ID, id.ID, string) (*FinancialAdjustment, error)
	Get(context.Context, id.ID) (*FinancialAdjustment, error)
	List(context.Context, string, int) ([]FinancialAdjustment, error)
	ListForPayment(context.Context, id.ID, string, int) ([]FinancialAdjustment, error)
	PaymentSources(context.Context, id.ID) (*FinancialAdjustmentPaymentSources, error)
	Sources(context.Context, id.ID) ([]FinancialAdjustmentSourceSelection, error)
}

type FinancialAdjustmentService struct {
	txm  tx.Manager
	repo FinancialAdjustmentRepository
}

func NewFinancialAdjustmentService(txm tx.Manager, repo FinancialAdjustmentRepository) *FinancialAdjustmentService {
	if txm == nil || repo == nil {
		panic("financial adjustment requires transaction manager and repository")
	}
	return &FinancialAdjustmentService{txm: txm, repo: repo}
}

func (s *FinancialAdjustmentService) Propose(ctx context.Context, cmd ProposeFinancialAdjustmentCommand) (*FinancialAdjustment, error) {
	cmd.ReasonCode = strings.TrimSpace(cmd.ReasonCode)
	cmd.IdempotencyKey = strings.TrimSpace(cmd.IdempotencyKey)
	var evidence map[string]any
	evidenceErr := json.Unmarshal(cmd.Evidence, &evidence)
	justification, _ := evidence["description"].(string)
	if cmd.Mode == "" {
		cmd.Mode = "payment_reversal"
	}
	validMode := cmd.Mode == "payment_reversal" && !id.IsNil(cmd.PaymentID) ||
		cmd.Mode == "wallet_balance" && id.IsNil(cmd.PaymentID) && !id.IsNil(cmd.WalletID) && !id.IsNil(cmd.TokenID) &&
			(cmd.Direction == "receipt" || cmd.Direction == "expense") && positiveRawAmount(cmd.Amount)
	if !validMode || id.IsNil(cmd.MakerID) ||
		len(cmd.ReasonCode) < 3 || cmd.IdempotencyKey == "" || len(cmd.IdempotencyKey) > 200 ||
		evidenceErr != nil || len(strings.TrimSpace(justification)) < 10 {
		return nil, apperror.NewValidation("adjustment mode, maker, reason, justification and idempotency key are required")
	}
	if cmd.Mode == "payment_reversal" {
		for _, line := range cmd.SourceLines {
			if id.IsNil(line.LineID) || !validAdjustmentRegister(line.Register) || !positiveRawAmount(line.Amount) {
				return nil, apperror.NewValidation("invalid selected payment movement or amount")
			}
		}
	} else if len(cmd.SourceLines) != 0 {
		return nil, apperror.NewValidation("wallet variance cannot select payment movements")
	}
	var result *FinancialAdjustment
	err := s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		var err error
		if cmd.Mode == "payment_reversal" {
			basis, loadErr := s.repo.PaymentBasis(txCtx, cmd.PaymentID)
			if loadErr != nil {
				return loadErr
			}
			if err := basis.ValidateAdjustmentGeneration(cmd.PostedVersion); err != nil {
				return err
			}
			cmd.PostedVersion = basis.Version
		} else {
			compatible, err := s.repo.WalletTokenCompatible(txCtx, cmd.WalletID, cmd.TokenID)
			if err != nil {
				return err
			}
			if !compatible {
				return apperror.NewValidation("wallet and token must exist on the same network")
			}
		}
		result, err = s.repo.Propose(txCtx, cmd)
		if err != nil {
			return err
		}
		if cmd.Direct && result.Status == "pending_approval" {
			result, err = s.repo.LockForDecision(txCtx, result.ID)
			if err != nil {
				return err
			}
			result, err = s.postLocked(txCtx, result, cmd.MakerID, "Проведено администратором после подтверждения входа", true)
		}
		return err
	})
	return result, err
}

func (s *FinancialAdjustmentService) PaymentSources(ctx context.Context, paymentID id.ID) (*FinancialAdjustmentPaymentSources, error) {
	if id.IsNil(paymentID) {
		return nil, apperror.NewValidation("payment ID is required")
	}
	return s.repo.PaymentSources(ctx, paymentID)
}

func (s *FinancialAdjustmentService) Sources(ctx context.Context, adjustmentID id.ID) ([]FinancialAdjustmentSourceSelection, error) {
	if id.IsNil(adjustmentID) {
		return nil, apperror.NewValidation("adjustment ID is required")
	}
	return s.repo.Sources(ctx, adjustmentID)
}

func validAdjustmentRegister(value string) bool {
	return value == "custody" || value == "merchant" || value == "fee"
}

func positiveRawAmount(value string) bool {
	if value == "" || len(value) > 78 || value[0] == '0' {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func (s *FinancialAdjustmentService) Approve(ctx context.Context, adjustmentID, checkerID id.ID, reason string) (*FinancialAdjustment, error) {
	if id.IsNil(adjustmentID) || id.IsNil(checkerID) || len(strings.TrimSpace(reason)) < 3 {
		return nil, apperror.NewValidation("adjustment, checker and decision reason are required")
	}
	var result *FinancialAdjustment
	err := s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		var err error
		state, lockErr := s.repo.LockForDecision(txCtx, adjustmentID)
		if lockErr != nil {
			return lockErr
		}
		result, err = s.postLocked(txCtx, state, checkerID, strings.TrimSpace(reason), false)
		return err
	})
	return result, err
}

func (s *FinancialAdjustmentService) Reject(ctx context.Context, adjustmentID, checkerID id.ID, reason string) (*FinancialAdjustment, error) {
	if id.IsNil(adjustmentID) || id.IsNil(checkerID) || len(strings.TrimSpace(reason)) < 3 {
		return nil, apperror.NewValidation("adjustment, checker and decision reason are required")
	}
	var result *FinancialAdjustment
	err := s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		var err error
		state, lockErr := s.repo.LockForDecision(txCtx, adjustmentID)
		if lockErr != nil {
			return lockErr
		}
		if decisionErr := state.CanDecide(checkerID, false); decisionErr != nil {
			return decisionErr
		}
		result, err = s.repo.RejectPending(txCtx, adjustmentID, checkerID, strings.TrimSpace(reason))
		return err
	})
	return result, err
}

func (s *FinancialAdjustmentService) Get(ctx context.Context, adjustmentID id.ID) (*FinancialAdjustment, error) {
	if id.IsNil(adjustmentID) {
		return nil, apperror.NewValidation("adjustment ID is required")
	}
	return s.repo.Get(ctx, adjustmentID)
}

func (s *FinancialAdjustmentService) List(ctx context.Context, status string, limit int) ([]FinancialAdjustment, error) {
	if status != "" && status != "pending_approval" && status != "posted" && status != "rejected" {
		return nil, apperror.NewValidation("invalid crypto adjustment status")
	}
	if limit < 1 || limit > 100 {
		return nil, apperror.NewValidation("crypto adjustment list limit must be between 1 and 100")
	}
	return s.repo.List(ctx, status, limit)
}

func (s *FinancialAdjustmentService) ListForPayment(ctx context.Context, paymentID id.ID, status string, limit int) ([]FinancialAdjustment, error) {
	if id.IsNil(paymentID) || (status != "" && status != "pending_approval" && status != "posted" && status != "rejected") || limit < 1 || limit > 100 {
		return nil, apperror.NewValidation("invalid payment adjustment filter")
	}
	return s.repo.ListForPayment(ctx, paymentID, status, limit)
}

func (s *FinancialAdjustmentService) postLocked(ctx context.Context, state *FinancialAdjustment, actor id.ID, reason string, direct bool) (*FinancialAdjustment, error) {
	if err := state.CanDecide(actor, direct); err != nil {
		return nil, err
	}
	basis, err := s.repo.PostingBasis(ctx, state)
	if err != nil {
		return nil, err
	}
	plan, err := state.PlanPosting(actor, reason, direct, basis)
	if err != nil {
		return nil, err
	}
	return s.repo.ApplyPosting(ctx, plan)
}
