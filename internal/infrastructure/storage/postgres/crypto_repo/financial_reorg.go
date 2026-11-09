package crypto_repo

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/infrastructure/storage/postgres"
)

// FinancialReorgRepo persists the incident and operational network hold in the
// same transaction as the orphaned payment marker.
type FinancialReorgRepo struct{}

func NewFinancialReorgRepo() *FinancialReorgRepo { return &FinancialReorgRepo{} }

type NetworkHoldRepo struct{}

func NewNetworkHoldRepo() *NetworkHoldRepo { return &NetworkHoldRepo{} }

func (r *NetworkHoldRepo) HoldNetwork(ctx context.Context, networkID id.ID, reason crypto.OperationalHoldReason, details map[string]any) error {
	if id.IsNil(networkID) || reason == "" {
		return fmt.Errorf("network and hold reason are required")
	}
	encoded, err := json.MarshalDeterministic(details)
	if err != nil {
		return fmt.Errorf("marshal network hold details: %w", err)
	}
	txm := postgres.MustGetTxManager(ctx)
	evidenceKey := fmt.Sprintf("%x", sha256.Sum256([]byte(networkID.String()+"|"+string(reason)+"|"+string(encoded))))
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		if _, err := q.Exec(txCtx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "crypto-network-incident|"+evidenceKey); err != nil {
			return err
		}
		incidentID, err := networkIncident(txCtx, networkID, string(reason), evidenceKey, encoded)
		if err != nil {
			return err
		}
		holds := cryptoapp.NewOperationalHoldService(txm, NewOperationalHoldRepo())
		return holds.Acquire(txCtx, cryptoapp.AcquireOperationalHoldCommand{
			Scope: cryptoapp.OperationalHoldScopeNetwork, ScopeID: networkID, Reason: string(reason),
			OwnerType: "security_incident", OwnerID: incidentID.String(), HeldBy: "crypto-worker",
			Details: details,
		})
	})
}

func (r *FinancialReorgRepo) RecordFinancialReorg(ctx context.Context, payment *crypto_payment.CryptoPayment, event crypto.BlockchainEvent) error {
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		return r.recordFinancialReorg(txCtx, payment, event)
	})
}

