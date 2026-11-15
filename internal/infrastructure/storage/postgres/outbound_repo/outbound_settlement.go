package outbound_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/application/outboundapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_sweep"
	"metapus/internal/domain/documents/crypto_withdrawal"
	"metapus/internal/domain/documents/outbound_settlement"
	"metapus/internal/domain/documents/treasury_withdrawal"
	"metapus/internal/domain/documents/withdrawal_request"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

const (
	outboundSettlementLease = 2 * time.Minute
	settlementWithdrawal    = outboundapp.SettlementWithdrawal
	settlementSweep         = outboundapp.SettlementSweep
	settlementTreasury      = outboundapp.SettlementTreasury
)

type outboundSettlementTask = outboundapp.SettlementTask

func (r *OutboundRepo) ClaimOutboundSettlements(ctx context.Context, workerID string, limit int) ([]outboundSettlementTask, error) {
	if limit <= 0 {
		limit = _outboundBatchLimit
	}
	tasks := make([]outboundSettlementTask, 0, limit)
	loaders := []func(context.Context, string, int) ([]outboundSettlementTask, error){
		r.claimWithdrawalSettlements,
		r.claimSweepSettlements,
		r.claimTreasurySettlements,
	}
	for _, load := range loaders {
		if len(tasks) >= limit {
			break
		}
		batch, err := load(ctx, workerID, limit-len(tasks))
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, batch...)
	}
	return tasks, nil
}

func (r *OutboundRepo) claimWithdrawalSettlements(ctx context.Context, workerID string, limit int) ([]outboundSettlementTask, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `
		WITH picked AS (
			SELECT cw.id
			FROM doc_crypto_withdrawals cw
			WHERE cw.tx_hash <> '' AND cw.status IN ($4,$5,$6)
			  AND cw.settlement_status IN ($7,$8,$9)
			  AND (cw.settlement_lease_expires_at IS NULL OR cw.settlement_lease_expires_at < NOW())
			  AND cw.deletion_mark = FALSE
			ORDER BY COALESCE(cw.settlement_checked_at,cw.updated_at),cw.id
			FOR UPDATE SKIP LOCKED LIMIT $1
		), claimed AS (
			UPDATE doc_crypto_withdrawals cw
			SET settlement_status=$10,settlement_claimed_by=$2,
			    settlement_claimed_at=NOW(),settlement_lease_expires_at=NOW()+($3*interval '1 second'),
			    settlement_error='',updated_at=NOW()
			FROM picked p WHERE cw.id=p.id
			RETURNING cw.id,cw.token_id,cw.source_wallet_id,cw.tx_hash,cw.amount
		)
		SELECT c.id,wr.id,wr.merchant_id,t.network_id,c.token_id,c.source_wallet_id,c.tx_hash,c.amount,
		       COALESCE(n.confirmations_needed,1),wr.posted_version,COALESCE(aml.id,'00000000-0000-0000-0000-000000000000'::uuid)
		FROM claimed c
		JOIN cat_tokens t ON t.id=c.token_id
		JOIN cat_blockchain_networks n ON n.id=t.network_id
		LEFT JOIN reg_crypto_aml_withdrawal_lines aml ON aml.withdrawal_id=c.id
		JOIN doc_withdrawal_requests wr ON wr.id=COALESCE(aml.request_id,(SELECT direct.id FROM doc_withdrawal_requests direct WHERE direct.withdrawal_id=c.id))
	`,
		limit, workerID, int64(outboundSettlementLease.Seconds()),
		crypto_withdrawal.WithdrawalStatusBroadcast, crypto_withdrawal.WithdrawalStatusConfirmed, crypto_withdrawal.WithdrawalStatusFailed,
		outbound_settlement.StatusUnknown, outbound_settlement.StatusRepairing, outbound_settlement.StatusHeld,
		outbound_settlement.StatusRepairing,
	)
	if err != nil {
		return nil, fmt.Errorf("claim withdrawal settlements: %w", err)
	}
	defer rows.Close()
	result := make([]outboundSettlementTask, 0, limit)
	for rows.Next() {
		var task outboundSettlementTask
		task.Kind = settlementWithdrawal
		if err := rows.Scan(&task.DocumentID, &task.RequestID, &task.MerchantID, &task.NetworkID, &task.OperationTokenID,
			&task.SourceWalletID, &task.TxHash, &task.Amount, &task.RequiredConfs, &task.PostedVersion, &task.LineID); err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, rows.Err()
}

