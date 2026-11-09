package cryptoapp

import (
	"context"
	"errors"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tx"
	"metapus/internal/domain/crypto"
)

type NativeFundingSettlementRepository interface {
	GetNativeFundingForUpdate(context.Context, id.ID) (crypto.NativeFunding, error)
	PrepareNativeFundingPrincipal(context.Context, crypto.NativeFunding, crypto.ChainReceipt) error
	RecordNativeFundingPrincipal(context.Context, crypto.NativeFunding, crypto.ChainReceipt) error
	FinishNativeFunding(context.Context, id.ID, string, string, crypto.ChainReceipt) error
	RequestNativeFundingReconciliation(context.Context, crypto.NativeFunding) error
}

type NativeFundingFeeSettlement interface {
	Get(context.Context, string, id.ID, crypto.SigningPurpose, int) (crypto.FeeReservation, bool, error)
	Settle(context.Context, id.ID, crypto.ChainReceipt) error
}

// NativeFundingSettlementService is shared by the parent executor and the
// independent receipt poller. The RPC call precedes this command; fee, principal
// and terminal state commit together, under the funding aggregate lock.
type NativeFundingSettlementService struct {
	txm  tx.Manager
	repo NativeFundingSettlementRepository
	fees NativeFundingFeeSettlement
}

func NewNativeFundingSettlementService(txm tx.Manager, repo NativeFundingSettlementRepository, fees NativeFundingFeeSettlement) *NativeFundingSettlementService {
	if txm == nil || repo == nil || fees == nil {
		panic("native funding settlement requires transactional ports")
	}
	return &NativeFundingSettlementService{txm: txm, repo: repo, fees: fees}
}

func (s *NativeFundingSettlementService) Settle(ctx context.Context, fundingID id.ID, receipt crypto.ChainReceipt) (settled bool, err error) {
	if !receipt.FinanciallyFinal() {
		return false, nil
	}
	if receipt.ObservedAt.IsZero() {
		receipt.ObservedAt = time.Now().UTC()
	}
	var capErr error
	err = s.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		funding, err := s.repo.GetNativeFundingForUpdate(ctx, fundingID)
		if err != nil {
			return err
		}
		if err := funding.ValidateReceipt(receipt); err != nil {
			return err
		}
		if funding.Status == "settled" || funding.Status == "failed" {
			if funding.FeeReservationID == nil {
				return apperror.NewConflict("recognized funding fee reservation is missing")
			}
			if err := s.fees.Settle(ctx, *funding.FeeReservationID, receipt); err != nil {
				return err
			}
		}
		if funding.Status == "settled" {
			if receipt.Status != crypto.ReceiptSucceeded {
				return apperror.NewConflict("funding receipt conflicts with settled outcome")
			}
			settled = true
			return nil
		}
		if funding.Status == "failed" {
			if receipt.Status != crypto.ReceiptReverted {
				return apperror.NewConflict("funding receipt conflicts with failed outcome")
			}
			return nil
		}
		// Each funding round has a distinct document ID and its own first
		// authorization. funding.Attempt counts rounds of the parent operation.
		reservation, found, err := s.fees.Get(ctx, crypto.NativeFundingBasisType, funding.ID, crypto.SigningPurposeNativeFunding, 1)
		if err != nil {
			return err
		}
		if !found || funding.FeeReservationID == nil || reservation.ID != *funding.FeeReservationID ||
			reservation.NetworkID != funding.NetworkID || reservation.NativeTokenID != funding.NativeTokenID ||
			reservation.PayerWalletID != funding.SourceWalletID || reservation.TxHash != funding.TxHash ||
			reservation.ReservedPrincipal.Cmp(funding.Amount) != 0 {
			return apperror.NewConflict("native funding fee reservation identity mismatch")
		}
		if receipt.Fee.TotalFee.Cmp(reservation.MaxFee) > 0 {
			// The budget adapter commits an audited cap hold. Return its error
			// only AFTER the transaction, preserving the incident and no principal.
			capErr = s.fees.Settle(ctx, reservation.ID, receipt)
			if !errors.Is(capErr, crypto.ErrFeeBudgetExceeded) {
				if capErr != nil {
					return capErr
				}
				return apperror.NewConflict("native funding fee cap hold was not recorded")
			}
			if capErr == nil {
				capErr = apperror.NewConflict("native funding fee exceeds reserved cap")
			}
			return s.repo.FinishNativeFunding(ctx, funding.ID, "held", "actual funding fee exceeds reserved cap", receipt)
		}
		if receipt.Status == crypto.ReceiptSucceeded {
			if err := s.repo.PrepareNativeFundingPrincipal(ctx, funding, receipt); err != nil {
				return err
			}
		}
		if err := s.fees.Settle(ctx, reservation.ID, receipt); err != nil {
			return err
		}
		if receipt.Status == crypto.ReceiptReverted {
			if err := s.repo.FinishNativeFunding(ctx, funding.ID, "failed", "native funding transaction reverted", receipt); err != nil {
				return err
			}
			return s.repo.RequestNativeFundingReconciliation(ctx, funding)
		}
		if err := s.repo.RecordNativeFundingPrincipal(ctx, funding, receipt); err != nil {
			return err
		}
		if err := s.repo.FinishNativeFunding(ctx, funding.ID, "settled", "", receipt); err != nil {
			return err
		}
		if err := s.repo.RequestNativeFundingReconciliation(ctx, funding); err != nil {
			return err
		}
		settled = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return settled, capErr
}
