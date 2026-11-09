package crypto_repo

import (
	"context"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"
)

// ReadWalletRiskDisplays is a bounded batch read for already-authorized wallet
// IDs. Asset risks remain separate; the list summary is the worst funded class.
func (r *AMLRepository) ReadWalletRiskDisplays(ctx context.Context, wallets []id.ID) (map[id.ID]aml.WalletDisplay, error) {
	result := make(map[id.ID]aml.WalletDisplay, len(wallets))
	if len(wallets) == 0 {
		return result, nil
	}
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `SELECT w.id,t.id,t.symbol,COALESCE(physical.amount,0)>0,
 COALESCE(r.inventory_covered,FALSE) AND fn_crypto_aml_total_inventory_covered(w.id,t.id),
 CASE WHEN r.risk_class='high' THEN 'high' WHEN r.scope_version=s.version AND r.inventory_covered AND r.expires_at>NOW() AND fn_crypto_aml_total_inventory_covered(w.id,t.id) THEN r.risk_class ELSE 'unknown' END,
 CASE WHEN COALESCE(physical.amount,0)=0 THEN 'empty_inventory' WHEN r.expires_at<=NOW() AND r.risk_class<>'high' THEN 'assessment_expired' ELSE COALESCE(r.reason,'assessment_pending') END,
 CASE WHEN o.wallet_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_effects e JOIN reg_crypto_aml_wallet_baselines baseline ON baseline.wallet_id=e.wallet_id AND baseline.token_id=e.token_id WHERE e.wallet_id=w.id AND e.token_id=t.id AND e.block_number>baseline.block_number) THEN 'testnet_bootstrap' ELSE 'inventory' END,
 r.evaluated_at,r.expires_at,b.id,COALESCE(b.risk_class,pool_policy.risk_class,''),CASE WHEN b.id IS NULL AND pool_policy.id IS NOT NULL THEN 'preparing' WHEN b.id IS NULL THEN '' WHEN b.execution_held THEN 'held' WHEN b.intake_closed_at IS NOT NULL THEN 'closed' ELSE 'open' END
 FROM cat_wallets w JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=w.network_id AND s.enabled
 JOIN cat_tokens t ON t.id=s.token_id LEFT JOIN reg_crypto_balance_balances physical ON physical.wallet_id=w.id AND physical.token_id=t.id
 LEFT JOIN reg_crypto_wallet_risk_states r ON r.wallet_id=w.id AND r.token_id=t.id
 LEFT JOIN reg_crypto_aml_testnet_openings o ON o.wallet_id=w.id AND o.token_id=t.id
 LEFT JOIN reg_crypto_aml_batches b ON b.collector_wallet_id=w.id AND b.token_id=t.id
 LEFT JOIN reg_crypto_aml_wallet_provisions provision ON provision.wallet_id=w.id AND provision.status='completed'
 LEFT JOIN reg_crypto_aml_pool_intents intent ON intent.id=provision.id
 LEFT JOIN sys_crypto_aml_pool_policies pool_policy ON pool_policy.id=intent.policy_id AND pool_policy.wallet_role='liquidity_hot' AND pool_policy.token_id=t.id
 WHERE w.id=ANY($1) ORDER BY w.id,t.id`, wallets)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var wallet id.ID
		var asset aml.WalletAssetDisplay
		if err := rows.Scan(&wallet, &asset.TokenID, &asset.Symbol, &asset.HasFunds, &asset.InventoryCovered, &asset.Assessment.Class, &asset.Assessment.Reason, &asset.Assessment.Source, &asset.Assessment.EvaluatedAt, &asset.Assessment.ExpiresAt, &asset.BatchID, &asset.IntakeClass, &asset.BatchState); err != nil {
			return nil, err
		}
		asset.Assessment.Enabled = true
		view := result[wallet]
		view.Enabled = true
		view.Assets = append(view.Assets, asset)
		if asset.HasFunds || asset.Assessment.Class == aml.High {
			if !view.Class.Valid() {
				view.Class = asset.Assessment.Class
			} else {
				view.Class = aml.PoolClass(view.Class, asset.Assessment.Class)
			}
			view.HasFunds = view.HasFunds || asset.HasFunds
			if asset.Assessment.ExpiresAt != nil && (view.ExpiresAt == nil || asset.Assessment.ExpiresAt.Before(*view.ExpiresAt)) {
				view.ExpiresAt = asset.Assessment.ExpiresAt
			}
		}
		result[wallet] = view
	}
	for wallet, view := range result {
		if !view.Class.Valid() {
			view.Class = aml.Unknown
		}
		result[wallet] = view
	}
	return result, rows.Err()
}

