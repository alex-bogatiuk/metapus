package crypto_repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/types"
)

// RegisterAMLOperatingReceipt records a control-plane attestation of service
// capital. SQL requires an external, verified native receipt on an owned fee
// wallet. It creates no merchant credit or processing fee income.
func (r *AMLRepository) RegisterAMLOperatingReceipt(ctx context.Context, effectID id.ID, reference, actor string) (id.ID, error) {
	var receiptID id.ID
	if id.IsNil(effectID) || strings.TrimSpace(reference) == "" || strings.TrimSpace(actor) == "" {
		return receiptID, apperror.NewValidation("operating receipt requires an effect, evidence reference and actor")
	}
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var wallet id.ID
		if err := q.QueryRow(ctx, `SELECT wallet_id FROM reg_crypto_aml_effects WHERE id=$1`, effectID).Scan(&wallet); err != nil {
			return err
		}
		if err := NewCustodyRecognitionRepo().LockWallets(ctx, wallet); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `SELECT s.version FROM reg_crypto_aml_effects e JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=e.merchant_id AND s.network_id=e.network_id AND s.token_id=e.token_id WHERE e.id=$1 FOR UPDATE OF s`, effectID); err != nil {
			return err
		}
		var storedReference, storedActor string
		err := q.QueryRow(ctx, `SELECT id,evidence_reference,recorded_by FROM reg_crypto_aml_operating_receipts WHERE effect_id=$1`, effectID).Scan(&receiptID, &storedReference, &storedActor)
		if err == nil {
			if reference != storedReference || actor != storedActor {
				return apperror.NewConflict("operating receipt attestation changed")
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		receiptID = id.New()
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_operating_receipts(id,effect_id,merchant_id,network_id,token_id,wallet_id,tx_hash,event_kind,event_index,amount,movement_id,evidence_reference,recorded_by)
 SELECT $2,id,merchant_id,network_id,token_id,wallet_id,tx_hash,event_kind,event_index,amount,$2,$3,$4 FROM reg_crypto_aml_effects WHERE id=$1`, effectID, receiptID, reference, actor)
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
 SELECT movement_id,id,'AMLOperatingReceipt',1,NOW(),'receipt',wallet_id,token_id,amount FROM reg_crypto_aml_operating_receipts WHERE id=$1`, receiptID)
		if err != nil {
			return err
		}
		tag, err := q.Exec(ctx, `INSERT INTO reg_crypto_aml_resource_facts(kind,merchant_id,network_id,token_id,destination_wallet_id,amount,receipt_id,effect_id,destination_movement_id)
 SELECT 'seed',merchant_id,network_id,token_id,wallet_id,amount,id,effect_id,movement_id FROM reg_crypto_aml_operating_receipts WHERE id=$1`, receiptID)
		if err == nil && tag.RowsAffected() != 1 {
			return apperror.NewNotFound("verified operating receipt", effectID)
		}
		return err
	})
	return receiptID, err
}

// LockAMLResourceScopes precedes any reservation row lock. It locks the
// complete payer owner/network scope set of the reservation.
func LockAMLResourceScopes(ctx context.Context, reservationID id.ID) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `SELECT fn_crypto_aml_lock_resource_scopes($1)`, reservationID)
	return err
}

