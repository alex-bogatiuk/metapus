package outbound_repo

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

func legacySettlementConflict() error {
	return apperror.NewConflict("legacy outbound settlement differs from its original basis or financial history; reconciliation required")
}

// Compare the worker snapshot to the posted owner before accepting a terminal
// replay or using any of its IDs as compensation destinations.
func loadLegacySettlementBasis(ctx context.Context, t outboundSettlementTask) (int, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var model int
	var matches bool
	var err error
	switch t.Kind {
	case settlementWithdrawal:
		err = q.QueryRow(ctx, `SELECT c.accounting_model_version,
 c.posted AND c.posted_version=1 AND c.token_id=$2 AND c.source_wallet_id=$3 AND c.tx_hash=$4 AND c.amount=$5 AND c.merchant_id=$6 AND a.network_id=$7
 AND EXISTS(SELECT 1 FROM doc_withdrawal_requests r WHERE r.id=$8 AND r.merchant_id=$6 AND r.token_id=$2 AND r.posted AND r.posted_version=$9
 AND (($10::uuid='00000000-0000-0000-0000-000000000000'::uuid AND r.withdrawal_id=c.id AND c.basis_type='WithdrawalRequest' AND c.basis_id=r.id AND r.amount=c.amount)
 OR EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines l WHERE l.id=$10 AND l.request_id=r.id AND l.withdrawal_id=c.id AND l.wallet_id=$3 AND l.token_id=$2 AND l.amount=$5 AND c.basis_type='CryptoAMLWithdrawalLine' AND c.basis_id=l.id)))
 FROM doc_crypto_withdrawals c JOIN cat_tokens a ON a.id=c.token_id WHERE c.id=$1`, t.DocumentID, t.OperationTokenID, t.SourceWalletID, t.TxHash, t.Amount, t.MerchantID, t.NetworkID, t.RequestID, t.PostedVersion, t.LineID).Scan(&model, &matches)
	case settlementSweep:
		err = q.QueryRow(ctx, `SELECT s.accounting_model_version,s.posted AND s.posted_version=$2 AND s.token_id=$3 AND s.hot_wallet_id=$4 AND a.network_id=$5
 AND EXISTS(SELECT 1 FROM doc_crypto_sweep_lines l WHERE l.document_id=s.id AND l.line_id=$6 AND l.wallet_id=$7 AND l.tx_hash=$8 AND l.amount=$9)
 FROM doc_crypto_sweeps s JOIN cat_tokens a ON a.id=s.token_id WHERE s.id=$1`, t.DocumentID, t.PostedVersion, t.OperationTokenID, t.HotWalletID, t.NetworkID, t.LineID, t.SourceWalletID, t.TxHash, t.Amount).Scan(&model, &matches)
	case settlementTreasury:
		err = q.QueryRow(ctx, `SELECT d.accounting_model_version,d.posted AND d.posted_version=$2 AND d.token_id=$3 AND d.source_wallet_id=$4 AND a.network_id=$5 AND d.tx_hash=$6 AND d.amount=$7
 FROM doc_treasury_withdrawals d JOIN cat_tokens a ON a.id=d.token_id WHERE d.id=$1`, t.DocumentID, t.PostedVersion, t.OperationTokenID, t.SourceWalletID, t.NetworkID, t.TxHash, t.Amount).Scan(&model, &matches)
	default:
		return 0, legacySettlementConflict()
	}
	if err != nil {
		return 0, err
	}
	// Model 2 has independent financial admission and SQL owner guards.
	if model == 1 && !matches {
		return model, legacySettlementConflict()
	}
	return model, nil
}