// ReadPaymentRiskDisplays evaluates this payment's exact effect and generation.
// It never substitutes the recipient wallet's current risk for a payment check.
func (r *AMLRepository) ReadPaymentRiskDisplays(ctx context.Context, payments []id.ID) (map[id.ID]aml.AssessmentDisplay, error) {
	result := make(map[id.ID]aml.AssessmentDisplay, len(payments))
	if len(payments) == 0 {
		return result, nil
	}
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `SELECT p.id,s.enabled IS TRUE,
 EXISTS(SELECT 1 FROM reg_crypto_aml_reports pending_report JOIN reg_crypto_aml_provider_checks pc ON pc.id=pending_report.provider_check_id JOIN reg_crypto_aml_jobs pj ON pj.id=pc.job_id WHERE pending_report.subject_id=subject.id AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_decisions assessed WHERE assessed.report_id=pending_report.id AND assessed.subject_id=subject.id AND assessed.generation=pj.generation AND assessed.policy_version=s.policy_version)),
 COALESCE(adverse.risk_class,current_decision.risk_class,''),COALESCE(adverse.reason,current_decision.reason,''),COALESCE(adverse.created_at,current_decision.created_at),COALESCE(adverse.expires_at,current_decision.expires_at),report.score_bp,COALESCE(report.adapter_version,''),COALESCE(job.status,''),e.id IS NOT NULL,
 h.created_at,h.expires_at,p.posted AND p.canonicality='canonical' AND p.status IN ('confirmed','settled'),cs.canonicality='canonical' AND cs.coverage_complete
 FROM doc_crypto_payments p LEFT JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.token_id=p.token_id AND s.enabled
 LEFT JOIN reg_crypto_aml_effects e ON e.wallet_id=p.wallet_id AND e.token_id=p.token_id AND e.network_id=p.network_id AND e.tx_hash=p.tx_hash AND e.event_kind=p.event_kind AND e.event_index=p.event_index AND e.block_hash=p.block_hash
 LEFT JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id
 LEFT JOIN reg_crypto_aml_effect_checks c ON c.effect_id=e.id LEFT JOIN reg_crypto_aml_subjects subject ON subject.id=c.subject_id
 LEFT JOIN LATERAL(SELECT d.* FROM reg_crypto_aml_decisions d WHERE d.subject_id=subject.id AND d.risk_class='high' ORDER BY d.created_at DESC,d.id DESC LIMIT 1)adverse ON TRUE
 LEFT JOIN LATERAL(SELECT d.* FROM reg_crypto_aml_decisions d WHERE d.subject_id=subject.id AND d.generation=subject.generation AND d.policy_version=s.policy_version AND subject.mapping_version=s.mapping_version ORDER BY d.created_at DESC,d.id DESC LIMIT 1)current_decision ON TRUE
 LEFT JOIN reg_crypto_aml_reports report ON report.id=COALESCE(adverse.report_id,current_decision.report_id)
 LEFT JOIN LATERAL(SELECT j.status FROM reg_crypto_aml_jobs j WHERE j.subject_id=subject.id AND j.generation=subject.generation ORDER BY j.created_at DESC,j.id DESC LIMIT 1)job ON TRUE
 LEFT JOIN reg_crypto_aml_testnet_openings o ON o.wallet_id=p.wallet_id AND o.token_id=p.token_id AND o.merchant_id=p.merchant_id
 LEFT JOIN reg_crypto_aml_testnet_bootstraps h ON h.id=o.bootstrap_id AND h.block_number>=p.block_number
 WHERE p.id=ANY($1) ORDER BY p.id`, payments)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	for rows.Next() {
		var payment id.ID
		var v aml.AssessmentDisplay
		var rawClass, reason, adapter, job string
		var effect, settled, pendingReport bool
		var coverage *bool
		var bootstrapAt, bootstrapExpiry *time.Time
		if err := rows.Scan(&payment, &v.Enabled, &pendingReport, &rawClass, &reason, &v.EvaluatedAt, &v.ExpiresAt, &v.ScoreBP, &adapter, &job, &effect, &bootstrapAt, &bootstrapExpiry, &settled, &coverage); err != nil {
			return nil, err
		}
		v.Class, v.Source, v.Reason = aml.Unknown, "none", "aml_disabled"
		if v.Enabled {
			v.Reason = "assessment_pending"
			if rawClass == "high" || (settled && rawClass != "") {

				v.Class, v.Source, v.Reason = aml.Class(rawClass), "provider", reason
				if adapter == aml.TestnetAdapter {
					v.Source = aml.TestnetProvider
				}
				if v.Class != aml.High && pendingReport {
					v.Class, v.Reason = aml.Unknown, "assessment_pending"
				}
				if v.Class != aml.High && (v.ExpiresAt == nil || !now.Before(*v.ExpiresAt)) {
					v.Class, v.Reason = aml.Unknown, "assessment_expired"
				}
				if v.Class != aml.High && (coverage == nil || !*coverage) {
					v.Class, v.Reason = aml.Unknown, "insufficient_evidence"
				}
			} else if !settled {
				v.Reason = "payment_unconfirmed"
			} else if !effect && bootstrapAt != nil {
				v.Source, v.Reason, v.EvaluatedAt, v.ExpiresAt = "testnet_bootstrap", "testnet_bootstrap", bootstrapAt, bootstrapExpiry
				if bootstrapExpiry != nil && now.Before(*bootstrapExpiry) {
					v.Class = aml.Low
				} else {
					v.Reason = "assessment_expired"
				}
			} else if job != "" {
				v.Reason = job
			}
		}
		result[payment] = v
	}
	return result, rows.Err()
}