// EnsureAMLResourceBudget atomically partitions the existing fee reservation
// from operating capital. It never uses merchant principal to cover gas.
func (r *AMLRepository) EnsureAMLResourceBudget(ctx context.Context, reservationID id.ID) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error { return r.ensureAMLResourceBudget(ctx, reservationID) })
}
func (r *AMLRepository) ensureAMLResourceBudget(ctx context.Context, reservationID id.ID) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	// The resource guard verifies the funding-to-fee identity. Bind it in
	// this same reservation transaction before the guard runs, rather than
	// waiting for the JIT service to link it after Reserve returns.
	if _, err := q.Exec(ctx, `UPDATE doc_crypto_native_fundings funding SET fee_reservation_id=fee.id,status='reserved',last_error=''
 FROM reg_crypto_fee_reservations fee JOIN cat_wallets w ON w.id=fee.payer_wallet_id
 JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=w.custody_merchant_id AND scope.network_id=fee.network_id AND scope.token_id=fee.native_token_id AND scope.enabled
 WHERE fee.id=$1 AND fee.basis_type='CryptoNativeFunding' AND fee.purpose='native_funding' AND funding.id=fee.basis_id
 AND funding.status IN ('created','reserved') AND (funding.fee_reservation_id IS NULL OR funding.fee_reservation_id=fee.id)
 AND funding.source_wallet_id=fee.payer_wallet_id AND funding.network_id=fee.network_id AND funding.native_token_id=fee.native_token_id AND funding.amount_minor=fee.reserved_principal_minor`, reservationID); err != nil {
		return err
	}
	var request aml.ResourceBudgetRequest
	err := q.QueryRow(ctx, `SELECT f.id,w.custody_merchant_id,p.revision,risk.revision,s.version,risk.risk_class,
 EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines line JOIN reg_crypto_aml_withdrawal_plans plan ON plan.request_id=line.request_id WHERE f.basis_type='CryptoAMLWithdrawalLine' AND f.purpose='withdrawal' AND line.id=f.basis_id AND plan.risk_accepted AND plan.risk_class=risk.risk_class)
 FROM reg_crypto_fee_reservations f JOIN cat_wallets w ON w.id=f.payer_wallet_id
 JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=f.network_id AND s.token_id=f.native_token_id AND s.enabled
 LEFT JOIN reg_crypto_aml_resource_positions p ON p.wallet_id=w.id AND p.token_id=f.native_token_id
 LEFT JOIN reg_crypto_wallet_risk_states risk ON risk.wallet_id=w.id AND risk.token_id=f.native_token_id
 WHERE f.id=$1 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets b WHERE b.fee_reservation_id=f.id)`, reservationID).Scan(&request.FeeReservationID, &request.MerchantID, &request.ResourceRevision, &request.RiskRevision, &request.ScopeVersion, &request.Class, &request.RiskExportAccepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = r.ReserveAMLResourceBudget(ctx, request)
	return err
}

func (r *AMLRepository) ReserveAMLResourceBudget(ctx context.Context, request aml.ResourceBudgetRequest) (aml.ResourceBudget, error) {
	var result aml.ResourceBudget
	if err := request.Validate(); err != nil {
		return result, err
	}
	raw, err := json.MarshalDeterministic(request)
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	err = postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		// Same ordered scope set as signing and fee settlement, before positions
		// or fee rows. The reservation's identity is rechecked by the SQL guard.
		if _, err := q.Exec(ctx, `SELECT s.version FROM reg_crypto_fee_reservations f JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=$2 AND s.network_id=f.network_id WHERE f.id=$1 ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s`, request.FeeReservationID, request.MerchantID); err != nil {
			return err
		}
		var storedDigest string
		err := q.QueryRow(ctx, `SELECT id,status,funding_amount,max_fee,request_digest FROM reg_crypto_aml_resource_budgets WHERE fee_reservation_id=$1`, request.FeeReservationID).Scan(&result.ID, &result.Status, &result.Requirement.Funding, &result.Requirement.MaxFee, &storedDigest)
		if err == nil {
			if digest != storedDigest {
				return apperror.NewConflict("operating resource request changed; create a new attempt")
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var networkID, tokenID, walletID id.ID
		var fundingID *id.ID
		var available types.RawAmount
		err = q.QueryRow(ctx, `SELECT f.network_id,f.native_token_id,f.payer_wallet_id,funding.id,COALESCE(funding.amount_minor,0),f.max_fee_minor,
 p.amount-COALESCE((SELECT SUM(CASE WHEN fee.strategy<>'resource_rental' THEN b.max_fee ELSE 0 END + b.funding_amount) FROM reg_crypto_aml_resource_budgets b JOIN reg_crypto_fee_reservations fee ON fee.id=b.fee_reservation_id WHERE b.wallet_id=p.wallet_id AND b.token_id=p.token_id AND b.status='reserved'),0),
 f.strategy='resource_rental'
 FROM reg_crypto_fee_reservations f JOIN reg_crypto_aml_resource_positions p ON p.wallet_id=f.payer_wallet_id AND p.token_id=f.native_token_id
 LEFT JOIN doc_crypto_native_fundings funding ON f.purpose='native_funding' AND funding.id=f.basis_id AND funding.fee_reservation_id=f.id WHERE f.id=$1 FOR UPDATE OF p`, request.FeeReservationID).
			Scan(&networkID, &tokenID, &walletID, &fundingID, &result.Requirement.Funding, &result.Requirement.MaxFee, &available, &result.Requirement.RentalCovered)
		if err != nil {
			return err
		}
		if err := result.Requirement.ValidateAvailable(available); err != nil {
			return err
		}
		return q.QueryRow(ctx, `INSERT INTO reg_crypto_aml_resource_budgets(fee_reservation_id,merchant_id,network_id,token_id,wallet_id,funding_id,funding_amount,max_fee,resource_revision,risk_revision,scope_version,risk_class,request_digest)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id,status`, request.FeeReservationID, request.MerchantID, networkID, tokenID, walletID, fundingID,
			result.Requirement.Funding, result.Requirement.MaxFee, request.ResourceRevision, request.RiskRevision, request.ScopeVersion, request.Class, digest).Scan(&result.ID, &result.Status)
	})
	return result, err
}