func (r *OutboundRepo) claimSweepSettlements(ctx context.Context, workerID string, limit int) ([]outboundSettlementTask, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `
		WITH picked AS (
			SELECT l.line_id
			FROM doc_crypto_sweeps__lines l
			JOIN doc_crypto_sweeps s ON s.id=l.document_id
			WHERE l.tx_hash <> '' AND l.settlement_status IN ($4,$5,$6)
			  AND (l.settlement_lease_expires_at IS NULL OR l.settlement_lease_expires_at < NOW())
			  AND s.status IN ($8,$9,$10) AND s.deletion_mark=FALSE
			ORDER BY COALESCE(l.settlement_checked_at,s.updated_at),l.line_id
			FOR UPDATE OF l SKIP LOCKED LIMIT $1
		), claimed AS (
			UPDATE doc_crypto_sweeps__lines l
			SET settlement_status=$7,settlement_claimed_by=$2,
			    settlement_claimed_at=NOW(),settlement_lease_expires_at=NOW()+($3*interval '1 second'),
			    settlement_error=''
			FROM picked p WHERE l.line_id=p.line_id
			RETURNING l.line_id,l.document_id,l.wallet_id,l.tx_hash,l.amount
		)
		SELECT c.line_id,c.document_id,t.network_id,s.token_id,c.wallet_id,s.hot_wallet_id,
		       c.tx_hash,c.amount,COALESCE(n.confirmations_needed,1),s.posted_version
		FROM claimed c
		JOIN doc_crypto_sweeps s ON s.id=c.document_id
		JOIN cat_tokens t ON t.id=s.token_id
		JOIN cat_blockchain_networks n ON n.id=t.network_id
	`,
		limit, workerID, int64(outboundSettlementLease.Seconds()),
		outbound_settlement.StatusUnknown, outbound_settlement.StatusRepairing, outbound_settlement.StatusHeld,
		outbound_settlement.StatusRepairing,
		crypto_sweep.SweepStatusBroadcast, crypto_sweep.SweepStatusConfirmed, crypto_sweep.SweepStatusPartialFailed,
	)
	if err != nil {
		return nil, fmt.Errorf("claim sweep settlements: %w", err)
	}
	defer rows.Close()
	result := make([]outboundSettlementTask, 0, limit)
	for rows.Next() {
		var task outboundSettlementTask
		task.Kind = settlementSweep
		if err := rows.Scan(&task.LineID, &task.DocumentID, &task.NetworkID, &task.OperationTokenID,
			&task.SourceWalletID, &task.HotWalletID, &task.TxHash, &task.Amount,
			&task.RequiredConfs, &task.PostedVersion); err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, rows.Err()
}

