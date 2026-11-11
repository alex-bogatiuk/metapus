package crypto_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

// ObservedExternalOutflowRepo persists one exact observed effect identity.
type ObservedExternalOutflowRepo struct{}

func NewObservedExternalOutflowRepo() *ObservedExternalOutflowRepo {
	return &ObservedExternalOutflowRepo{}
}

func (r *ObservedExternalOutflowRepo) UpsertExternalOutflow(ctx context.Context, event crypto.BlockchainEvent) (cryptoapp.DetectedExternalOutflow, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	eventKind := event.EffectiveEventKind()
	finalityStatus := externalOutflowFinality(event)

	var chainFamily string
	if err := q.QueryRow(ctx, `SELECT chain_family FROM cat_blockchain_networks WHERE id=$1`, event.NetworkID).Scan(&chainFamily); err != nil {
		return cryptoapp.DetectedExternalOutflow{}, fmt.Errorf("resolve outflow chain family: %w", err)
	}
	sourceKey, destinationKey, contractKey := event.FromAddress, event.ToAddress, event.TokenContract
	if chainFamily == "evm" {
		sourceKey, destinationKey, contractKey = lowerAddressKey(sourceKey), lowerAddressKey(destinationKey), lowerAddressKey(contractKey)
	}

	if err := lockExternalOutflowIdentity(ctx, event.NetworkID, event.TxHash, nil); err != nil {
		return cryptoapp.DetectedExternalOutflow{}, err
	}
	var tokenID *id.ID
	var walletID id.ID
	var exact bool
	// Existing effect ownership is independent of today's catalog selection.
	// In particular, retiring an asset must not redirect an old observation.
	err := q.QueryRow(ctx, `SELECT source_wallet_id,token_id,
 source_address_key=$5 AND destination_address_key=$6 AND contract_address_key=$7 AND amount=$8 FROM reg_crypto_external_outflows
 WHERE network_id=$1 AND tx_hash=$2 AND event_kind=$3 AND event_index=$4`, event.NetworkID, event.TxHash, eventKind, event.EventIndex,
		sourceKey, destinationKey, contractKey, event.Amount).Scan(&walletID, &tokenID, &exact)
	if err == nil && !exact {
		return cryptoapp.DetectedExternalOutflow{}, apperror.NewConflict("observed chain identity was reused with a different effect payload")
	}
	if errors.Is(err, pgx.ErrNoRows) {
		var resolvedTokenID id.ID
		err = q.QueryRow(ctx, `SELECT id FROM cat_tokens WHERE network_id=$1 AND deletion_mark=FALSE
 AND CASE WHEN $3='evm' THEN lower(COALESCE(contract_address,''))=lower($2) ELSE COALESCE(contract_address,'')=$2 END
 ORDER BY id LIMIT 1`, event.NetworkID, event.TokenContract, chainFamily).Scan(&resolvedTokenID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return cryptoapp.DetectedExternalOutflow{}, fmt.Errorf("resolve outflow token: %w", err)
		}
		if err == nil {
			tokenID = &resolvedTokenID
		}
		err = q.QueryRow(ctx, `SELECT id FROM cat_wallets WHERE network_id=$1 AND deletion_mark=FALSE
 AND address_key=$2 ORDER BY id LIMIT 1`, event.NetworkID, sourceKey).Scan(&walletID)
		if errors.Is(err, pgx.ErrNoRows) {
			return cryptoapp.DetectedExternalOutflow{}, apperror.NewConflict("outgoing event source is not a registered managed wallet")
		}
	}
	if err != nil {
		return cryptoapp.DetectedExternalOutflow{}, fmt.Errorf("resolve outflow ownership: %w", err)
	}
	if err := lockExternalOutflowIdentity(ctx, event.NetworkID, event.TxHash, &walletID); err != nil {
		return cryptoapp.DetectedExternalOutflow{}, err
	}
	var detected cryptoapp.DetectedExternalOutflow
	detected.NetworkID, detected.WalletID, detected.TokenID = event.NetworkID, walletID, tokenID
	err = q.QueryRow(ctx, `
		WITH inserted AS (
		  INSERT INTO reg_crypto_external_outflows (
			network_id,token_id,source_wallet_id,tx_hash,event_kind,event_index,
			from_address,to_address,source_address_key,destination_address_key,
			contract_address,contract_address_key,amount,block_number,block_hash,
			confirmations,required_confs,finality_status
		  ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		  ON CONFLICT (network_id,tx_hash,event_kind,event_index) DO NOTHING
		  RETURNING id,status
		)
		SELECT id,status,TRUE FROM inserted
		UNION ALL
		SELECT id,status,FALSE FROM reg_crypto_external_outflows
		WHERE network_id=$1 AND tx_hash=$4 AND event_kind=$5 AND event_index=$6
		  AND token_id IS NOT DISTINCT FROM $2 AND source_wallet_id=$3
		  AND source_address_key=$9 AND destination_address_key=$10
		  AND contract_address_key=$12 AND amount=$13
		LIMIT 1
	`, event.NetworkID, tokenID, walletID, event.TxHash, eventKind, event.EventIndex,
		event.FromAddress, event.ToAddress, sourceKey, destinationKey, event.TokenContract,
		contractKey, event.Amount, event.BlockNumber, event.BlockHash, event.Confirmations,
		event.RequiredConfs, finalityStatus).Scan(&detected.OutflowID, &detected.Status, &detected.Inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return cryptoapp.DetectedExternalOutflow{}, apperror.NewConflict("observed chain identity was reused with a different effect payload")
	}
	if err != nil {
		return cryptoapp.DetectedExternalOutflow{}, fmt.Errorf("record external outflow: %w", err)
	}
	if detected.Inserted {
		return detected, nil
	}

	var current outflowProjection
	if err := q.QueryRow(ctx, `SELECT block_number,block_hash,confirmations,required_confs,finality_status
 FROM reg_crypto_external_outflows WHERE id=$1 FOR UPDATE`, detected.OutflowID).Scan(&current.Height, &current.Hash, &current.Confirmations, &current.Required, &current.Finality); err != nil {
		return cryptoapp.DetectedExternalOutflow{}, err
	}
	next, err := nextOutflowProjection(current, event)
	if err != nil {
		return cryptoapp.DetectedExternalOutflow{}, err
	}
	if _, err := q.Exec(ctx, `UPDATE reg_crypto_external_outflows SET block_number=$2,block_hash=$3,confirmations=$4,
 required_confs=$5,finality_status=$6,last_observed_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`,
		detected.OutflowID, next.Height, next.Hash, next.Confirmations, next.Required, next.Finality); err != nil {
		return cryptoapp.DetectedExternalOutflow{}, fmt.Errorf("update exact external outflow observation: %w", err)
	}
	return detected, nil
}

