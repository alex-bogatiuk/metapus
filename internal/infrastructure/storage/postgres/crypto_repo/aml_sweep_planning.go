package crypto_repo

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"
)

func (r *AMLRepository) ClaimAMLBatchSweepCandidate(ctx context.Context) (aml.BatchSweepRequest, bool, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var request aml.BatchSweepRequest
	err := q.QueryRow(ctx, `SELECT w.custody_merchant_id,w.network_id,t.id,w.id,
    p.principal+p.service_fee-COALESCE(config.sweep_retain_amount,t.sweep_retain_amount,0),p.revision,risk.revision,scope.version,risk.risk_class
 FROM cat_wallets w JOIN cat_blockchain_networks n ON n.id=w.network_id AND n.is_active AND NOT n.deletion_mark
 JOIN sys_crypto_custody_policies custody ON custody.network_id=w.network_id AND custody.enforced
 JOIN sys_crypto_network_policies network ON network.network_id=w.network_id AND network.enabled
 JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=w.custody_merchant_id AND scope.network_id=w.network_id AND scope.enabled
 JOIN cat_tokens t ON t.id=scope.token_id AND t.is_active AND NOT t.deletion_mark AND t.token_standard<>'native'
 JOIN reg_crypto_aml_inventory_positions p ON p.wallet_id=w.id AND p.token_id=t.id
 JOIN reg_crypto_wallet_risk_states risk ON risk.wallet_id=w.id AND risk.token_id=t.id AND risk.scope_version=scope.version AND w.deposit_retired_at IS NULL
 LEFT JOIN reg_merchant_token_config config ON config.merchant_id=w.custody_merchant_id AND config.token_id=t.id
 WHERE w.status='sweep_pending' AND w.wallet_role='merchant_pool' AND w.tier='pool' AND w.chain_activation_status='active' AND w.is_active AND NOT w.deletion_mark
 AND w.address<>'' AND w.vault_key_id<>'' AND risk.risk_class IN ('low','medium') AND risk.inventory_covered AND risk.expires_at>NOW()
 AND p.principal+p.service_fee>COALESCE(config.sweep_retain_amount,t.sweep_retain_amount,0)
 AND fn_crypto_aml_total_inventory_covered(w.id,t.id)
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds h WHERE h.wallet_id=w.id AND h.status='open')
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_network_holds h WHERE h.network_id=w.network_id AND h.status='open')
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_items i WHERE i.wallet_id=w.id AND i.status IN ('open','acknowledged') AND i.kind IN ('token_balance_mismatch','native_fee_balance_mismatch'))
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_balance_reservations reserved WHERE reserved.wallet_id=w.id AND reserved.token_id=t.id AND reserved.status='active')
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_sweep_attempts a WHERE a.wallet_id=w.id AND a.status IN ('claimed','resource_ready','signing','broadcast','held'))
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations a WHERE a.source_wallet_id=w.id AND a.token_id=t.id AND a.status='reserved')
 AND EXISTS(SELECT 1 FROM reg_crypto_aml_batches b JOIN crypto_aml_batch_usage usage ON usage.id=b.id
     WHERE b.merchant_id=scope.merchant_id AND b.network_id=scope.network_id AND b.token_id=t.id AND b.risk_class=risk.risk_class AND b.scope_version=scope.version
     AND b.intake_closed_at IS NULL AND b.intake_closes_at>NOW() AND NOT b.execution_held
     AND usage.used+p.principal+p.service_fee-COALESCE(config.sweep_retain_amount,t.sweep_retain_amount,0)<=b.max_gross
     AND fn_crypto_aml_total_inventory_covered(b.collector_wallet_id,b.token_id))
 ORDER BY w.updated_at,w.id,t.id FOR UPDATE OF scope SKIP LOCKED LIMIT 1`).Scan(&request.MerchantID, &request.NetworkID, &request.TokenID, &request.SourceWalletID, &request.Amount, &request.SourceRevision, &request.RiskRevision, &request.ScopeVersion, &request.Class)
	if errors.Is(err, pgx.ErrNoRows) {
		return request, false, nil
	}
	if err != nil {
		return request, false, err
	}
	var source id.ID
	if err = q.QueryRow(ctx, `SELECT id FROM cat_wallets WHERE id=$1 AND status='sweep_pending' FOR UPDATE`, request.SourceWalletID).Scan(&source); err != nil {
		return request, false, err
	}

	return request, true, nil
}

func (r *AMLRepository) MarkAMLSourceSweeping(ctx context.Context, walletID id.ID) (bool, error) {
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE cat_wallets SET status='sweeping',updated_at=NOW(),version=version+1 WHERE id=$1 AND status='sweep_pending'`, walletID)
	return tag.RowsAffected() == 1, err
}
