package outbound_repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"metapus/internal/application/outboundapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

type FeeReservationRequest = outboundapp.FeeReservationRequest

type FeeBudgetManager struct{ observer FeeBudgetObserver }

// Keep FeeBudget's serializable contract while reserving both activation
// commitments atomically. Actor state/dispatch transactions remain read committed.
func (m *FeeBudgetManager) ReserveActivation(ctx context.Context, task AccountActivationTask, req FeeReservationRequest) (crypto.FeeReservation, error) {
	if req.Purpose != crypto.SigningPurposeActivation || req.BasisType != "CryptoAccountActivationTask" || req.BasisID != task.ActivationID || req.NetworkID != task.NetworkID || req.PayerWalletID != task.HotWallet.ID || req.NativeTokenID != task.CostTokenID || req.OperationTokenID != task.CostTokenID || req.Attempt != 1 || req.Strategy != crypto.FeeStrategyNativeBurn || !req.ReservedPrincipal.IsZero() {
		return crypto.FeeReservation{}, apperror.NewConflict("activation fee reservation differs from its claim identity")
	}
	var reserved crypto.FeeReservation
	err := postgres.MustGetTxManager(ctx).RunInTransactionWithOptions(ctx, postgres.SerializableTxOptions(), func(ctx context.Context) error {
		if err := NewOutboundRepo().ReserveAccountActivationBudget(ctx, task); err != nil {
			return err
		}
		var err error
		reserved, err = m.Reserve(ctx, req)
		return err
	})
	return reserved, err
}

func (m *FeeBudgetManager) Get(ctx context.Context, basisType string, basisID id.ID, purpose crypto.SigningPurpose, attempt int) (crypto.FeeReservation, bool, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var r crypto.FeeReservation
	var rawPurpose, strategy, status string
	err := q.QueryRow(ctx, `
		SELECT id,network_id,payer_wallet_id,native_token_id,operation_token_id,reserved_principal_minor,basis_type,basis_id,purpose,attempt,strategy,
		       max_fee_minor,actual_fee_minor,unsigned_fingerprint,tx_hash,status,hold_reason,reserved_at
		FROM reg_crypto_fee_reservations
		WHERE basis_type=$1 AND basis_id=$2 AND purpose=$3 AND attempt=$4
	`, basisType, basisID, purpose, attempt).Scan(
		&r.ID, &r.NetworkID, &r.PayerWalletID, &r.NativeTokenID, &r.OperationTokenID, &r.ReservedPrincipal, &r.BasisType, &r.BasisID,
		&rawPurpose, &r.Attempt, &strategy, &r.MaxFee, &r.ActualFee, &r.UnsignedFingerprint,
		&r.TxHash, &status, &r.HoldReason, &r.ReservedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	r.Purpose, r.Strategy, r.Status = crypto.SigningPurpose(rawPurpose), crypto.FeeStrategy(strategy), crypto.FeeReservationStatus(status)
	return r, true, nil
}

func (m *FeeBudgetManager) LoadPolicy(ctx context.Context, networkID id.ID) (crypto.NetworkPolicy, error) {
	return scanNetworkPolicy(postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, networkPolicySelect+` WHERE p.network_id=$1 AND p._deleted_at IS NULL`, networkID))
}