func (r *FinancialReorgRepo) recordFinancialReorg(ctx context.Context, payment *crypto_payment.CryptoPayment, event crypto.BlockchainEvent) error {
	if payment == nil || id.IsNil(payment.ID) || id.IsNil(payment.NetworkID) || payment.PostedVersion <= 0 {
		return fmt.Errorf("posted payment and network identity are required")
	}
	requestBasis, err := financialReorgRequestBasis(payment, event)
	if err != nil {
		return err
	}
	if err := lockFinancialReorgBasis(ctx, payment); err != nil {
		return err
	}
	lockQuery := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if _, err := lockQuery.Exec(ctx, "SELECT fn_crypto_lock_receipt_balance($1,$2)", payment.WalletID, payment.TokenID); err != nil {
		return err
	}
	if _, err := lockQuery.Exec(ctx, `SELECT e.id FROM reg_crypto_custody_effect_postings p JOIN reg_crypto_accounting_effects e ON e.id=p.effect_id
		WHERE p.recorder_type='CryptoPayment' AND p.recorder_id=$1 ORDER BY e.id FOR UPDATE OF e`, payment.ID); err != nil {
		return err
	}
	details, err := json.Marshal(map[string]any{
		"invoiceId":         payment.InvoiceID.String(),
		"amount":            payment.Amount.String(),
		"postedVersion":     payment.PostedVersion,
		"blockNumber":       event.BlockNumber,
		"previousBlock":     payment.BlockNumber,
		"previousHash":      payment.BlockHash,
		"observedBlockHash": event.BlockHash,
	})
	if err != nil {
		return fmt.Errorf("marshal financial reorg details: %w", err)
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	incidentID, err := paymentReorgIncident(ctx, payment, details)
	if err != nil {
		return fmt.Errorf("insert financial reorg incident: %w", err)
	}

	// An incident may reappear and be posted again. Deduplicate the financial
	// effect by posting generation, not by chain-event identity.
	var compensationID id.ID
	err = q.QueryRow(ctx, `
		INSERT INTO reg_crypto_reorg_compensations (
			id,incident_id,payment_id,posted_version,observed_block_number,observed_block_hash,request_basis
		) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)
		ON CONFLICT (payment_id,posted_version) DO NOTHING
		RETURNING id
	`, id.New(), incidentID, payment.ID, payment.PostedVersion,
		event.BlockNumber, event.BlockHash, string(requestBasis)).Scan(&compensationID)
	if err == pgx.ErrNoRows {
		var exact bool
		if err := q.QueryRow(ctx, `SELECT incident_id=$3 AND observed_block_number=$4 AND observed_block_hash=$5
 AND (request_basis IS NULL OR request_basis=$6::jsonb) FROM reg_crypto_reorg_compensations WHERE payment_id=$1 AND posted_version=$2`, payment.ID, payment.PostedVersion, incidentID, event.BlockNumber, event.BlockHash, string(requestBasis)).Scan(&exact); err != nil {
			return err
		}
		if !exact {
			return apperror.NewConflict("financial reorg replay differs from its original compensation basis")
		}
		return verifyFinancialReorgComplete(ctx, payment)
	}
	if err != nil {
		return fmt.Errorf("insert financial reorg compensation: %w", err)
	}

	// A financial reorg is compensated with opposite immutable movements. The
	// original posting remains intact and auditable.
	balanceEffect, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_balance_movements (
			line_id,recorder_id,recorder_type,recorder_version,period,
			record_type,wallet_id,token_id,amount,reversal_of_line_id,reversal_reason
		)
		SELECT gen_random_uuid_v7(),$1,'CryptoPaymentReorg',1,NOW(),
		       CASE WHEN record_type='receipt' THEN 'expense' ELSE 'receipt' END,
		       m.wallet_id,m.token_id,m.amount-COALESCE(reversed.amount,0),m.line_id,'posted_payment_reorg'
		FROM reg_crypto_balance_movements m
		LEFT JOIN LATERAL (SELECT SUM(r.amount) AS amount FROM reg_crypto_balance_movements r
		    WHERE r.reversal_of_line_id=m.line_id) reversed ON TRUE
		WHERE m.recorder_id=$2 AND m.recorder_type='CryptoPayment' AND m.recorder_version=$3
		  AND m.amount>COALESCE(reversed.amount,0)
	`, compensationID, payment.ID, payment.PostedVersion)
	if err != nil {
		return fmt.Errorf("record crypto balance reorg reversal: %w", err)
	}
	if balanceEffect.RowsAffected() == 0 {
		var originalCount int
		if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_balance_movements
			WHERE recorder_id=$1 AND recorder_type='CryptoPayment' AND recorder_version=$2`,
			payment.ID, payment.PostedVersion).Scan(&originalCount); err != nil {
			return err
		}
		if originalCount == 0 {
			return fmt.Errorf("posted payment %s version %d has no custody movement to compensate",
				payment.ID, payment.PostedVersion)
		}
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_merchant_balance_movements (
			line_id,recorder_id,recorder_type,recorder_version,period,
			record_type,merchant_id,token_id,amount,reversal_of_line_id,reversal_reason
		)
		SELECT gen_random_uuid_v7(),$1,'CryptoPaymentReorg',1,NOW(),
		       CASE WHEN record_type='receipt' THEN 'expense' ELSE 'receipt' END,
		       m.merchant_id,m.token_id,m.amount-COALESCE(reversed.amount,0),m.line_id,'posted_payment_reorg'
		FROM reg_crypto_merchant_balance_movements m
		LEFT JOIN LATERAL (SELECT SUM(r.amount) AS amount FROM reg_crypto_merchant_balance_movements r
		    WHERE r.reversal_of_line_id=m.line_id) reversed ON TRUE
		WHERE m.recorder_id=$2 AND m.recorder_type='CryptoPayment' AND m.recorder_version=$3
		  AND m.amount>COALESCE(reversed.amount,0)
	`, compensationID, payment.ID, payment.PostedVersion); err != nil {
		return fmt.Errorf("record merchant balance reorg reversal: %w", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_fee_movements (
			line_id,recorder_id,recorder_type,recorder_version,period,
			record_type,merchant_id,token_id,fee_type,amount,reversal_of_line_id,reversal_reason
		)
		SELECT gen_random_uuid_v7(),$1,'CryptoPaymentReorg',1,NOW(),
		       CASE WHEN record_type='receipt' THEN 'expense' ELSE 'receipt' END,
		       m.merchant_id,m.token_id,m.fee_type,m.amount-COALESCE(reversed.amount,0),m.line_id,'posted_payment_reorg'
		FROM reg_crypto_fee_movements m
		LEFT JOIN LATERAL (SELECT SUM(r.amount) AS amount FROM reg_crypto_fee_movements r
		    WHERE r.reversal_of_line_id=m.line_id) reversed ON TRUE
		WHERE m.recorder_id=$2 AND m.recorder_type='CryptoPayment' AND m.recorder_version=$3
		  AND m.amount>COALESCE(reversed.amount,0)
	`, compensationID, payment.ID, payment.PostedVersion); err != nil {
		return fmt.Errorf("record crypto fee reorg reversal: %w", err)
	}
	if err := verifyFinancialReorgComplete(ctx, payment); err != nil {
		return err
	}
	holds := cryptoapp.NewOperationalHoldService(postgres.MustGetTxManager(ctx), NewOperationalHoldRepo())
	if err := holds.Acquire(ctx, cryptoapp.AcquireOperationalHoldCommand{
		Scope: cryptoapp.OperationalHoldScopeNetwork, ScopeID: payment.NetworkID,
		Reason: "posted_payment_reorg", OwnerType: "security_incident", OwnerID: incidentID.String(),
		HeldBy: "crypto-worker", Details: map[string]any{
			"invoiceId": payment.InvoiceID.String(), "paymentId": payment.ID.String(),
			"txHash": payment.TxHash, "eventKind": payment.EventKind, "eventIndex": payment.EventIndex,
		},
	}); err != nil {
		return fmt.Errorf("hold network after financial reorg: %w", err)
	}
	return nil
}

var _ crypto.FinancialReorgRecorder = (*FinancialReorgRepo)(nil)
var _ crypto.NetworkOperationalHoldRecorder = (*NetworkHoldRepo)(nil)
