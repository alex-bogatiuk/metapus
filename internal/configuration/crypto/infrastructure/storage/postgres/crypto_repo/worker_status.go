package crypto_repo

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

func (r *WorkerRuntimeRepo) PersistNetworkRuntimeStatus(ctx context.Context, status crypto.NetworkRuntimeStatus) error {
	rawEndpoints, err := json.Marshal(status.RPCEndpoints)
	if err != nil {
		return err
	}
	rawProviders, err := json.Marshal(status.ResourceProviders)
	if err != nil {
		return err
	}
	if len(status.ResourceProviders) == 0 {
		rawProviders = []byte("[]")
	}
	var rawIngestion *string
	if status.Ingestion != nil {
		raw, err := json.Marshal(status.Ingestion)
		if err != nil {
			return err
		}
		value := string(raw)
		rawIngestion = &value
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	_, err = q.Exec(ctx, `
		INSERT INTO sys_crypto_network_runtime_status (
			network_id,rpc_endpoints,signer_ready,signer_error,resource_provider_ready,
			resource_provider_state,resource_providers,observed_at,worker_id,ingestion
		) VALUES ($1,$2::jsonb,$3,$4,$5,$6,$7::jsonb,$8,$9,$10::jsonb)
		ON CONFLICT (network_id) DO UPDATE SET
			rpc_endpoints=EXCLUDED.rpc_endpoints,signer_ready=EXCLUDED.signer_ready,
			signer_error=EXCLUDED.signer_error,resource_provider_ready=EXCLUDED.resource_provider_ready,
			resource_provider_state=EXCLUDED.resource_provider_state,
			resource_providers=EXCLUDED.resource_providers,observed_at=EXCLUDED.observed_at,
			worker_id=EXCLUDED.worker_id,ingestion=EXCLUDED.ingestion,updated_at=NOW()
	`, status.NetworkID, string(rawEndpoints), status.SignerReady, status.SignerError,
		status.ResourceProviderReady, status.ResourceProviderState, string(rawProviders),
		status.ObservedAt, status.WorkerID, rawIngestion)
	return err
}

func (r *WorkerRuntimeRepo) ListFinalAMLInternalTransfers(ctx context.Context) ([]crypto.BlockchainEvent, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, finalAMLInternalProofSQL)
	if err != nil {
		return nil, err
	}
	var events []crypto.BlockchainEvent
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var event crypto.BlockchainEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			rows.Close()
			return nil, err
		}
		events = append(events, event)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return events, nil
}

const finalAMLInternalProofSQL = `SELECT DISTINCT ON (o.network_id,o.tx_hash,o.event_kind,o.event_index) o.payload
 FROM reg_crypto_chain_observations o
 JOIN reg_crypto_expected_chain_effects expected ON expected.network_id=o.network_id AND expected.tx_hash=o.tx_hash AND expected.event_kind=o.event_kind
 JOIN reg_crypto_signing_authorizations auth ON auth.id=expected.signing_authorization_id AND auth.signed_envelope IS NOT NULL
 JOIN reg_crypto_chain_settlements settled ON settled.network_id=o.network_id AND settled.tx_hash=o.tx_hash AND settled.status='succeeded' AND settled.block_hash=o.block_hash AND settled.block_height=o.block_number
 JOIN cat_wallets source ON source.network_id=expected.network_id AND source.address_key=expected.source_address_key AND source.custody_merchant_id=auth.merchant_id
 JOIN cat_wallets dest ON dest.network_id=expected.network_id AND dest.address_key=expected.destination_address_key AND dest.custody_merchant_id=auth.merchant_id
 JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=auth.merchant_id AND scope.network_id=o.network_id AND scope.token_id=expected.token_id AND scope.enabled
 WHERE o.payload->>'direction'='internal' AND auth.purpose IN ('sweep','native_funding')
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_effects effect JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=effect.id AND cs.canonicality='canonical' WHERE effect.network_id=o.network_id AND effect.tx_hash=o.tx_hash AND effect.event_kind=o.event_kind AND effect.event_index=o.event_index AND effect.wallet_id=dest.id AND effect.block_hash=o.block_hash)
 ORDER BY o.network_id,o.tx_hash,o.event_kind,o.event_index,o.received_at DESC LIMIT 10`

func (r *WorkerRuntimeRepo) WithdrawalRecoveryQueue(ctx context.Context) (int64, int64, error) {
	var count, age int64
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT COUNT(*),
		COALESCE(MAX(EXTRACT(EPOCH FROM NOW()-wr.recovery_started_at))::bigint,0)
		FROM doc_withdrawal_requests wr WHERE wr.recovery_reason<>'' AND wr.status IN ('approved','signing','held')`).Scan(&count, &age)
	return count, age, err
}