// CancelAMLResourceBudget releases both reservations atomically, but only
// before an authorization exists. An uncertain signing outcome stays reserved.
func (r *AMLRepository) CancelAMLResourceBudget(ctx context.Context, budgetID id.ID, reason, actor string) error {
	if id.IsNil(budgetID) || len(strings.TrimSpace(reason)) < 3 || len(reason) > 500 || strings.TrimSpace(actor) == "" {
		return apperror.NewValidation("resource cancellation requires a reason and actor")
	}
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var reservationID id.ID
		if err := q.QueryRow(ctx, `SELECT fee_reservation_id FROM reg_crypto_aml_resource_budgets WHERE id=$1`, budgetID).Scan(&reservationID); err != nil {
			return err
		}
		if err := LockAMLResourceScopes(ctx, reservationID); err != nil {
			return err
		}
		var status, feeStatus string
		if err := q.QueryRow(ctx, `SELECT b.status,f.status FROM reg_crypto_aml_resource_budgets b JOIN reg_crypto_fee_reservations f ON f.id=b.fee_reservation_id WHERE b.id=$1 FOR UPDATE OF b,f`, budgetID).Scan(&status, &feeStatus); err != nil {
			return err
		}
		if status == "cancelled" {
			return recordAMLCancellation(ctx, true, budgetID, reason, actor, false)
		}
		if status != "reserved" {
			return apperror.NewConflict("settled operating resources cannot be cancelled")
		}
		if err := recordAMLCancellation(ctx, true, budgetID, reason, actor, true); err != nil {
			return err
		}

		if _, err := q.Exec(ctx, `UPDATE reg_crypto_aml_resource_budgets SET status='cancelled' WHERE id=$1`, budgetID); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `UPDATE reg_crypto_fee_reservations SET status='released',released_at=NOW() WHERE id=$1`, reservationID); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `INSERT INTO reg_crypto_fee_reservation_events(reservation_id,from_status,to_status,reason,amount_minor)
 SELECT id,$3,'released',$2,max_fee_minor FROM reg_crypto_fee_reservations WHERE id=$1`, reservationID, reason, feeStatus)
		return err
	})
}

func (r *AMLRepository) captureAMLResourceBudget(ctx context.Context, budgetID id.ID) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_aml_resource_facts(kind,merchant_id,network_id,token_id,source_wallet_id,amount,budget_id,source_movement_id)
 SELECT 'cost',ready.merchant_id,ready.network_id,ready.token_id,ready.wallet_id,ready.actual_fee,ready.id,ready.cost_movement_id FROM crypto_aml_ready_resource_budgets ready
 JOIN reg_crypto_aml_resource_budgets b ON b.id=ready.id AND b.status='reserved' WHERE ready.id=$1 AND ready.actual_fee>0`, budgetID); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_aml_resource_facts(kind,merchant_id,network_id,token_id,source_wallet_id,destination_wallet_id,amount,budget_id,effect_id,source_movement_id,destination_movement_id)
 SELECT 'funding',ready.merchant_id,ready.network_id,ready.token_id,ready.wallet_id,ready.destination_wallet_id,ready.funding_amount,ready.id,ready.effect_id,ready.funding_source_movement_id,ready.funding_destination_movement_id
 FROM crypto_aml_ready_resource_budgets ready JOIN reg_crypto_aml_resource_budgets b ON b.id=ready.id AND b.status='reserved' WHERE ready.id=$1 AND ready.chain_status='succeeded' AND ready.funding_id IS NOT NULL`, budgetID); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `UPDATE reg_crypto_aml_resource_budgets b SET status=CASE WHEN ready.chain_status='succeeded' THEN 'settled' ELSE 'failed' END,settlement_id=ready.settlement_id,actual_fee=ready.actual_fee
 FROM crypto_aml_ready_resource_budgets ready WHERE ready.id=b.id AND b.id=$1 AND b.status='reserved'`, budgetID)
	return err
}