// Must precede FeeBudget.Settle as well as workflow rows. Direct repository
// callers use the same method before entering the financial write sequence.
func (r *OutboundRepo) PrepareLegacyOutboundSettlement(ctx context.Context, t outboundSettlementTask) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("outbound settlement requires a transaction")
	}
	model, err := loadLegacySettlementBasis(ctx, t)
	if err != nil || model != 1 {
		return err
	}
	q := tm.GetQuerier(ctx)
	var isolation string
	if err := q.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
		return err
	}
	if isolation != "read committed" {
		return apperror.NewValidation("legacy outbound settlement requires read committed isolation")
	}
	switch t.Kind {
	case settlementWithdrawal:
		if !id.IsNil(t.LineID) {
			return crypto_repo.NewAMLRepository().LockAMLWithdrawalRequest(ctx, t.RequestID)
		}
		basis, err := prepareWithdrawalResources(ctx, t.RequestID)
		if err != nil {
			return err
		}
		_, err = basis.lockWalletSet(ctx, []id.ID{t.SourceWalletID}, false)
		return err
	case settlementTreasury:
		return lockExecutionResources(ctx, "TreasuryWithdrawal", t.DocumentID, nil)
	case settlementSweep:
		var attempts []id.ID
		const keys = `SELECT ARRAY(SELECT id FROM (SELECT line_id id FROM doc_crypto_sweep_lines WHERE document_id=$1 UNION SELECT id FROM reg_crypto_sweep_attempts WHERE sweep_id=$1) k ORDER BY id)`
		if err := q.QueryRow(ctx, keys, t.DocumentID).Scan(&attempts); err != nil {
			return err
		}
		locker := postgres.NewDocLocker()
		for _, key := range attempts {
			if err := locker.LockDocument(ctx, "CryptoSweepAttempt", key); err != nil {
				return err
			}
		}
		if err := locker.LockDocument(ctx, "CryptoSweep", t.DocumentID); err != nil {
			return err
		}
		var current []id.ID
		if err := q.QueryRow(ctx, keys, t.DocumentID).Scan(&current); err != nil {
			return err
		}
		if len(current) != len(attempts) {
			return legacySettlementConflict()
		}
		for i := range current {
			if current[i] != attempts[i] {
				return legacySettlementConflict()
			}
		}
		_, err := q.Exec(ctx, `SELECT fn_crypto_lock_custody_wallets(ARRAY(
 SELECT wallet_id FROM doc_crypto_sweep_lines WHERE document_id=$1
 UNION SELECT hot_wallet_id FROM doc_crypto_sweeps WHERE id=$1
 UNION SELECT payer_wallet_id FROM reg_crypto_fee_reservations WHERE basis_type='CryptoSweepAttempt' AND basis_id=ANY($2::uuid[])
 UNION SELECT source_wallet_id FROM doc_crypto_native_fundings WHERE basis_type='CryptoSweepAttempt' AND basis_id=ANY($2::uuid[]) AND status NOT IN ('settled','failed')
 UNION SELECT destination_wallet_id FROM doc_crypto_native_fundings WHERE basis_type='CryptoSweepAttempt' AND basis_id=ANY($2::uuid[]) AND status NOT IN ('settled','failed')))`, t.DocumentID, attempts)
		return err
	}
	return legacySettlementConflict()
}

func legacyCompensationKeys(t outboundSettlementTask) (recorder, source, other id.ID) {
	if t.Kind == settlementWithdrawal && !id.IsNil(t.LineID) {
		return t.LineID, t.LineID, t.LineID
	}
	basis := t.DocumentID
	if t.Kind == settlementSweep {
		basis = t.LineID
	}
	recorder = deterministicMovementID(basis, "failed-outbound:"+t.TxHash)
	source = deterministicMovementID(recorder, "source-receipt")
	if t.Kind == settlementSweep {
		other = deterministicMovementID(recorder, "hot-expense")
	}
	if t.Kind == settlementWithdrawal {
		other = deterministicMovementID(t.RequestID, "failed-withdrawal-merchant:"+t.TxHash)
	}
	return
}