type ExternalOutflowReconciliationRunRepo struct{}

func NewExternalOutflowReconciliationRunRepo() *ExternalOutflowReconciliationRunRepo {
	return &ExternalOutflowReconciliationRunRepo{}
}

func (r *ExternalOutflowReconciliationRunRepo) CreateExternalOutflowRun(ctx context.Context, detected cryptoapp.DetectedExternalOutflow) (id.ID, error) {
	var runID id.ID
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		INSERT INTO reg_crypto_reconciliation_runs (
			network_id,status,worker_id,started_at,heartbeat_at,completed_at,wallets_checked,items_created
		) VALUES ($1,'completed','chain-watcher',NOW(),NOW(),NOW(),1,CASE WHEN $2 THEN 1 ELSE 0 END)
		RETURNING id
	`, detected.NetworkID, detected.Inserted).Scan(&runID)
	if err != nil {
		return id.Nil(), fmt.Errorf("create external outflow reconciliation run: %w", err)
	}
	return runID, nil
}

type ExternalOutflowIncidentRepo struct{}

func NewExternalOutflowIncidentRepo() *ExternalOutflowIncidentRepo {
	return &ExternalOutflowIncidentRepo{}
}

func (r *ExternalOutflowIncidentRepo) UpsertExternalOutflowIncident(ctx context.Context, runID id.ID, detected cryptoapp.DetectedExternalOutflow, event crypto.BlockchainEvent) (id.ID, error) {
	if err := validateExternalOutflowSource(ctx, runID, detected, event); err != nil {
		return id.Nil(), err
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	delta := types.ZeroCryptoAmount().Difference(event.Amount)
	eventKind := event.EffectiveEventKind()
	var incidentID id.ID
	if detected.Status == crypto.ExternalOutflowResolved {
		err := q.QueryRow(ctx, `
			UPDATE reg_crypto_reconciliation_items incident
			SET last_run_id=$1,last_seen_at=NOW(),occurrence_count=occurrence_count+1,
			    chain_amount=$2,delta_amount=$3,updated_at=NOW()
			WHERE incident.id=(
			  SELECT id FROM reg_crypto_reconciliation_items
			  WHERE network_id=$4 AND wallet_id=$5 AND token_id IS NOT DISTINCT FROM $6
			    AND kind='external_outflow' AND tx_hash=$7 AND event_kind=$8 AND event_index=$9
			  ORDER BY created_at DESC LIMIT 1 FOR UPDATE
			)
			RETURNING incident.id
		`, runID, event.Amount, delta, detected.NetworkID, detected.WalletID,
			detected.TokenID, event.TxHash, eventKind, event.EventIndex).Scan(&incidentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return id.Nil(), apperror.NewConflict("resolved external outflow has no reconciliation incident history")
		}
		if err != nil {
			return id.Nil(), fmt.Errorf("append resolved external outflow incident occurrence: %w", err)
		}
		return incidentID, nil
	}

	err := q.QueryRow(ctx, `
		INSERT INTO reg_crypto_reconciliation_items (
			run_id,last_run_id,network_id,wallet_id,token_id,kind,
			ledger_amount,chain_amount,delta_amount,tx_hash,event_kind,event_index,status,
			first_seen_at,last_seen_at,occurrence_count,created_at,updated_at
		) VALUES ($1,$1,$2,$3,$4,'external_outflow',0,$5,$6,$7,$8,$9,'open',NOW(),NOW(),1,NOW(),NOW())
		ON CONFLICT (
			network_id,wallet_id,COALESCE(token_id,'00000000-0000-0000-0000-000000000000'::uuid),
			tx_hash,event_kind,event_index
		) WHERE status IN ('open','acknowledged') AND kind='external_outflow'
		DO UPDATE SET last_run_id=EXCLUDED.last_run_id,last_seen_at=NOW(),
		              occurrence_count=reg_crypto_reconciliation_items.occurrence_count+1,
		              chain_amount=EXCLUDED.chain_amount,delta_amount=EXCLUDED.delta_amount,updated_at=NOW()
		RETURNING id
	`, runID, detected.NetworkID, detected.WalletID, detected.TokenID, event.Amount, delta,
		event.TxHash, eventKind, event.EventIndex).Scan(&incidentID)
	if err != nil {
		return id.Nil(), fmt.Errorf("upsert external outflow reconciliation incident: %w", err)
	}
	return incidentID, nil
}

type ExternalOutflowObservationRepo struct{}

func NewExternalOutflowObservationRepo() *ExternalOutflowObservationRepo {
	return &ExternalOutflowObservationRepo{}
}

func (r *ExternalOutflowObservationRepo) AppendExternalOutflowObservations(ctx context.Context, runID, incidentID id.ID, detected cryptoapp.DetectedExternalOutflow, event crypto.BlockchainEvent) error {
	if err := validateExternalOutflowSource(ctx, runID, detected, event); err != nil {
		return err
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	delta := types.ZeroCryptoAmount().Difference(event.Amount)
	eventKind := event.EffectiveEventKind()
	if _, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_reconciliation_observations (
			run_id,incident_id,network_id,wallet_id,token_id,kind,
			ledger_amount,chain_amount,delta_amount,tx_hash,event_kind,event_index,block_hash,observed_at
		) VALUES ($1,$2,$3,$4,$5,'external_outflow',0,$6,$7,$8,$9,$10,$11,NOW())
	`, runID, incidentID, detected.NetworkID, detected.WalletID, detected.TokenID, event.Amount,
		delta, event.TxHash, eventKind, event.EventIndex, event.BlockHash); err != nil {
		return fmt.Errorf("record external outflow reconciliation observation: %w", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_external_outflow_observations (
			external_outflow_id,run_id,network_id,tx_hash,event_kind,event_index,incident_id,
			block_number,block_hash,confirmations,required_confs,finality_status,observation_source,observed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$12,$7,$8,$9,$10,$11,'chain-watcher',NOW())
	`, detected.OutflowID, runID, detected.NetworkID, event.TxHash, eventKind, event.EventIndex,
		event.BlockNumber, event.BlockHash, event.Confirmations, event.RequiredConfs,
		externalOutflowFinality(event), incidentID); err != nil {
		return fmt.Errorf("record immutable external outflow observation: %w", err)
	}
	return nil
}

type ExternalOutflowActionRepo struct{}

func NewExternalOutflowActionRepo() *ExternalOutflowActionRepo { return &ExternalOutflowActionRepo{} }

func (r *ExternalOutflowActionRepo) AppendExternalOutflowDetected(ctx context.Context, detected cryptoapp.DetectedExternalOutflow, incidentID id.ID, event crypto.BlockchainEvent) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		INSERT INTO reg_crypto_external_outflow_actions (
			external_outflow_id,actor_type,actor_id,action,old_state,new_state,reason,evidence
		) VALUES ($1,'worker','chain-watcher','detected','','pending_review','unknown managed-wallet outflow',
		          jsonb_build_object('incidentId',$2::text,'txHash',$3::text,'eventKind',$4::text,
		                             'eventIndex',$5::bigint,'blockHash',$6::text))
	`, detected.OutflowID, incidentID, event.TxHash, event.EffectiveEventKind(), event.EventIndex, event.BlockHash)
	if err != nil {
		return fmt.Errorf("record external outflow detection action: %w", err)
	}
	return nil
}