func (r *OutboundRepo) claimTreasurySettlements(ctx context.Context, workerID string, limit int) ([]outboundSettlementTask, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `
		WITH picked AS (
			SELECT tw.id
			FROM doc_treasury_withdrawals tw
			WHERE tw.tx_hash <> '' AND tw.status IN ($4,$5,$6)
			  AND tw.settlement_status IN ($7,$8,$9)
			  AND (tw.settlement_lease_expires_at IS NULL OR tw.settlement_lease_expires_at < NOW())
			  AND tw._deleted_at IS NULL
			ORDER BY COALESCE(tw.settlement_checked_at,tw.updated_at),tw.id
			FOR UPDATE SKIP LOCKED LIMIT $1
		), claimed AS (
			UPDATE doc_treasury_withdrawals tw
			SET settlement_status=$10,settlement_claimed_by=$2,
			    settlement_claimed_at=NOW(),settlement_lease_expires_at=NOW()+($3*interval '1 second'),
			    settlement_error='',updated_at=NOW()
			FROM picked p WHERE tw.id=p.id
			RETURNING tw.id,tw.token_id,tw.source_wallet_id,tw.tx_hash,tw.amount,tw.posted_version
		)
		SELECT c.id,t.network_id,c.token_id,c.source_wallet_id,c.tx_hash,c.amount,
		       COALESCE(n.confirmations_needed,1),c.posted_version
		FROM claimed c
		JOIN cat_tokens t ON t.id=c.token_id
		JOIN cat_blockchain_networks n ON n.id=t.network_id
	`,
		limit, workerID, int64(outboundSettlementLease.Seconds()),
		treasury_withdrawal.StatusBroadcast, treasury_withdrawal.StatusConfirmed, treasury_withdrawal.StatusFailed,
		outbound_settlement.StatusUnknown, outbound_settlement.StatusRepairing, outbound_settlement.StatusHeld,
		outbound_settlement.StatusRepairing,
	)
	if err != nil {
		return nil, fmt.Errorf("claim treasury settlements: %w", err)
	}
	defer rows.Close()
	result := make([]outboundSettlementTask, 0, limit)
	for rows.Next() {
		var task outboundSettlementTask
		task.Kind = settlementTreasury
		if err := rows.Scan(&task.DocumentID, &task.NetworkID, &task.OperationTokenID,
			&task.SourceWalletID, &task.TxHash, &task.Amount, &task.RequiredConfs, &task.PostedVersion); err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, rows.Err()
}

func (r *OutboundRepo) ReleaseOutboundSettlementClaim(ctx context.Context, task outboundSettlementTask) error {
	return r.updateOutboundSettlementClaim(ctx, task, outbound_settlement.StatusUnknown, "")
}

func (r *OutboundRepo) HoldOutboundSettlementClaim(ctx context.Context, task outboundSettlementTask, cause error) error {
	return r.updateOutboundSettlementClaim(ctx, task, outbound_settlement.StatusHeld, statusFailedReason(cause))
}

func (r *OutboundRepo) updateOutboundSettlementClaim(ctx context.Context, task outboundSettlementTask, status outbound_settlement.Status, message string) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var err error
	switch task.Kind {
	case settlementWithdrawal:
		_, err = q.Exec(ctx, `UPDATE doc_crypto_withdrawals SET settlement_status=$2,settlement_error=$3,
			settlement_checked_at=NOW(),settlement_claimed_by='',settlement_claimed_at=NULL,
			settlement_lease_expires_at=NULL,updated_at=NOW() WHERE id=$1 AND settlement_status=$4`,
			task.DocumentID, status, message, outbound_settlement.StatusRepairing)
	case settlementSweep:
		_, err = q.Exec(ctx, `UPDATE doc_crypto_sweeps__lines SET settlement_status=$2,settlement_error=$3,
			settlement_checked_at=NOW(),settlement_claimed_by='',settlement_claimed_at=NULL,
			settlement_lease_expires_at=NULL WHERE line_id=$1 AND settlement_status=$4`,
			task.LineID, status, message, outbound_settlement.StatusRepairing)
	case settlementTreasury:
		_, err = q.Exec(ctx, `UPDATE doc_treasury_withdrawals SET settlement_status=$2,settlement_error=$3,
			settlement_checked_at=NOW(),settlement_claimed_by='',settlement_claimed_at=NULL,
			settlement_lease_expires_at=NULL,updated_at=NOW() WHERE id=$1 AND settlement_status=$4`,
			task.DocumentID, status, message, outbound_settlement.StatusRepairing)
	default:
		return fmt.Errorf("unknown outbound settlement kind %q", task.Kind)
	}
	return err
}