func (m *FeeBudgetManager) Reserve(ctx context.Context, req FeeReservationRequest) (reserved crypto.FeeReservation, err error) {
	var budgetRemaining *types.CryptoAmount
	finish := func(error, *types.CryptoAmount) {}
	if m != nil && m.observer != nil {
		ctx, finish = m.observer.Reserve(ctx, req)
	}
	defer func() { finish(err, budgetRemaining) }()
	if err := validateFeeReservationRequest(req); err != nil {
		return crypto.FeeReservation{}, err
	}
	err = postgres.MustGetTxManager(ctx).RunInTransactionWithOptions(ctx, postgres.SerializableTxOptions(), func(txCtx context.Context) error {
		q := postgres.MustGetTxManager(txCtx).GetQuerier(txCtx)
		if _, err := q.Exec(txCtx, `SELECT fn_crypto_aml_lock_fee_wallet_scopes($1,$2)`, req.PayerWalletID, req.NativeTokenID); err != nil {
			return err
		}
		policy, err := scanNetworkPolicy(q.QueryRow(txCtx, networkPolicyLockSQL, req.NetworkID))
		if err != nil {
			return fmt.Errorf("load network fee policy: %w", err)
		}
		if err := policy.CheckFeeCap(req.Strategy, req.MaxFee); err != nil {
			return err
		}

		existing, ok, err := findFeeReservation(txCtx, q, req)
		if err != nil {
			return err
		}
		if ok {
			if err := existing.CheckReusable(); err != nil {
				return err
			}
			reserved = existing
			return crypto_repo.NewAMLRepository().EnsureAMLResourceBudget(txCtx, reserved.ID)
		}

		budget := policy.FeeBudgetFor(req.Strategy)
		if budget.IsPositive() {
			var committed types.CryptoAmount
			if err := q.QueryRow(txCtx, `
				SELECT COALESCE(SUM(CASE WHEN strategy='native_funding' THEN reserved_principal_minor ELSE 0 END + CASE WHEN status='settled' THEN actual_fee_minor ELSE max_fee_minor END),0)
				FROM reg_crypto_fee_reservations
				WHERE network_id=$1 AND strategy=$2
				  AND reserved_at >= date_trunc('day', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
				  AND status IN ('reserved','broadcast','settled','held')
			`, req.NetworkID, req.Strategy).Scan(&committed); err != nil {
				return fmt.Errorf("load daily fee commitment: %w", err)
			}
			requested := req.MaxFee
			if req.Strategy == crypto.FeeStrategyNativeFunding {
				requested = requested.Add(req.ReservedPrincipal)
			}
			budgetRemaining, err = policy.CheckDailyFeeCommitment(req.Strategy, committed, requested)
			if err != nil {
				return err
			}
		}

		var walletNetwork id.ID
		var walletRole string
		if err := q.QueryRow(txCtx, `
			SELECT network_id, wallet_role
			FROM cat_wallets
			WHERE id=$1 AND is_active=TRUE AND deletion_mark=FALSE
			FOR UPDATE
		`, req.PayerWalletID).Scan(&walletNetwork, &walletRole); err != nil {
			return fmt.Errorf("lock fee payer wallet: %w", err)
		}
		if err := req.ValidatePayer(walletNetwork, walletRole); err != nil {
			return err
		}

		if req.Strategy != crypto.FeeStrategyResourceRental {
			var ledgerBalance types.CryptoAmount
			err = q.QueryRow(txCtx, `
				SELECT amount FROM reg_crypto_balance_balances
				WHERE wallet_id=$1 AND token_id=$2
				FOR UPDATE
			`, req.PayerWalletID, req.NativeTokenID).Scan(&ledgerBalance)
			if errors.Is(err, pgx.ErrNoRows) {
				ledgerBalance = types.ZeroCryptoAmount()
			} else if err != nil {
				return fmt.Errorf("lock fee payer native ledger balance: %w", err)
			}
			operating, amlEnabled, err := amlAvailableOperatingBalance(txCtx, req.PayerWalletID, req.NativeTokenID)
			if err != nil {
				return err
			}
			// AML operating funds can only reduce the balance available for fees.
			ledgerBalance = crypto.EffectiveFeeBalance(ledgerBalance, operating, amlEnabled)
			var activeReserved types.CryptoAmount
			if err := q.QueryRow(txCtx, `
				SELECT COALESCE(SUM(
					CASE
						WHEN status IN ('reserved','broadcast','held') AND strategy <> 'resource_rental' THEN reserved_principal_minor + max_fee_minor
						WHEN status IN ('reserved','broadcast','held') AND strategy = 'resource_rental' THEN reserved_principal_minor
						WHEN status='settled' AND reserved_principal_minor > 0 AND EXISTS (
							SELECT 1 FROM doc_crypto_native_fundings f
							WHERE f.fee_reservation_id=reg_crypto_fee_reservations.id
							  AND f.status NOT IN ('settled','failed')
						) THEN reserved_principal_minor
						ELSE 0
					END
				),0)
				FROM reg_crypto_fee_reservations
				WHERE payer_wallet_id=$1
			`, req.PayerWalletID).Scan(&activeReserved); err != nil {
				return fmt.Errorf("load active wallet fee reservations: %w", err)
			}
			var principalReserved types.CryptoAmount
			if err := q.QueryRow(txCtx, `SELECT COALESCE(sum(amount),0) FROM reg_crypto_balance_reservations WHERE wallet_id=$1 AND token_id=$2 AND status='active'`, req.PayerWalletID, req.NativeTokenID).Scan(&principalReserved); err != nil {
				return err
			}
			activeReserved = activeReserved.Add(principalReserved)
			if err := policy.CheckFeeWalletBalance(walletRole, ledgerBalance, activeReserved, req.ReservedPrincipal.Add(req.MaxFee)); err != nil {
				return err
			}
		}

		reserved = crypto.FeeReservation{
			ID: id.New(), NetworkID: req.NetworkID, PayerWalletID: req.PayerWalletID,
			NativeTokenID: req.NativeTokenID, OperationTokenID: req.OperationTokenID, BasisType: req.BasisType, BasisID: req.BasisID,
			ReservedPrincipal: req.ReservedPrincipal,
			Purpose:           req.Purpose, Attempt: req.Attempt, Strategy: req.Strategy,
			MaxFee: req.MaxFee, Status: crypto.FeeReservationReserved, ReservedAt: time.Now().UTC(),
		}
		if _, err := q.Exec(txCtx, `
			INSERT INTO reg_crypto_fee_reservations (
				id,network_id,payer_wallet_id,native_token_id,operation_token_id,reserved_principal_minor,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,status,reserved_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'reserved',$13)
		`, reserved.ID, reserved.NetworkID, reserved.PayerWalletID, reserved.NativeTokenID, reserved.OperationTokenID,
			reserved.ReservedPrincipal, reserved.BasisType, reserved.BasisID, reserved.Purpose, reserved.Attempt, reserved.Strategy, reserved.MaxFee, reserved.ReservedAt); err != nil {
			return fmt.Errorf("insert fee reservation: %w", err)
		}
		if err := crypto_repo.NewAMLRepository().EnsureAMLResourceBudget(txCtx, reserved.ID); err != nil {
			return err
		}
		return appendFeeReservationEvent(txCtx, q, reserved.ID, "", crypto.FeeReservationReserved, "fee cap reserved", reserved.MaxFee, nil)
	})
	return reserved, err
}

