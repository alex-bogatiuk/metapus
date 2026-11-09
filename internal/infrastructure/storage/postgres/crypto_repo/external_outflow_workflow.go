package crypto_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

// ExternalOutflowWorkflowRepo contains persistence only; state transitions and
// maker-checker decisions live in cryptoapp.ExternalOutflowService.
type ExternalOutflowWorkflowRepo struct{}

func NewExternalOutflowWorkflowRepo() *ExternalOutflowWorkflowRepo {
	return &ExternalOutflowWorkflowRepo{}
}

const externalOutflowColumns = `
	id,network_id,token_id,source_wallet_id,tx_hash,event_kind,event_index,
	from_address,to_address,contract_address,amount,block_number,block_hash,
	confirmations,required_confs,finality_status,status,classification,
	matched_basis_type,matched_basis_id,classification_reason,classification_evidence,
	classified_by,classified_at,resolved_by,resolved_at,resolution,detected_at,last_observed_at`

func (r *ExternalOutflowWorkflowRepo) ListExternalOutflows(ctx context.Context, status string, limit int) ([]crypto.ExternalOutflow, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `
		SELECT `+externalOutflowColumns+`
		FROM reg_crypto_external_outflows
		WHERE ($1='' OR status=$1)
		ORDER BY detected_at DESC,id
		LIMIT $2
	`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list external outflows: %w", err)
	}
	defer rows.Close()
	result := make([]crypto.ExternalOutflow, 0, limit)
	for rows.Next() {
		outflow, err := scanExternalOutflow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *outflow)
	}
	return result, rows.Err()
}

func (r *ExternalOutflowWorkflowRepo) GetExternalOutflow(ctx context.Context, outflowID id.ID, forUpdate bool) (*crypto.ExternalOutflow, error) {
	suffix := ""
	if forUpdate {
		if err := lockExternalOutflowResources(ctx, outflowID); err != nil {
			return nil, err
		}
		suffix = " FOR UPDATE"
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	outflow, err := scanExternalOutflow(q.QueryRow(ctx, `SELECT `+externalOutflowColumns+`
		FROM reg_crypto_external_outflows WHERE id=$1`+suffix, outflowID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperror.NewNotFound("external outflow", outflowID.String())
		}
		return nil, fmt.Errorf("get external outflow: %w", err)
	}
	if forUpdate {
		// The aggregate details are reloaded after the transition. Avoid taking
		// unrelated row locks while the command decision is being made.
		_ = r.loadAdjustment(ctx, q, outflow)
		return outflow, nil
	}
	if err := r.loadOutflowDetails(ctx, q, outflow); err != nil {
		return nil, err
	}
	return outflow, nil
}

func (r *ExternalOutflowWorkflowRepo) MatchExactBasis(ctx context.Context, outflow *crypto.ExternalOutflow, basisType string, basisID id.ID) (bool, error) {
	var matches bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		WITH exact_effect AS (
		  SELECT e.id
		  FROM reg_crypto_expected_chain_effects e
		  JOIN reg_crypto_signing_authorizations a ON a.id=e.signing_authorization_id
		  JOIN reg_crypto_external_outflows o ON o.id=$6
		  WHERE a.basis_type=$1 AND a.basis_id=$2
		    AND e.network_id=$3 AND e.tx_hash=$4 AND e.event_kind=$5
		    AND e.token_id IS NOT DISTINCT FROM o.token_id
		    AND e.source_address_key=o.source_address_key
		    AND e.destination_address_key=o.destination_address_key
		    AND e.contract_address_key=o.contract_address_key
		    AND e.amount=$7
		    AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_chain_effect_matches used
		      WHERE used.expected_effect_id=e.id
		        AND (used.network_id,used.tx_hash,used.event_kind,used.event_index)
		            IS DISTINCT FROM (o.network_id,o.tx_hash,o.event_kind,o.event_index)
		    )
		  ORDER BY e.ordinal
		  LIMIT 1
		), inserted AS (
		  INSERT INTO reg_crypto_chain_effect_matches(
		    expected_effect_id,network_id,tx_hash,event_kind,event_index,matched_source
		  )
		  SELECT e.id,o.network_id,o.tx_hash,o.event_kind,o.event_index,'reconciliation-classification'
		  FROM exact_effect e
		  JOIN reg_crypto_external_outflows o ON o.id=$6
		  ON CONFLICT DO NOTHING
		  RETURNING expected_effect_id
		)
		SELECT EXISTS(SELECT 1 FROM inserted) OR EXISTS(
		  SELECT 1
		  FROM exact_effect e
		  JOIN reg_crypto_chain_effect_matches matched ON matched.expected_effect_id=e.id
		  JOIN reg_crypto_external_outflows o ON o.id=$6
		  WHERE (matched.network_id,matched.tx_hash,matched.event_kind,matched.event_index)
		        = (o.network_id,o.tx_hash,o.event_kind,o.event_index)
		)
	`, basisType, basisID, outflow.NetworkID, outflow.TxHash, outflow.EventKind, outflow.ID, outflow.Amount).Scan(&matches)
	return matches, err
}

func (r *ExternalOutflowWorkflowRepo) HasFalsePositiveProof(ctx context.Context, outflow *crypto.ExternalOutflow) (bool, error) {
	var proved bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT finality_status IN ('removed','reorged') AND block_hash<>''
		FROM reg_crypto_external_outflows WHERE id=$1
	`, outflow.ID).Scan(&proved)
	return proved, err
}