func (r *OutboundRepo) RecordOutboundSettlement(ctx context.Context, task outboundSettlementTask, receipt crypto.ChainReceipt) error {
	if receipt.TxHash != task.TxHash || receipt.NetworkID != task.NetworkID || !receipt.FinanciallyFinal() {
		return fmt.Errorf("final chain receipt identity mismatch")
	}
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("outbound settlement requires a transaction")
	}
	q := tm.GetQuerier(ctx)
	if task.Kind == settlementWithdrawal && !id.IsNil(task.LineID) {
		if _, err := crypto_repo.NewAMLRepository().LockAMLWithdrawalScope(ctx, task.MerchantID, task.NetworkID, task.OperationTokenID); err != nil {
			return err
		}
	}
	locked, err := lockOutboundSettlement(ctx, q, task)
	if err != nil || !locked {
		return err
	}
	nativeTokenID := receipt.Fee.NativeAsset.TokenID
	result := outbound_settlement.ChainResultSuccess
	if receipt.Status == crypto.ReceiptReverted {
		result = outbound_settlement.ChainResult("REVERTED")
	}
	failed := result != outbound_settlement.ChainResultSuccess
	if task.Kind == settlementSweep && !failed {
		if err := crypto_repo.NewSweepCustodyRepo().RecordPrincipal(ctx, sweepAccountingBasis(task), receipt); err != nil {
			return err
		}
	}
	if task.Kind == settlementTreasury && !failed {
		if err := crypto_repo.NewTreasuryCustodyRepo().RecordPrincipalAndRevenue(ctx, treasuryAccountingBasis(task), receipt); err != nil {
			return err
		}
	}
	if task.Kind == settlementWithdrawal {
		if err := crypto_repo.NewWithdrawalCustodyRepo().RecordOutcome(ctx, withdrawalAccountingBasis(task), receipt); err != nil {
			return err
		}
	}
	if err := updateSettledOutbound(ctx, q, task, nativeTokenID, receipt, result, failed); err != nil {
		return err
	}
	if task.Kind == settlementWithdrawal {
		basis, basisID := withdrawalAccountingBasis(task).SigningBasis()
		status := "consumed"
		if failed {
			status = "released"
		}
		tag, err := q.Exec(ctx, `UPDATE reg_crypto_balance_reservations SET status=$3,updated_at=now() WHERE document_type=$1 AND document_id=$2 AND status='active'`, basis, basisID, status)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("withdrawal requires its active principal reservation")
		}
		if failed {
			if _, err := q.Exec(ctx, `UPDATE reg_crypto_signing_authorizations SET status='failed',updated_at=now() WHERE basis_type=$1 AND basis_id=$2 AND status IN ('created','signed','broadcast','held','superseded')`, basis, basisID); err != nil {
				return err
			}
		}
	}
	if task.Kind == settlementTreasury {
		if failed {
			if _, err := q.Exec(ctx, `UPDATE reg_crypto_signing_authorizations SET status='failed',updated_at=now() WHERE basis_type='TreasuryWithdrawal' AND basis_id=$1 AND status IN ('created','signed','broadcast','held','superseded')`, task.DocumentID); err != nil {
				return err
			}
			return r.ReleaseTreasuryReservations(ctx, task.DocumentID)
		}
		return r.ConsumeTreasuryReservations(ctx, task.DocumentID)
	}
	if task.Kind == settlementSweep {
		if failed {
			if _, err := q.Exec(ctx, `UPDATE reg_crypto_signing_authorizations SET status='failed',updated_at=now()
				WHERE basis_type='CryptoSweepAttempt' AND basis_id=$1 AND status IN ('created','signed','broadcast','held','superseded')`, task.LineID); err != nil {
				return err
			}
			if err := r.CancelSweepAttemptAllocation(ctx, task.LineID, "final sweep transaction reverted", "chain-settlement"); err != nil {
				return err
			}
			if err := r.MarkSweepAttemptFailed(ctx, task.LineID, "chain transaction reverted"); err != nil {
				return err
			}
			return r.ReleaseSweepClaims(ctx, []id.ID{task.SourceWalletID})
		}
		return cryptoapp.NewSweepLifecycleService(postgres.NewContextTxManager(), r).Complete(ctx, []id.ID{task.SourceWalletID}, []id.ID{task.LineID})
	}
	if task.Kind == settlementWithdrawal && !id.IsNil(task.LineID) {
		return recordAMLWithdrawalFact(ctx, task, !failed)
	}
	return nil
}