func (m *FeeBudgetManager) BindUnsignedFingerprint(ctx context.Context, reservationID id.ID, fingerprint string) error {
	if id.IsNil(reservationID) || strings.TrimSpace(fingerprint) == "" {
		return fmt.Errorf("reservation and unsigned fingerprint are required")
	}
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := crypto_repo.LockAMLResourceScopes(ctx, reservationID); err != nil {
			return err
		}
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		tag, err := q.Exec(ctx, `
		UPDATE reg_crypto_fee_reservations
		SET unsigned_fingerprint=$2
		WHERE id=$1 AND status='reserved' AND (unsigned_fingerprint='' OR unsigned_fingerprint=$2)
	`, reservationID, fingerprint)
		if err != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("bind fee reservation fingerprint: immutable reservation mismatch")
		}
		return nil
	})
}

func (m *FeeBudgetManager) MarkBroadcast(ctx context.Context, reservationID id.ID, fingerprint, txHash string) error {
	return m.transition(ctx, reservationID, []crypto.FeeReservationStatus{crypto.FeeReservationReserved, crypto.FeeReservationBroadcast, crypto.FeeReservationHeld}, crypto.FeeReservationBroadcast, "transaction broadcast", func(q pgx.Tx, txCtx context.Context) error {
		tag, err := q.Exec(txCtx, `
			UPDATE reg_crypto_fee_reservations
			SET status='broadcast',unsigned_fingerprint=$2,tx_hash=$3,broadcast_at=COALESCE(broadcast_at,NOW())
			WHERE id=$1 AND status IN ('reserved','broadcast','held')
			  AND (unsigned_fingerprint='' OR unsigned_fingerprint=$2)
			  AND (tx_hash='' OR tx_hash=$3)
		`, reservationID, fingerprint, txHash)
		if err != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("fee reservation broadcast identity mismatch")
		}
		return nil
	})
}

// IncreaseMaxFee grows the single worst-case reservation owned by a business
// attempt. A replacement is not a second spend: only the delta is charged to
// the daily and wallet commitments, while every change remains append-only in
// reg_crypto_fee_reservation_events.
func (m *FeeBudgetManager) IncreaseMaxFee(ctx context.Context, reservationID id.ID, newMax types.CryptoAmount, generation int) error {
	if id.IsNil(reservationID) || !newMax.IsPositive() || generation < 2 || generation > 3 {
		return fmt.Errorf("invalid replacement fee reservation increase")
	}
	return postgres.MustGetTxManager(ctx).RunInTransactionWithOptions(ctx, postgres.SerializableTxOptions(), func(txCtx context.Context) error {
		if err := crypto_repo.LockAMLResourceScopes(txCtx, reservationID); err != nil {
			return err
		}
		q := postgres.MustGetTxManager(txCtx).GetQuerier(txCtx)
		var network, payer id.ID
		if err := q.QueryRow(txCtx, `SELECT network_id,payer_wallet_id FROM reg_crypto_fee_reservations WHERE id=$1`, reservationID).Scan(&network, &payer); err != nil {
			return err
		}
		policy, err := scanNetworkPolicy(q.QueryRow(txCtx, networkPolicyLockSQL, network))
		if err != nil {
			return fmt.Errorf("load replacement fee policy: %w", err)
		}
		var walletRole string
		if err := q.QueryRow(txCtx, `SELECT wallet_role FROM cat_wallets WHERE id=$1 FOR UPDATE`, payer).Scan(&walletRole); err != nil {
			return err
		}
		var reservation crypto.FeeReservation
		var strategy, status string
		if err := q.QueryRow(txCtx, `
			SELECT network_id,payer_wallet_id,strategy,max_fee_minor,status
			FROM reg_crypto_fee_reservations WHERE id=$1 FOR UPDATE
		`, reservationID).Scan(&reservation.NetworkID, &reservation.PayerWalletID, &strategy, &reservation.MaxFee, &status); err != nil {
			return fmt.Errorf("lock replacement fee reservation: %w", err)
		}
		if reservation.NetworkID != network || reservation.PayerWalletID != payer {
			return fmt.Errorf("replacement fee payer changed while acquiring locks")
		}
		reservation.Strategy = crypto.FeeStrategy(strategy)
		reservation.Status = crypto.FeeReservationStatus(status)
		if reservation.Status != crypto.FeeReservationReserved && reservation.Status != crypto.FeeReservationBroadcast {
			return fmt.Errorf("fee reservation cannot be increased from %s", reservation.Status)
		}
		if newMax.Cmp(reservation.MaxFee) <= 0 {
			return nil
		}
		if err := policy.CheckFeeCap(reservation.Strategy, newMax); err != nil {
			return err
		}
		delta := newMax.Sub(reservation.MaxFee)
		budget := policy.FeeBudgetFor(reservation.Strategy)
		if budget.IsPositive() {
			var committed types.CryptoAmount
			if err := q.QueryRow(txCtx, `
				SELECT COALESCE(SUM(CASE WHEN strategy='native_funding' THEN reserved_principal_minor ELSE 0 END + CASE WHEN status='settled' THEN actual_fee_minor ELSE max_fee_minor END),0)
				FROM reg_crypto_fee_reservations
				WHERE network_id=$1 AND strategy=$2
				  AND reserved_at >= date_trunc('day', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
				  AND status IN ('reserved','broadcast','settled','held')
			`, reservation.NetworkID, reservation.Strategy).Scan(&committed); err != nil {
				return fmt.Errorf("load replacement daily commitment: %w", err)
			}
			if _, err := policy.CheckDailyFeeCommitment(reservation.Strategy, committed, delta); err != nil {
				return err
			}
		}
		if reservation.Strategy != crypto.FeeStrategyResourceRental {
			var ledgerBalance, activeReserved types.CryptoAmount
			if err := q.QueryRow(txCtx, `SELECT amount FROM reg_crypto_balance_balances
				WHERE wallet_id=$1 AND token_id=(SELECT native_token_id FROM reg_crypto_fee_reservations WHERE id=$2)
				FOR UPDATE`, reservation.PayerWalletID, reservationID).Scan(&ledgerBalance); errors.Is(err, pgx.ErrNoRows) {
				ledgerBalance = types.ZeroCryptoAmount()
			} else if err != nil {
				return fmt.Errorf("lock replacement fee wallet balance: %w", err)
			}
			if err := q.QueryRow(txCtx, `SELECT COALESCE(SUM(CASE WHEN strategy <> 'resource_rental' THEN reserved_principal_minor+max_fee_minor ELSE reserved_principal_minor END),0)
				FROM reg_crypto_fee_reservations WHERE payer_wallet_id=$1 AND status IN ('reserved','broadcast','held')`, reservation.PayerWalletID).Scan(&activeReserved); err != nil {
				return fmt.Errorf("load replacement wallet commitments: %w", err)
			}
			var principalReserved types.CryptoAmount
			if err := q.QueryRow(txCtx, `SELECT COALESCE(sum(amount),0) FROM reg_crypto_balance_reservations WHERE wallet_id=$1 AND status='active'
				AND token_id=(SELECT native_token_id FROM reg_crypto_fee_reservations WHERE id=$2)`, reservation.PayerWalletID, reservationID).Scan(&principalReserved); err != nil {
				return err
			}
			activeReserved = activeReserved.Add(principalReserved)
			if err := policy.CheckFeeWalletBalance(walletRole, ledgerBalance, activeReserved, delta); err != nil {
				return err
			}
		}
		tag, err := q.Exec(txCtx, `UPDATE reg_crypto_fee_reservations SET max_fee_minor=$2 WHERE id=$1 AND max_fee_minor=$3`, reservationID, newMax, reservation.MaxFee)
		if err != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("update replacement fee reservation: concurrent identity mismatch")
		}
		return appendFeeReservationEvent(txCtx, q, reservationID, reservation.Status, reservation.Status,
			fmt.Sprintf("replacement fee cap generation %d", generation), newMax,
			map[string]any{"generation": generation, "oldMaxFee": reservation.MaxFee.String(), "newMaxFee": newMax.String(), "delta": delta.String()})
	})
}