func (r *ExternalOutflowWorkflowRepo) ClassifyExternalOutflow(ctx context.Context, outflow *crypto.ExternalOutflow, command cryptoapp.ClassifyExternalOutflowCommand, actorID id.ID) error {
	evidence, err := json.Marshal(command.Evidence)
	if err != nil {
		return fmt.Errorf("marshal classification evidence: %w", err)
	}
	var basisID any
	if command.MatchedBasisID != nil {
		basisID = *command.MatchedBasisID
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	tag, err := q.Exec(ctx, `
		UPDATE reg_crypto_external_outflows
		SET status='classified',classification=$2,matched_basis_type=$3,matched_basis_id=$4,
		    classification_reason=$5,classification_evidence=$6::jsonb,
		    classified_by=$7,classified_at=NOW(),classification_basis=jsonb_build_object('id',$9::uuid),updated_at=NOW()
		WHERE id=$1 AND status=$8
	`, outflow.ID, command.Classification, command.MatchedBasisType, basisID, command.Reason, string(evidence), actorID, outflow.Status, id.New())
	if err != nil {
		return fmt.Errorf("classify external outflow: %w", outflowDecisionError(err))
	}
	if tag.RowsAffected() != 1 {
		return apperror.NewConflict("external outflow classification raced with another decision")
	}
	return nil
}

func (r *ExternalOutflowWorkflowRepo) CreateCustodyAdjustment(ctx context.Context, outflow *crypto.ExternalOutflow, makerID id.ID) (*crypto.CryptoCustodyAdjustment, error) {
	if outflow.TokenID == nil || outflow.SourceWalletID == nil {
		return nil, apperror.NewValidation("outflow wallet and token are required")
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var adjustment crypto.CryptoCustodyAdjustment
	err := q.QueryRow(ctx, `
		INSERT INTO doc_crypto_custody_adjustments (
			external_outflow_id,network_id,wallet_id,token_id,amount,maker_id
		) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (external_outflow_id) DO NOTHING
		RETURNING id,external_outflow_id,network_id,wallet_id,token_id,amount,status,maker_id,
		          decision_by,decision_reason,posted_version,created_at,decided_at
	`, outflow.ID, outflow.NetworkID, *outflow.SourceWalletID, *outflow.TokenID, outflow.Amount, makerID).Scan(
		&adjustment.ID, &adjustment.ExternalOutflowID, &adjustment.NetworkID, &adjustment.WalletID,
		&adjustment.TokenID, &adjustment.Amount, &adjustment.Status, &adjustment.MakerID,
		&adjustment.DecisionBy, &adjustment.DecisionReason, &adjustment.PostedVersion,
		&adjustment.CreatedAt, &adjustment.DecidedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.NewConflict("custody adjustment already exists")
	}
	if err != nil {
		return nil, err
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_external_outflow_actions(
			external_outflow_id,actor_type,actor_id,action,old_state,new_state,reason,evidence
		) VALUES($1,'user',$2::text,'custody_adjustment_created','classified','pending_approval',
		         'unauthorized custody loss requires accounting adjustment',
		         jsonb_build_object('adjustmentId',$3::text))
	`, outflow.ID, makerID, adjustment.ID); err != nil {
		return nil, err
	}
	return &adjustment, nil
}

func (r *ExternalOutflowWorkflowRepo) HasFreshSuccessfulReconciliation(ctx context.Context, outflow *crypto.ExternalOutflow) (bool, error) {
	if outflow.ClassifiedAt == nil || outflow.SourceWalletID == nil {
		return false, nil
	}
	requiredAfter := *outflow.ClassifiedAt
	if outflow.Classification == crypto.OutflowUnauthorizedLoss {
		if outflow.Adjustment == nil || outflow.Adjustment.Status != "posted" || outflow.Adjustment.DecidedAt == nil {
			return false, nil
		}
		if outflow.Adjustment.DecidedAt.After(requiredAfter) {
			requiredAfter = *outflow.Adjustment.DecidedAt
		}
	}
	var fresh bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT EXISTS(
		  SELECT 1 FROM reg_crypto_reconciliation_checks c
		  JOIN reg_crypto_reconciliation_runs run ON run.id=c.run_id
		  WHERE c.wallet_id=$1 AND c.token_id=$2
		    AND run.status='completed' AND run.completed_at>$3
		    AND c.delta_amount=0
		    AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_reconciliation_items i
		      WHERE i.wallet_id=c.wallet_id AND i.token_id=c.token_id
		        AND i.kind IN ('token_balance_mismatch','native_fee_balance_mismatch')
		        AND i.status IN ('open','acknowledged')
		    )
		)
	`, *outflow.SourceWalletID, outflow.TokenID, requiredAfter).Scan(&fresh)
	return fresh, err
}

func (r *ExternalOutflowWorkflowRepo) ResolveExternalOutflow(ctx context.Context, outflow *crypto.ExternalOutflow, actorID id.ID, reason string) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	tag, err := q.Exec(ctx, `
		UPDATE reg_crypto_external_outflows
		SET status='resolved',resolved_by=$2,resolved_at=NOW(),resolution=$3,resolution_basis=jsonb_build_object('id',$4::uuid),updated_at=NOW()
		WHERE id=$1 AND status='classified'
	`, outflow.ID, actorID, reason, id.New())
	if err != nil {
		return outflowDecisionError(err)
	}
	if tag.RowsAffected() != 1 {
		return apperror.NewConflict("external outflow resolution raced with another decision")
	}
	incidentTag, err := q.Exec(ctx, `
		UPDATE reg_crypto_reconciliation_items
		SET status='resolved',resolved_by=$1,resolved_at=NOW(),resolution_reason=$2,updated_at=NOW()
		WHERE network_id=$3 AND wallet_id=$4 AND token_id IS NOT DISTINCT FROM $5
		  AND kind='external_outflow' AND tx_hash=$6 AND event_kind=$7 AND event_index=$8
		  AND status IN ('open','acknowledged')
	`, actorID, reason, outflow.NetworkID, outflow.SourceWalletID, outflow.TokenID,
		outflow.TxHash, outflow.EventKind, outflow.EventIndex)
	if err != nil {
		return outflowDecisionError(err)
	}
	if incidentTag.RowsAffected() != 1 {
		return apperror.NewConflict("external outflow reconciliation incident is missing or already resolved")
	}
	return nil
}

func (r *ExternalOutflowWorkflowRepo) PrepareCustodyAdjustmentForPosting(ctx context.Context, adjustmentID, actorID id.ID) (*crypto.CryptoCustodyAdjustment, error) {
	if err := r.lockCustodyAdjustment(ctx, adjustmentID, true); err != nil {
		return nil, err
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var adjustment crypto.CryptoCustodyAdjustment
	var outflowStatus, classification, finalityStatus string
	var confirmations, requiredConfirmations int
	err := q.QueryRow(ctx, `
		SELECT adjustment.id,adjustment.external_outflow_id,adjustment.network_id,
		       adjustment.wallet_id,adjustment.token_id,adjustment.amount,adjustment.status,
		       adjustment.maker_id,adjustment.decision_by,adjustment.decision_reason,
		       adjustment.posted_version,adjustment.created_at,adjustment.decided_at,
		       outflow.status,outflow.classification,outflow.finality_status,
		       outflow.confirmations,outflow.required_confs
		FROM doc_crypto_custody_adjustments adjustment
		JOIN reg_crypto_external_outflows outflow ON outflow.id=adjustment.external_outflow_id
		WHERE adjustment.id=$1
		FOR UPDATE OF adjustment,outflow
	`, adjustmentID).Scan(
		&adjustment.ID, &adjustment.ExternalOutflowID, &adjustment.NetworkID, &adjustment.WalletID,
		&adjustment.TokenID, &adjustment.Amount, &adjustment.Status, &adjustment.MakerID,
		&adjustment.DecisionBy, &adjustment.DecisionReason, &adjustment.PostedVersion,
		&adjustment.CreatedAt, &adjustment.DecidedAt, &outflowStatus, &classification,
		&finalityStatus, &confirmations, &requiredConfirmations,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.NewNotFound("custody adjustment", adjustmentID.String())
	}
	if err != nil {
		return nil, err
	}
	if adjustment.Status != "pending_approval" {
		return nil, apperror.NewConflict("custody adjustment is not pending approval")
	}
	if adjustment.MakerID == actorID {
		return nil, apperror.NewForbidden("custody adjustment maker cannot approve it")
	}
	if outflowStatus != string(crypto.ExternalOutflowClassified) || classification != string(crypto.OutflowUnauthorizedLoss) {
		return nil, apperror.NewConflict("custody adjustment source is not a classified unauthorized loss")
	}
	if (finalityStatus != "confirmed" && finalityStatus != "finalized") ||
		(requiredConfirmations > 0 && confirmations < requiredConfirmations) {
		return nil, apperror.NewConflict("custody loss chain effect is no longer final")
	}
	if err := r.prepareCustodyLossRecognition(ctx, &adjustment, actorID); err != nil {
		return nil, err
	}
	return &adjustment, nil
}

func (r *ExternalOutflowWorkflowRepo) MarkCustodyAdjustmentPosted(ctx context.Context, adjustment *crypto.CryptoCustodyAdjustment, actorID id.ID, reason string) error {
	if adjustment == nil || adjustment.Status != "posted" || adjustment.PostedVersion != 1 {
		return apperror.NewValidation("custody adjustment posting state is invalid")
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var decidedAt time.Time
	err := q.QueryRow(ctx, `
		UPDATE doc_crypto_custody_adjustments
		SET status='posted',decision_by=$2,decision_reason=$3,posted_version=$4,decided_at=NOW(),decision_basis=jsonb_build_object('id',$5::uuid)
		WHERE id=$1 AND status='pending_approval'
		RETURNING decided_at
	`, adjustment.ID, actorID, reason, adjustment.PostedVersion, id.New()).Scan(&decidedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NewConflict("custody adjustment approval raced with another decision")
	}
	if err != nil {
		return outflowDecisionError(err)
	}
	if err := enqueueReconciliationRequests(ctx, reconciliationRequest{adjustment.NetworkID, adjustment.WalletID, adjustment.TokenID, "custody_adjustment", adjustment.ID}); err != nil {
		return fmt.Errorf("enqueue custody adjustment reconciliation: %w", err)
	}

	adjustment.DecisionBy = &actorID
	adjustment.DecisionReason = reason
	adjustment.DecidedAt = &decidedAt
	return nil
}

func (r *ExternalOutflowWorkflowRepo) RejectCustodyAdjustment(ctx context.Context, adjustmentID, actorID id.ID, reason string) (*crypto.CryptoCustodyAdjustment, error) {
	if err := r.lockCustodyAdjustment(ctx, adjustmentID, false); err != nil {
		return nil, err
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var adjustment crypto.CryptoCustodyAdjustment
	err := q.QueryRow(ctx, `
		SELECT id,external_outflow_id,network_id,wallet_id,token_id,amount,status,maker_id,
		       decision_by,decision_reason,posted_version,created_at,decided_at
		FROM doc_crypto_custody_adjustments WHERE id=$1 FOR UPDATE
	`, adjustmentID).Scan(
		&adjustment.ID, &adjustment.ExternalOutflowID, &adjustment.NetworkID, &adjustment.WalletID,
		&adjustment.TokenID, &adjustment.Amount, &adjustment.Status, &adjustment.MakerID,
		&adjustment.DecisionBy, &adjustment.DecisionReason, &adjustment.PostedVersion,
		&adjustment.CreatedAt, &adjustment.DecidedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.NewNotFound("custody adjustment", adjustmentID.String())
	}
	if err != nil {
		return nil, err
	}
	if adjustment.Status != "pending_approval" {
		return nil, apperror.NewConflict("custody adjustment is not pending approval")
	}
	if adjustment.MakerID == actorID {
		return nil, apperror.NewForbidden("custody adjustment maker cannot reject it")
	}
	var decidedAt time.Time
	err = q.QueryRow(ctx, `UPDATE doc_crypto_custody_adjustments SET status='rejected',decision_by=$2,decision_reason=$3,decided_at=NOW(),decision_basis=jsonb_build_object('id',$4::uuid)
 WHERE id=$1 AND status='pending_approval' RETURNING decided_at`, adjustment.ID, actorID, reason, id.New()).Scan(&decidedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.NewConflict("custody adjustment rejection raced with another decision")
	}
	if err != nil {
		return nil, outflowDecisionError(err)
	}
	adjustment.Status = "rejected"
	adjustment.DecisionBy = &actorID
	adjustment.DecisionReason = reason
	adjustment.DecidedAt = &decidedAt
	return &adjustment, nil
}

func (r *ExternalOutflowWorkflowRepo) loadOutflowDetails(ctx context.Context, q postgres.Querier, outflow *crypto.ExternalOutflow) error {
	rows, err := q.Query(ctx, `
		SELECT actor_type,actor_id,action,old_state,new_state,reason,evidence,created_at
		FROM reg_crypto_external_outflow_actions WHERE external_outflow_id=$1 ORDER BY created_at,id
	`, outflow.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var action crypto.ExternalOutflowAction
		var evidence []byte
		if err := rows.Scan(&action.ActorType, &action.ActorID, &action.Action, &action.OldState,
			&action.NewState, &action.Reason, &evidence, &action.CreatedAt); err != nil {
			return err
		}
		_ = json.Unmarshal(evidence, &action.Evidence)
		outflow.Actions = append(outflow.Actions, action)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := r.loadAdjustment(ctx, q, outflow); err != nil {
		return err
	}
	holdRows, err := q.Query(ctx, `
		SELECT 'network',reason,owner_type,owner_id,held_at FROM reg_crypto_network_holds
		WHERE network_id=$1 AND status='open'
		UNION ALL
		SELECT 'wallet',reason,owner_type,owner_id,held_at FROM reg_crypto_wallet_allocation_holds
		WHERE wallet_id=$2 AND status='open'
		ORDER BY held_at
	`, outflow.NetworkID, outflow.SourceWalletID)
	if err != nil {
		return err
	}
	defer holdRows.Close()
	for holdRows.Next() {
		var hold crypto.OperationalHold
		if err := holdRows.Scan(&hold.Scope, &hold.Reason, &hold.OwnerType, &hold.OwnerID, &hold.HeldAt); err != nil {
			return err
		}
		outflow.OpenHolds = append(outflow.OpenHolds, hold)
	}
	return holdRows.Err()
}

func (r *ExternalOutflowWorkflowRepo) loadAdjustment(ctx context.Context, q postgres.Querier, outflow *crypto.ExternalOutflow) error {
	var adjustment crypto.CryptoCustodyAdjustment
	err := q.QueryRow(ctx, `
		SELECT id,external_outflow_id,network_id,wallet_id,token_id,amount,status,maker_id,
		       decision_by,decision_reason,posted_version,created_at,decided_at
		FROM doc_crypto_custody_adjustments WHERE external_outflow_id=$1
	`, outflow.ID).Scan(
		&adjustment.ID, &adjustment.ExternalOutflowID, &adjustment.NetworkID, &adjustment.WalletID,
		&adjustment.TokenID, &adjustment.Amount, &adjustment.Status, &adjustment.MakerID,
		&adjustment.DecisionBy, &adjustment.DecisionReason, &adjustment.PostedVersion,
		&adjustment.CreatedAt, &adjustment.DecidedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	outflow.Adjustment = &adjustment
	return nil
}

type externalOutflowRowScanner interface{ Scan(...any) error }

func scanExternalOutflow(row externalOutflowRowScanner) (*crypto.ExternalOutflow, error) {
	var outflow crypto.ExternalOutflow
	var evidence []byte
	err := row.Scan(
		&outflow.ID, &outflow.NetworkID, &outflow.TokenID, &outflow.SourceWalletID,
		&outflow.TxHash, &outflow.EventKind, &outflow.EventIndex, &outflow.FromAddress,
		&outflow.ToAddress, &outflow.ContractAddress, &outflow.Amount, &outflow.BlockNumber,
		&outflow.BlockHash, &outflow.Confirmations, &outflow.RequiredConfirmations,
		&outflow.FinalityStatus, &outflow.Status, &outflow.Classification,
		&outflow.MatchedBasisType, &outflow.MatchedBasisID, &outflow.ClassificationReason,
		&evidence, &outflow.ClassifiedBy, &outflow.ClassifiedAt, &outflow.ResolvedBy,
		&outflow.ResolvedAt, &outflow.Resolution, &outflow.DetectedAt, &outflow.LastObservedAt,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(evidence, &outflow.ClassificationEvidence)
	return &outflow, nil
}

var _ cryptoapp.ExternalOutflowRepository = (*ExternalOutflowWorkflowRepo)(nil)