func legacyCompensationExists(ctx context.Context, q providerBalanceQuerier, t outboundSettlementTask) (bool, error) {
	recorder, source, other := legacyCompensationKeys(t)
	var exists bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE line_id=$2 OR line_id=$3 OR (recorder_id=$1 AND recorder_type IN ('ChainOutboundStorno','AMLWithdrawalRevert')))
 OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE line_id=$3)
 OR EXISTS(SELECT 1 FROM reg_crypto_fee_movements WHERE recorder_id=$1)`, recorder, source, other).Scan(&exists)
	return exists, err
}

func validateLegacyDebit(ctx context.Context, q providerBalanceQuerier, t outboundSettlementTask) error {
	var matches bool
	if t.Kind == settlementSweep {
		// Broadcast posting credited the whole hot wallet total, whereas each
		// settlement compensates just its source line. Compare grouped totals.
		err := q.QueryRow(ctx, `WITH expected AS (
 SELECT 'expense'::text kind,l.wallet_id wallet,s.token_id token,sum(l.amount) amount FROM doc_crypto_sweep_lines l JOIN doc_crypto_sweeps s ON s.id=l.document_id WHERE s.id=$1 GROUP BY l.wallet_id,s.token_id
 UNION ALL SELECT 'receipt',hot_wallet_id,token_id,total_amount FROM doc_crypto_sweeps WHERE id=$1), actual AS (
 SELECT record_type kind,wallet_id wallet,token_id token,sum(amount) amount FROM reg_crypto_balance_movements WHERE recorder_id=$1 AND recorder_type='CryptoSweep' AND recorder_version=$2 AND reversal_of_line_id IS NULL GROUP BY record_type,wallet_id,token_id)
 SELECT NOT EXISTS(SELECT 1 FROM expected e FULL JOIN actual a USING(kind,wallet,token) WHERE e.amount IS DISTINCT FROM a.amount)
 AND (SELECT total_amount FROM doc_crypto_sweeps WHERE id=$1)=(SELECT sum(amount) FROM doc_crypto_sweep_lines WHERE document_id=$1)`, t.DocumentID, t.PostedVersion).Scan(&matches)
		if err != nil {
			return err
		}
	} else {
		kind := "CryptoWithdrawal"
		version := 1
		if t.Kind == settlementTreasury {
			kind = "TreasuryWithdrawal"
			version = t.PostedVersion
		}
		err := q.QueryRow(ctx, `SELECT count(*)=1 AND COALESCE(bool_and(record_type='expense' AND wallet_id=$4 AND token_id=$5 AND amount=$6 AND reversal_of_line_id IS NULL),FALSE)
 FROM reg_crypto_balance_movements WHERE recorder_id=$1 AND recorder_type=$2 AND recorder_version=$3`, t.DocumentID, kind, version, t.SourceWalletID, t.OperationTokenID, t.Amount).Scan(&matches)
		if err != nil {
			return err
		}
	}
	if !matches {
		return legacySettlementConflict()
	}
	if t.Kind == settlementWithdrawal {
		err := q.QueryRow(ctx, `SELECT count(*)=1 AND COALESCE(bool_and(m.recorder_type='WithdrawalRequest' AND m.record_type='expense' AND m.merchant_id=r.merchant_id AND m.token_id=r.token_id AND m.amount=r.amount AND m.reversal_of_line_id IS NULL),FALSE)
 FROM doc_withdrawal_requests r JOIN reg_crypto_merchant_balance_movements m ON m.recorder_id=r.id AND m.recorder_version=r.posted_version WHERE r.id=$1`, t.RequestID).Scan(&matches)
		if err != nil {
			return err
		}
		if !matches {
			return legacySettlementConflict()
		}
	}
	return nil
}

func validateLegacyCompensation(ctx context.Context, q providerBalanceQuerier, t outboundSettlementTask) error {
	recorder, source, other := legacyCompensationKeys(t)
	kind := "ChainOutboundStorno"
	if t.Kind == settlementWithdrawal && !id.IsNil(t.LineID) {
		kind = "AMLWithdrawalRevert"
	}
	var period time.Time
	var matches bool
	err := q.QueryRow(ctx, `SELECT period,recorder_id=$2 AND recorder_type=$3 AND recorder_version=1 AND record_type='receipt' AND wallet_id=$4 AND token_id=$5 AND amount=$6 AND reversal_of_line_id IS NULL
 FROM reg_crypto_balance_movements WHERE line_id=$1`, source, recorder, kind, t.SourceWalletID, t.OperationTokenID, t.Amount).Scan(&period, &matches)
	if err == pgx.ErrNoRows {
		return legacySettlementConflict()
	}
	if err != nil {
		return err
	}
	if !matches {
		return legacySettlementConflict()
	}
	expected := 1
	if t.Kind == settlementSweep {
		expected = 2
		err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE line_id=$1 AND recorder_id=$2 AND recorder_type='ChainOutboundStorno' AND recorder_version=1 AND record_type='expense' AND wallet_id=$3 AND token_id=$4 AND amount=$5 AND period=$6 AND reversal_of_line_id IS NULL)`, other, recorder, t.HotWalletID, t.OperationTokenID, t.Amount, period).Scan(&matches)
		if err != nil {
			return err
		}
		if !matches {
			return legacySettlementConflict()
		}
	}
	var count int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1 AND recorder_type=$2`, recorder, kind).Scan(&count); err != nil {
		return err
	}
	if count != expected {
		return legacySettlementConflict()
	}
	if t.Kind == settlementWithdrawal {
		err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE line_id=$1 AND recorder_id=$2 AND recorder_type='WithdrawalRequest' AND recorder_version=$3 AND record_type='receipt' AND merchant_id=$4 AND token_id=$5 AND amount=$6 AND period=$7 AND reversal_of_line_id IS NULL)`, other, t.RequestID, t.PostedVersion+1, t.MerchantID, t.OperationTokenID, t.Amount, period).Scan(&matches)
		if err != nil {
			return err
		}
		if !matches {
			return legacySettlementConflict()
		}
	}
	if t.Kind == settlementTreasury {
		// Old generations used random reversal IDs. Compare the exact multiset
		// so a partial old set cannot suppress the remaining fee reversals.
		err = q.QueryRow(ctx, `WITH original AS (
 SELECT merchant_id,token_id,fee_type,amount,CASE WHEN record_type='expense' THEN 'receipt' ELSE 'expense' END record_type,count(*) n
 FROM reg_crypto_fee_movements WHERE recorder_id=$1 AND recorder_type='TreasuryWithdrawal' AND recorder_version=$3 AND reversal_of_line_id IS NULL GROUP BY merchant_id,token_id,fee_type,amount,record_type), reversed AS (
 SELECT merchant_id,token_id,fee_type,amount,record_type,count(*) n FROM reg_crypto_fee_movements WHERE recorder_id=$2
 AND recorder_type='ChainOutboundStorno' AND recorder_version=1 AND period=$4 AND reversal_of_line_id IS NULL GROUP BY merchant_id,token_id,fee_type,amount,record_type)
 SELECT NOT EXISTS(SELECT 1 FROM original o FULL JOIN reversed r USING(merchant_id,token_id,fee_type,amount,record_type) WHERE o.n IS DISTINCT FROM r.n)
 AND (SELECT count(*) FROM reg_crypto_fee_movements WHERE recorder_id=$2)=(SELECT COALESCE(sum(n),0) FROM original)`, t.DocumentID, recorder, t.PostedVersion, period).Scan(&matches)
		if err != nil {
			return err
		}
		if !matches {
			return legacySettlementConflict()
		}
	}
	return nil
}