// MarkReplacementBroadcast advances the current transaction identity without
// creating another reservation. The previous identity is retained in the
// immutable event stream and signing-generation records.
func (m *FeeBudgetManager) MarkReplacementBroadcast(ctx context.Context, reservationID id.ID, oldFingerprint, oldTxHash, newFingerprint, newTxHash string, generation int) error {
	if id.IsNil(reservationID) || generation < 1 || generation > 3 || strings.TrimSpace(newFingerprint) == "" || strings.TrimSpace(newTxHash) == "" {
		return fmt.Errorf("invalid replacement broadcast identity")
	}
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := crypto_repo.LockAMLResourceScopes(txCtx, reservationID); err != nil {
			return err
		}
		q := postgres.MustGetTxManager(txCtx).GetQuerier(txCtx)
		var status crypto.FeeReservationStatus
		var maxFee types.CryptoAmount
		var currentFingerprint, currentTxHash string
		if err := q.QueryRow(txCtx, `SELECT status,max_fee_minor,unsigned_fingerprint,tx_hash
			FROM reg_crypto_fee_reservations WHERE id=$1 FOR UPDATE`, reservationID).
			Scan(&status, &maxFee, &currentFingerprint, &currentTxHash); err != nil {
			return err
		}
		if status != crypto.FeeReservationBroadcast && status != crypto.FeeReservationReserved {
			return fmt.Errorf("fee reservation cannot record replacement from %s", status)
		}
		if currentFingerprint == newFingerprint && currentTxHash == newTxHash {
			return nil
		}
		if currentFingerprint != oldFingerprint || currentTxHash != oldTxHash {
			return fmt.Errorf("fee reservation replacement identity mismatch")
		}
		tag, err := q.Exec(txCtx, `UPDATE reg_crypto_fee_reservations
			SET status='broadcast',unsigned_fingerprint=$2,tx_hash=$3,broadcast_at=COALESCE(broadcast_at,NOW()) WHERE id=$1`, reservationID, newFingerprint, newTxHash)
		if err != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("record replacement broadcast: %w", err)
		}
		return appendFeeReservationEvent(txCtx, q, reservationID, status, crypto.FeeReservationBroadcast,
			fmt.Sprintf("replacement generation %d broadcast", generation), maxFee,
			map[string]any{"generation": generation, "oldFingerprint": oldFingerprint, "oldTxHash": oldTxHash, "newFingerprint": newFingerprint, "newTxHash": newTxHash})
	})
}

func (m *FeeBudgetManager) Release(ctx context.Context, reservationID id.ID, reason string) error {
	return m.simpleStatusTransition(ctx, reservationID, []crypto.FeeReservationStatus{crypto.FeeReservationReserved}, crypto.FeeReservationReleased, reason)
}