func lowerAddressKey(value string) string {
	result := make([]byte, len(value))
	for index := range value {
		char := value[index]
		if char >= 'A' && char <= 'Z' {
			char += 'a' - 'A'
		}
		result[index] = char
	}
	return string(result)
}

func externalOutflowFinality(event crypto.BlockchainEvent) string {
	if event.Canonicality == crypto.CanonicalityOrphaned {
		return "reorged"
	}
	switch event.Finality {
	case crypto.FinalityFinalized:
		return "finalized"
	case crypto.FinalitySolidified:
		return "confirmed"
	default:
		return "observed"
	}
}

var (
	_ cryptoapp.ExpectedChainEffectMatcher                 = (*ExpectedTransferRepo)(nil)
	_ cryptoapp.ObservedExternalOutflowRepository          = (*ObservedExternalOutflowRepo)(nil)
	_ cryptoapp.ExternalOutflowReconciliationRunRepository = (*ExternalOutflowReconciliationRunRepo)(nil)
	_ cryptoapp.ExternalOutflowIncidentRepository          = (*ExternalOutflowIncidentRepo)(nil)
	_ cryptoapp.ExternalOutflowObservationRepository       = (*ExternalOutflowObservationRepo)(nil)
	_ cryptoapp.ExternalOutflowActionRepository            = (*ExternalOutflowActionRepo)(nil)
)