func validateLegacyRecordedOutcome(ctx context.Context, q providerBalanceQuerier, t outboundSettlementTask, r crypto.ChainReceipt) error {
	var energyUsed, netFee, energyFee, netUsage int64
	for _, component := range r.Fee.Components {
		amount, err := component.Amount.Int64Exact()
		if err != nil {
			return legacySettlementConflict()
		}
		switch component.Name {
		case "energy_fee":
			energyUsed, energyFee = component.Quantity, amount
		case "bandwidth_fee":
			netUsage, netFee = component.Quantity, amount
		}
	}
	result := "SUCCESS"
	if r.Status == crypto.ReceiptReverted {
		result = "REVERTED"
	}
	query := ""
	switch t.Kind {
	case settlementWithdrawal:
		query = `SELECT COALESCE(chain_result=$2 AND network_fee_token_id=$3 AND network_fee=$4 AND energy_used=$5 AND net_fee=$6 AND energy_fee=$7 AND net_usage=$8,FALSE) FROM doc_crypto_withdrawals WHERE id=$1`
	case settlementSweep:
		query = `SELECT COALESCE(chain_result=$2 AND network_fee_token_id=$3 AND network_fee=$4 AND energy_used=$5 AND net_fee=$6 AND energy_fee=$7 AND net_usage=$8,FALSE) FROM doc_crypto_sweep_lines WHERE line_id=$1`
	case settlementTreasury:
		query = `SELECT COALESCE(chain_result=$2 AND network_fee_token_id=$3 AND network_fee=$4 AND energy_used=$5 AND net_fee=$6 AND energy_fee=$7 AND net_usage=$8,FALSE) FROM doc_treasury_withdrawals WHERE id=$1`
	}
	key := t.DocumentID
	if t.Kind == settlementSweep {
		key = t.LineID
	}
	var matches bool
	if err := q.QueryRow(ctx, query, key, result, r.Fee.NativeAsset.TokenID, r.Fee.TotalFee, energyUsed, netFee, energyFee, netUsage).Scan(&matches); err != nil {
		return err
	}
	if !matches {
		return legacySettlementConflict()
	}
	if r.Status == crypto.ReceiptReverted {
		if err := validateLegacyDebit(ctx, q, t); err != nil {
			return err
		}
		return validateLegacyCompensation(ctx, q, t)
	}
	exists, err := legacyCompensationExists(ctx, q, t)
	if err != nil {
		return err
	}
	if exists {
		return legacySettlementConflict()
	}
	return nil
}