// ReleaseUnbroadcast releases a reservation only when no transaction identity
// was ever persisted. It is the fail-closed recovery path for an expired
// staged signing authorization; held reservations with a tx hash remain
// unavailable until reconciliation proves the chain outcome.
func (m *FeeBudgetManager) ReleaseUnbroadcast(ctx context.Context, reservationID id.ID, reason string) error {
	return m.transition(ctx, reservationID,
		[]crypto.FeeReservationStatus{crypto.FeeReservationReserved, crypto.FeeReservationHeld},
		crypto.FeeReservationReleased, reason, func(q pgx.Tx, txCtx context.Context) error {
			tag, err := q.Exec(txCtx, `
				UPDATE reg_crypto_fee_reservations
				SET status='released',released_at=NOW(),hold_reason=''
				WHERE id=$1 AND tx_hash=''
			`, reservationID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return fmt.Errorf("fee reservation has a persisted broadcast identity")
			}
			return nil
		})
}

func (m *FeeBudgetManager) Hold(ctx context.Context, reservationID id.ID, reason string) error {
	return m.transition(ctx, reservationID, []crypto.FeeReservationStatus{crypto.FeeReservationReserved, crypto.FeeReservationBroadcast, crypto.FeeReservationHeld}, crypto.FeeReservationHeld, reason, func(q pgx.Tx, txCtx context.Context) error {
		_, err := q.Exec(txCtx, `UPDATE reg_crypto_fee_reservations SET status='held',hold_reason=$2 WHERE id=$1`, reservationID, statusFailedReason(errors.New(reason)))
		return err
	})
}

type FeeSettlementResult = outboundapp.FeeSettlementResult

func (m *FeeBudgetManager) Settle(ctx context.Context, reservationID id.ID, receipt crypto.ChainReceipt) error {
	_, err := m.settle(ctx, reservationID, receipt, true, "", id.Nil())
	return err
}

// SettleForDocument persists final chain evidence and completes the reservation
// without writing accounting movements. The caller posts those movements from
// the supplied document inside the same outer transaction.
func (m *FeeBudgetManager) SettleForDocument(
	ctx context.Context,
	reservationID id.ID,
	receipt crypto.ChainReceipt,
	basisType string,
	basisID id.ID,
) (FeeSettlementResult, error) {
	if strings.TrimSpace(basisType) == "" || id.IsNil(basisID) {
		return FeeSettlementResult{}, fmt.Errorf("document settlement basis is required")
	}
	return m.settle(ctx, reservationID, receipt, false, basisType, basisID)
}

func validateFeeSettlementOwner(purpose crypto.SigningPurpose, recordMovements bool) error {
	if purpose == crypto.SigningPurposeActivation && recordMovements {
		return fmt.Errorf("activation fee reservations must be settled through CryptoNetworkOperation")
	}
	return nil
}