func sweepAccountingBasis(t outboundSettlementTask) crypto.SweepAccountingBasis {
	return crypto.SweepAccountingBasis{DocumentID: t.DocumentID, LineID: t.LineID, NetworkID: t.NetworkID, TokenID: t.OperationTokenID,
		SourceWalletID: t.SourceWalletID, DestinationWalletID: t.HotWalletID, TxHash: t.TxHash, Amount: t.Amount, VerificationID: t.VerificationID}
}

func (*OutboundRepo) PrepareSweepSettlement(ctx context.Context, task outboundSettlementTask, receipt crypto.ChainReceipt) (bool, error) {
	return crypto_repo.NewSweepCustodyRepo().Prepare(ctx, sweepAccountingBasis(task), receipt)
}

func treasuryAccountingBasis(t outboundSettlementTask) crypto.TreasuryAccountingBasis {
	return crypto.TreasuryAccountingBasis{DocumentID: t.DocumentID, NetworkID: t.NetworkID, TokenID: t.OperationTokenID, SourceWalletID: t.SourceWalletID, TxHash: t.TxHash, Amount: t.Amount, VerificationID: t.VerificationID}
}

func (*OutboundRepo) PrepareTreasurySettlement(ctx context.Context, task outboundSettlementTask, receipt crypto.ChainReceipt) (bool, error) {
	return crypto_repo.NewTreasuryCustodyRepo().Prepare(ctx, treasuryAccountingBasis(task), receipt)
}

func withdrawalAccountingBasis(t outboundSettlementTask) crypto.WithdrawalAccountingBasis {
	return crypto.WithdrawalAccountingBasis{DocumentID: t.DocumentID, RequestID: t.RequestID, LineID: t.LineID, MerchantID: t.MerchantID, NetworkID: t.NetworkID, TokenID: t.OperationTokenID, SourceWalletID: t.SourceWalletID, TxHash: t.TxHash, Amount: t.Amount, VerificationID: t.VerificationID}
}
func (*OutboundRepo) PrepareWithdrawalSettlement(ctx context.Context, t outboundSettlementTask, r crypto.ChainReceipt) (bool, error) {
	return crypto_repo.NewWithdrawalCustodyRepo().Prepare(ctx, withdrawalAccountingBasis(t), r)
}