func (m *FeeBudgetManager) settle(
	ctx context.Context,
	reservationID id.ID,
	receipt crypto.ChainReceipt,
	recordMovements bool,
	documentBasisType string,
	documentBasisID id.ID,
) (result FeeSettlementResult, err error) {
	var purpose crypto.SigningPurpose
	var reservedAt time.Time
	finish := func(crypto.SigningPurpose, time.Time, error) {}
	if m != nil && m.observer != nil {
		ctx, finish = m.observer.Settle(ctx, receipt)
	}
	defer func() { finish(purpose, reservedAt, err) }()
	if id.IsNil(reservationID) || strings.TrimSpace(receipt.TxHash) == "" || !receipt.FinanciallyFinal() {
		return result, fmt.Errorf("solidified or finalized receipt is required to settle a fee reservation")
	}
	if err := validateFeeComponents(receipt.Fee); err != nil {
		return result, err
	}
	var committedHoldErr error
	err = postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := crypto_repo.LockAMLResourceScopes(txCtx, reservationID); err != nil {
			return err
		}
		if err := crypto_repo.NewNetworkFeeRepo().LockReservation(txCtx, reservationID); err != nil {
			return err
		}
		tx := postgres.MustGetTxManager(txCtx).GetTx(txCtx)
		var reservation crypto.FeeReservation
		var rawPurpose, rawStrategy, rawStatus string
		if err := tx.QueryRow(txCtx, `
			SELECT id,network_id,payer_wallet_id,native_token_id,operation_token_id,reserved_principal_minor,basis_type,basis_id,purpose,attempt,strategy,
			       max_fee_minor,actual_fee_minor,unsigned_fingerprint,tx_hash,status,hold_reason,reserved_at
			FROM reg_crypto_fee_reservations WHERE id=$1 FOR UPDATE
		`, reservationID).Scan(
			&reservation.ID, &reservation.NetworkID, &reservation.PayerWalletID, &reservation.NativeTokenID,
			&reservation.OperationTokenID, &reservation.ReservedPrincipal, &reservation.BasisType, &reservation.BasisID, &rawPurpose,
			&reservation.Attempt, &rawStrategy, &reservation.MaxFee, &reservation.ActualFee,
			&reservation.UnsignedFingerprint, &reservation.TxHash, &rawStatus, &reservation.HoldReason,
			&reservation.ReservedAt); err != nil {
			return err
		}
		reservation.Purpose = crypto.SigningPurpose(rawPurpose)
		reservation.Strategy = crypto.FeeStrategy(rawStrategy)
		reservation.Status = crypto.FeeReservationStatus(rawStatus)
		if err := validateFeeSettlementOwner(reservation.Purpose, recordMovements); err != nil {
			return err
		}
		purpose = reservation.Purpose
		reservedAt = reservation.ReservedAt
		if reservation.NetworkID != receipt.NetworkID || reservation.TxHash != receipt.TxHash {
			return fmt.Errorf("fee settlement receipt identity mismatch")
		}
		if id.IsNil(receipt.Fee.NativeAsset.TokenID) || receipt.Fee.NativeAsset.TokenID != reservation.NativeTokenID {
			return fmt.Errorf("fee settlement native token mismatch")
		}
		result.ReservationID = reservation.ID
		if err := crypto_repo.NewNetworkFeeRepo().ValidatePayer(txCtx, reservation, receipt); err != nil {
			return err
		}
		if reservation.Status == crypto.FeeReservationSettled {
			if err := crypto_repo.NewNetworkFeeRepo().ValidateReplay(txCtx, reservation, receipt, documentBasisType, documentBasisID); err != nil {
				return err
			}
			var replayErr error
			result, replayErr = loadFeeSettlementReplay(txCtx, tx, reservation, receipt, documentBasisType, documentBasisID, recordMovements)
			return replayErr
		}
		actual := receipt.Fee.TotalFee
		if actual.Cmp(reservation.MaxFee) > 0 {
			reason := "actual fee exceeds reserved cap"
			tag, updateErr := tx.Exec(txCtx, `UPDATE reg_crypto_fee_reservations SET status='held',hold_reason=$2 WHERE id=$1`, reservationID, reason)
			if updateErr != nil || tag.RowsAffected() != 1 {
				return fmt.Errorf("hold fee reservation after cap violation: %w", updateErr)
			}
			if eventErr := appendFeeReservationEvent(txCtx, tx, reservationID, reservation.Status, crypto.FeeReservationHeld, reason, actual, nil); eventErr != nil {
				return eventErr
			}
			committedHoldErr = fmt.Errorf("%w: actual network fee exceeds reserved cap", crypto.ErrFeeBudgetExceeded)
			return nil
		}
		if err := crypto_repo.NewNetworkFeeRepo().LockEffect(txCtx, reservation, receipt); err != nil {
			return err
		}
		rawReceipt := receipt.RawReceipt
		if len(rawReceipt) == 0 {
			rawReceipt = json.RawMessage(`{}`)
		}
		settlementID := id.New()
		chainStatus := "succeeded"
		if receipt.Status == crypto.ReceiptReverted {
			chainStatus = "reverted"
		}
		settlementBasisType, settlementBasisID := reservation.BasisType, reservation.BasisID
		if documentBasisType != "" {
			settlementBasisType, settlementBasisID = documentBasisType, documentBasisID
		}
		if err := tx.QueryRow(txCtx, `
			INSERT INTO reg_crypto_chain_settlements (
				id,network_id,basis_type,basis_id,tx_hash,status,block_hash,block_height,finality_status,
				native_token_id,total_fee_minor,raw_receipt,observed_at,network_fee_reservation_id
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13,$14)
			ON CONFLICT DO NOTHING
			RETURNING id
		`, settlementID, receipt.NetworkID, settlementBasisType, settlementBasisID, receipt.TxHash,
			chainStatus, receipt.BlockHash, receipt.BlockHeight, receipt.Finality, reservation.NativeTokenID,
			actual, string(rawReceipt), receipt.ObservedAt, reservation.ID).Scan(&settlementID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperror.NewConflict("normalized chain settlement already exists; reconciliation is required before recognition")
			}
			return fmt.Errorf("persist normalized chain settlement: %w", err)
		}
		result = FeeSettlementResult{SettlementID: settlementID, ActualFee: actual, ReservationID: reservation.ID}
		if err := crypto_repo.NewNetworkFeeRepo().Prepare(txCtx, reservation, receipt, settlementID, documentBasisType, documentBasisID); err != nil {
			return err
		}
		for _, component := range receipt.Fee.Components {
			metadata := component.RawEvidence
			if len(metadata) == 0 {
				metadata = json.RawMessage(`{}`)
			}
			if _, err := tx.Exec(txCtx, `
				INSERT INTO reg_crypto_chain_settlement_components (settlement_id,component,amount_minor,quantity,metadata)
				VALUES ($1,$2,$3,$4,$5::jsonb) ON CONFLICT DO NOTHING
			`, settlementID, component.Name, component.Amount, component.Quantity, string(metadata)); err != nil {
				return err
			}
		}
		purpose := string(reservation.Purpose)
		if reservation.Purpose == crypto.SigningPurposeTreasury {
			purpose = "treasury"
		}
		if recordMovements && actual.IsPositive() {
			if _, err := tx.Exec(txCtx, `
				INSERT INTO reg_crypto_balance_movements (
					line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount
				) VALUES ($1,$2,'ChainSettlement',1,$3,'expense',$4,$5,$6)
				ON CONFLICT (line_id) DO NOTHING
			`, reservation.ID, reservation.BasisID, receipt.ObservedAt, reservation.PayerWalletID, reservation.NativeTokenID, actual); err != nil {
				return fmt.Errorf("record chain wallet fee expense once: %w", err)
			}
		}
		if recordMovements && actual.IsPositive() {
			if _, err := tx.Exec(txCtx, `
			INSERT INTO reg_crypto_platform_cost_movements (
				line_id,recorder_id,recorder_type,recorder_version,period,record_type,
				network_id,operation_token_id,cost_token_id,cost_type,purpose,provider,external_order_id,amount
			) VALUES ($1,$2,'ChainSettlement',1,$3,'expense',$4,$5,$6,'chain_network_fee',$7,'chain',$8,$9)
			ON CONFLICT (line_id) DO NOTHING
		`, reservation.ID, reservation.BasisID, receipt.ObservedAt, reservation.NetworkID,
				reservation.OperationTokenID, reservation.NativeTokenID, purpose, receipt.TxHash, actual); err != nil {
				return fmt.Errorf("record chain network fee once: %w", err)
			}
		}
		if recordMovements {
			if err := validateFeeSettlementMovements(txCtx, tx, reservation, actual, receipt.ObservedAt); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(txCtx, `
			UPDATE reg_crypto_fee_reservations
			SET status='settled',actual_fee_minor=$2,settled_at=NOW(),hold_reason=''
			WHERE id=$1 AND status IN ('broadcast','held') AND tx_hash=$3
		`, reservation.ID, actual, receipt.TxHash)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("fee reservation settlement state mismatch")
		}
		return appendFeeReservationEvent(txCtx, tx, reservation.ID, reservation.Status, crypto.FeeReservationSettled, "solidified receipt settled", actual, nil)
	})
	if err != nil {
		return result, err
	}
	return result, committedHoldErr
}

func (m *FeeBudgetManager) simpleStatusTransition(ctx context.Context, reservationID id.ID, from []crypto.FeeReservationStatus, to crypto.FeeReservationStatus, reason string) error {
	return m.transition(ctx, reservationID, from, to, reason, func(q pgx.Tx, txCtx context.Context) error {
		tag, err := q.Exec(txCtx, `UPDATE reg_crypto_fee_reservations SET status=$2,released_at=CASE WHEN $2='released' THEN NOW() ELSE released_at END WHERE id=$1`, reservationID, to)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("fee reservation transition target is missing")
		}
		return nil
	})
}

func (m *FeeBudgetManager) transition(ctx context.Context, reservationID id.ID, allowed []crypto.FeeReservationStatus, to crypto.FeeReservationStatus, reason string, mutate func(pgx.Tx, context.Context) error) error {
	if id.IsNil(reservationID) {
		return nil
	}
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := crypto_repo.LockAMLResourceScopes(txCtx, reservationID); err != nil {
			return err
		}
		tx := postgres.MustGetTxManager(txCtx).GetTx(txCtx)
		var from crypto.FeeReservationStatus
		var amount types.CryptoAmount
		if err := tx.QueryRow(txCtx, `SELECT status,max_fee_minor FROM reg_crypto_fee_reservations WHERE id=$1 FOR UPDATE`, reservationID).Scan(&from, &amount); err != nil {
			return err
		}
		allowedFrom := false
		for _, candidate := range allowed {
			allowedFrom = allowedFrom || from == candidate
		}
		if !allowedFrom {
			return fmt.Errorf("fee reservation cannot transition from %s to %s", from, to)
		}
		if to == crypto.FeeReservationReleased {
			var activationExposure bool
			if err := tx.QueryRow(txCtx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_fee_reservations f WHERE f.id=$1 AND f.purpose='activation' AND (
 f.tx_hash<>'' OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations s WHERE s.basis_type=f.basis_type AND s.basis_id=f.basis_id AND s.purpose=f.purpose AND s.attempt=f.attempt AND (
 s.signed_envelope IS NOT NULL OR EXISTS(SELECT 1 FROM reg_crypto_activation_dispatches d WHERE d.authorization_id=s.authorization_id AND d.phase='sign')))))`, reservationID).Scan(&activationExposure); err != nil {
				return err
			}
			if activationExposure {
				return apperror.NewConflict("activation fee has signing exposure; reconciliation is required")
			}
			cancellationReason := reason
			if len(strings.TrimSpace(cancellationReason)) < 3 {
				cancellationReason = "fee reservation released"
			} else if len(cancellationReason) > 500 {
				cancellationReason = cancellationReason[:500]
			}
			if _, err := tx.Exec(txCtx, `
				INSERT INTO reg_crypto_aml_resource_cancellations(budget_id, reason, cancelled_by)
				SELECT b.id, $2, 'fee_budget_manager'
				FROM reg_crypto_aml_resource_budgets b
				WHERE b.fee_reservation_id = $1 AND b.status = 'reserved'
				ON CONFLICT (budget_id) DO NOTHING
			`, reservationID, cancellationReason); err != nil {
				return fmt.Errorf("record resource cancellation for fee release: %w", err)
			}
			if _, err := tx.Exec(txCtx, `
				UPDATE reg_crypto_aml_resource_budgets
				SET status = 'cancelled', updated_at = NOW()
				WHERE fee_reservation_id = $1 AND status = 'reserved'
			`, reservationID); err != nil {
				return fmt.Errorf("cancel resource budget for fee release: %w", err)
			}
		}
		if err := mutate(tx, txCtx); err != nil {
			return err
		}
		if from == to {
			return nil
		}
		return appendFeeReservationEvent(txCtx, tx, reservationID, from, to, reason, amount, nil)
	})
}

const networkPolicySelect = `
	SELECT p.network_id,p.enabled,p.per_transaction_max_fee_minor,p.daily_network_fee_budget_minor,p.fee_funding_mode,
	       p.daily_jit_funding_budget_minor,p.min_fee_wallet_balance_minor,p.min_source_wallet_residual_minor,p.jit_funding_max_amount_minor,
	       p.retry_delay_seconds,p.rpc_failure_threshold,p.rpc_open_seconds,
	       p.version,p.updated_by,p.created_at,p.updated_at,
	       tr.resource_strategy,COALESCE(tr.provider_account_ids,ARRAY[]::uuid[]),tr.provider_failure_threshold,tr.provider_open_seconds,
	       EXISTS (SELECT 1 FROM reg_crypto_network_holds h WHERE h.network_id=p.network_id AND h.status='open'),
	       COALESCE((SELECT h.reason FROM reg_crypto_network_holds h WHERE h.network_id=p.network_id AND h.status='open' ORDER BY h.held_at DESC LIMIT 1),'')
	FROM sys_crypto_network_policies p
	LEFT JOIN sys_crypto_external_resource_policies tr ON tr.network_id=p.network_id`

// Lock only the policy row. A bare FOR UPDATE is invalid when the LEFT JOIN
// resource-policy side is NULL, which is the EVM/Sepolia default.
const networkPolicyLockSQL = networkPolicySelect + ` WHERE p.network_id=$1 AND p._deleted_at IS NULL FOR UPDATE OF p`

func scanNetworkPolicy(row pgx.Row) (crypto.NetworkPolicy, error) {
	var p crypto.NetworkPolicy
	var fundingMode string
	var strategy *string
	var providerAccountIDs []id.ID
	var providerThreshold, resourceOpenSeconds *int
	var retrySeconds, rpcOpenSeconds int
	err := row.Scan(&p.NetworkID, &p.Enabled, &p.PerTransactionMaxFee, &p.DailyNetworkFeeBudget, &fundingMode,
		&p.FeeFunding.DailyBudget, &p.FeeFunding.MinFeeWalletBalance,
		&p.FeeFunding.MinSourceWalletResidual, &p.FeeFunding.MaxFundingAmount, &retrySeconds,
		&p.RPCFailureThreshold, &rpcOpenSeconds, &p.Version, &p.UpdatedBy, &p.CreatedAt, &p.UpdatedAt,
		&strategy, &providerAccountIDs, &providerThreshold, &resourceOpenSeconds, &p.OperationalHold, &p.OperationalHoldReason)
	if err != nil {
		return p, err
	}
	p.FeeFunding.Mode = crypto.FeeFundingMode(fundingMode)
	p.RetryDelay = time.Duration(retrySeconds) * time.Second
	p.RPCOpenDuration = time.Duration(rpcOpenSeconds) * time.Second
	if strategy != nil && providerThreshold != nil && resourceOpenSeconds != nil {
		p.ExternalResources = &crypto.ExternalResourcePolicy{
			Strategy:                 crypto.ResourceStrategyPolicy(*strategy),
			ProviderAccountIDs:       providerAccountIDs,
			ProviderFailureThreshold: *providerThreshold,
			ProviderOpenDuration:     time.Duration(*resourceOpenSeconds) * time.Second,
		}
	}
	return p, p.Validate()
}

func validateFeeReservationRequest(req FeeReservationRequest) error { return req.Validate() }

func findFeeReservation(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, req FeeReservationRequest) (crypto.FeeReservation, bool, error) {
	var r crypto.FeeReservation
	var purpose, strategy, status string
	err := q.QueryRow(ctx, `
		SELECT id,network_id,payer_wallet_id,native_token_id,operation_token_id,reserved_principal_minor,basis_type,basis_id,purpose,attempt,strategy,
		       max_fee_minor,actual_fee_minor,unsigned_fingerprint,tx_hash,status,hold_reason,reserved_at
		FROM reg_crypto_fee_reservations
		WHERE basis_type=$1 AND basis_id=$2 AND purpose=$3 AND attempt=$4
		FOR UPDATE
	`, req.BasisType, req.BasisID, req.Purpose, req.Attempt).Scan(
		&r.ID, &r.NetworkID, &r.PayerWalletID, &r.NativeTokenID, &r.OperationTokenID, &r.ReservedPrincipal, &r.BasisType, &r.BasisID,
		&purpose, &r.Attempt, &strategy, &r.MaxFee, &r.ActualFee, &r.UnsignedFingerprint,
		&r.TxHash, &status, &r.HoldReason, &r.ReservedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	r.Purpose, r.Strategy, r.Status = crypto.SigningPurpose(purpose), crypto.FeeStrategy(strategy), crypto.FeeReservationStatus(status)
	if r.NetworkID != req.NetworkID || r.PayerWalletID != req.PayerWalletID || r.NativeTokenID != req.NativeTokenID || r.OperationTokenID != req.OperationTokenID || !r.ReservedPrincipal.Equal(req.ReservedPrincipal) || r.Strategy != req.Strategy || !r.MaxFee.Equal(req.MaxFee) {
		return r, false, fmt.Errorf("existing fee reservation conflicts with immutable attempt")
	}
	return r, true, nil
}

func appendFeeReservationEvent(ctx context.Context, q interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, reservationID id.ID, from crypto.FeeReservationStatus, to crypto.FeeReservationStatus, reason string, amount types.CryptoAmount, metadata any) error {
	raw := []byte("{}")
	if metadata != nil {
		raw, _ = json.Marshal(metadata)
	}
	_, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_fee_reservation_events (reservation_id,from_status,to_status,reason,amount_minor,metadata)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb)
	`, reservationID, from, to, statusFailedReason(errors.New(reason)), amount, string(raw))
	return err
}