func lockOutboundSettlement(ctx context.Context, q providerBalanceQuerier, task outboundSettlementTask) (bool, error) {
	var status outbound_settlement.Status
	var err error
	switch task.Kind {
	case settlementWithdrawal:
		err = q.QueryRow(ctx, `SELECT settlement_status FROM doc_crypto_withdrawals WHERE id=$1 FOR UPDATE`, task.DocumentID).Scan(&status)
	case settlementSweep:
		err = q.QueryRow(ctx, `SELECT settlement_status FROM doc_crypto_sweeps__lines WHERE line_id=$1 FOR UPDATE`, task.LineID).Scan(&status)
	case settlementTreasury:
		err = q.QueryRow(ctx, `SELECT settlement_status FROM doc_treasury_withdrawals WHERE id=$1 FOR UPDATE`, task.DocumentID).Scan(&status)
	default:
		return false, fmt.Errorf("unknown outbound settlement kind %q", task.Kind)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return status != outbound_settlement.StatusRecorded, nil
}

func updateSettledOutbound(ctx context.Context, q providerBalanceQuerier, task outboundSettlementTask, nativeTokenID id.ID, receipt crypto.ChainReceipt, result outbound_settlement.ChainResult, failed bool) error {
	withdrawalStatus := crypto_withdrawal.WithdrawalStatusConfirmed
	requestStatus := withdrawal_request.StatusConfirmed
	treasuryStatus := treasury_withdrawal.StatusConfirmed
	if failed {
		withdrawalStatus = crypto_withdrawal.WithdrawalStatusFailed
		requestStatus = withdrawal_request.StatusFailed
		treasuryStatus = treasury_withdrawal.StatusFailed
	}
	var energyUsed, netUsage, netFee, energyFee int64
	for _, component := range receipt.Fee.Components {
		componentAmount, err := component.Amount.Int64Exact()
		if err != nil {
			return fmt.Errorf("TRON settlement fee component %s: %w", component.Name, err)
		}
		switch component.Name {
		case "energy_fee":
			energyUsed, energyFee = component.Quantity, componentAmount
		case "bandwidth_fee":
			netUsage, netFee = component.Quantity, componentAmount
		}
	}
	args := []any{nativeTokenID, receipt.Fee.TotalFee, result, energyUsed,
		netFee, energyFee, netUsage, outbound_settlement.StatusRecorded}
	switch task.Kind {
	case settlementWithdrawal:
		if _, err := q.Exec(ctx, `UPDATE doc_crypto_withdrawals SET network_fee_token_id=$2,network_fee=$3,
			chain_result=$4,energy_used=$5,net_fee=$6,energy_fee=$7,net_usage=$8,
			settlement_status=$9,settlement_checked_at=NOW(),settlement_error='',
			settlement_claimed_by='',settlement_claimed_at=NULL,settlement_lease_expires_at=NULL,
			status=$10,updated_at=NOW(),version=version+1 WHERE id=$1`,
			append([]any{task.DocumentID}, append(args, withdrawalStatus)...)...); err != nil {
			return err
		}
		if !id.IsNil(task.LineID) {
			return nil
		}
		reason := ""
		if failed {
			reason = "chain transaction failed: " + result.String()
		}
		_, err := q.Exec(ctx, `UPDATE doc_withdrawal_requests SET status=$2,rejection_reason=$3,
			updated_at=NOW(),version=version+1 WHERE id=$1`, task.RequestID, requestStatus, reason)
		return err
	case settlementSweep:
		if _, err := q.Exec(ctx, `UPDATE doc_crypto_sweeps__lines SET network_fee_token_id=$2,network_fee=$3,
			chain_result=$4,energy_used=$5,net_fee=$6,energy_fee=$7,net_usage=$8,
			settlement_status=$9,settlement_checked_at=NOW(),settlement_error='',
			settlement_claimed_by='',settlement_claimed_at=NULL,settlement_lease_expires_at=NULL,
			confirmed=$10 WHERE line_id=$1`, append(append([]any{task.LineID}, args...), !failed)...); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `UPDATE doc_crypto_sweeps s SET status=CASE
			WHEN EXISTS(SELECT 1 FROM doc_crypto_sweeps__lines l WHERE l.document_id=s.id AND l.settlement_status=$2 AND l.chain_result<>$3) THEN $4
			WHEN NOT EXISTS(SELECT 1 FROM doc_crypto_sweeps__lines l WHERE l.document_id=s.id AND l.settlement_status<>$2) THEN $5
			ELSE s.status END,
			total_fee=(SELECT COALESCE(SUM(network_fee),0) FROM doc_crypto_sweeps__lines WHERE document_id=s.id),
			updated_at=NOW(),version=version+1 WHERE s.id=$1`,
			task.DocumentID,
			outbound_settlement.StatusRecorded,
			outbound_settlement.ChainResultSuccess,
			crypto_sweep.SweepStatusPartialFailed,
			crypto_sweep.SweepStatusConfirmed,
		)
		return err
	case settlementTreasury:
		_, err := q.Exec(ctx, `UPDATE doc_treasury_withdrawals SET network_fee_token_id=$2,network_fee=$3,
			chain_result=$4,energy_used=$5,net_fee=$6,energy_fee=$7,net_usage=$8,
			settlement_status=$9,settlement_checked_at=NOW(),settlement_error='',
			settlement_claimed_by='',settlement_claimed_at=NULL,settlement_lease_expires_at=NULL,
			status=$10,rejection_reason=CASE WHEN $10=$11 THEN 'chain transaction failed: '||$4 ELSE rejection_reason END,
			updated_at=NOW(),version=version+1 WHERE id=$1`,
			append([]any{task.DocumentID}, append(args, treasuryStatus, treasury_withdrawal.StatusFailed)...)...)
		return err
	default:
		return fmt.Errorf("unknown outbound settlement kind %q", task.Kind)
	}
}
