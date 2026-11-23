package crypto_repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/aml"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

type amlInventoryCoverage struct {
	WalletID       id.ID
	Baseline       *id.ID
	BaselineHeight *int64
	Ledger         string
	Net            string
	Covered        bool
	KnownHigh      bool
}
type amlInventoryEvidence struct {
	EffectID     id.ID
	DecisionID   *id.ID
	Class        aml.Class
	ExpiresAt    *time.Time
	Canonicality string
}

// RebuildNextAMLRiskProjection never updates a money register. Coverage is
// checked by receipt identity, not by assigning an equal checked amount to an
// unrelated receipt. Same-owner ancestors are evaluated together.
func (r *AMLRepository) RebuildNextAMLRiskProjection(ctx context.Context, now time.Time) (bool, error) {
	processed := false
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var scope amlScope
		var walletID id.ID
		err := q.QueryRow(ctx, `SELECT pending.wallet_id,s.merchant_id,s.network_id,s.token_id,s.version,s.policy_version,s.mapping_version
 FROM reg_crypto_aml_projection_queue pending JOIN sys_crypto_aml_routing_scopes s
 ON s.merchant_id=pending.merchant_id AND s.network_id=pending.network_id AND s.token_id=pending.token_id AND s.enabled
 WHERE pending.next_attempt_at<=$1 ORDER BY pending.next_attempt_at,pending.wallet_id,pending.token_id
 FOR UPDATE OF s SKIP LOCKED LIMIT 1`, now).Scan(&walletID, &scope.MerchantID, &scope.NetworkID, &scope.TokenID, &scope.Version, &scope.PolicyVersion, &scope.MappingVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		rows, err := q.Query(ctx, `WITH RECURSIVE ancestors(wallet_id) AS (
 SELECT $1::uuid UNION SELECT e.source_wallet_id FROM crypto_aml_risk_edges e JOIN ancestors a ON a.wallet_id=e.destination_wallet_id
 WHERE e.merchant_id=$2 AND e.network_id=$3 AND e.token_id=$4
 ) SELECT wallet_id FROM ancestors ORDER BY wallet_id`, walletID, scope.MerchantID, scope.NetworkID, scope.TokenID)
		if err != nil {
			return err
		}
		var ancestors []id.ID
		for rows.Next() {
			var value id.ID
			if err := rows.Scan(&value); err != nil {
				rows.Close()
				return err
			}
			ancestors = append(ancestors, value)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		coverage, err := r.loadAMLInventoryCoverage(ctx, ancestors, scope.TokenID)
		if err != nil {
			return err
		}
		covered := len(coverage) == len(ancestors)
		knownHigh := false
		for _, entry := range coverage {
			covered = covered && entry.Covered
			knownHigh = knownHigh || entry.KnownHigh
		}
		rows, err = q.Query(ctx, `SELECT e.id,d.id,
 CASE WHEN EXISTS(SELECT 1 FROM reg_crypto_aml_effect_checks oldc JOIN reg_crypto_aml_decisions oldd ON oldd.subject_id=oldc.subject_id WHERE oldc.effect_id=e.id AND oldd.risk_class='high') THEN 'high'
      WHEN cs.canonicality<>'canonical' OR NOT cs.coverage_complete THEN 'unknown'
      WHEN EXISTS(SELECT 1 FROM reg_crypto_aml_reports pending_report JOIN reg_crypto_aml_provider_checks pc ON pc.id=pending_report.provider_check_id
          JOIN reg_crypto_aml_jobs pj ON pj.id=pc.job_id WHERE pending_report.subject_id=s.id AND NOT EXISTS(
              SELECT 1 FROM reg_crypto_aml_decisions assessed WHERE assessed.report_id=pending_report.id AND assessed.subject_id=s.id
                  AND assessed.generation=pj.generation AND assessed.policy_version=$3)) THEN 'unknown'
      ELSE COALESCE(d.risk_class,'unknown') END,
 d.expires_at,cs.canonicality
 FROM reg_crypto_aml_effects e JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id
 LEFT JOIN reg_crypto_aml_wallet_baselines b ON b.wallet_id=e.wallet_id AND b.token_id=e.token_id
 LEFT JOIN reg_crypto_aml_effect_checks c ON c.effect_id=e.id LEFT JOIN reg_crypto_aml_subjects s ON s.id=c.subject_id
 LEFT JOIN LATERAL(SELECT candidate.id,candidate.risk_class,candidate.expires_at FROM reg_crypto_aml_decisions candidate
   WHERE candidate.subject_id=s.id AND candidate.generation=s.generation AND candidate.policy_version=$3 AND s.mapping_version=$4
   ORDER BY candidate.created_at DESC,candidate.id DESC LIMIT 1) d ON TRUE
 WHERE e.wallet_id=ANY($1) AND e.token_id=$2 AND
 (e.source_wallet_id IS NULL OR cs.canonicality<>'canonical' OR NOT cs.coverage_complete)
 AND (b.wallet_id IS NULL OR e.block_number>b.block_number OR EXISTS(SELECT 1 FROM reg_crypto_aml_effect_checks hc JOIN reg_crypto_aml_decisions hd ON hd.subject_id=hc.subject_id WHERE hc.effect_id=e.id AND hd.risk_class='high'))
 ORDER BY e.wallet_id,e.id`, ancestors, scope.TokenID, scope.PolicyVersion, scope.MappingVersion)
		if err != nil {
			return err
		}
		var evidence []amlInventoryEvidence
		var contributions []aml.Contribution
		for rows.Next() {
			var entry amlInventoryEvidence
			if err := rows.Scan(&entry.EffectID, &entry.DecisionID, &entry.Class, &entry.ExpiresAt, &entry.Canonicality); err != nil {
				rows.Close()
				return err
			}
			evidence = append(evidence, entry)
			contributions = append(contributions, aml.Contribution{Class: entry.Class, ExpiresAt: entry.ExpiresAt})
			knownHigh = knownHigh || entry.Class == aml.High
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		type openingEvidence struct {
			BootstrapID id.ID
			WalletID    id.ID
			ExpiresAt   time.Time
		}
		var openings []openingEvidence
		rows, err = q.Query(ctx, `SELECT h.id,o.wallet_id,h.expires_at FROM reg_crypto_aml_testnet_openings o JOIN reg_crypto_aml_testnet_bootstraps h ON h.id=o.bootstrap_id WHERE o.wallet_id=ANY($1) AND o.token_id=$2 AND o.amount>0 ORDER BY o.wallet_id`, ancestors, scope.TokenID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item openingEvidence
			if err := rows.Scan(&item.BootstrapID, &item.WalletID, &item.ExpiresAt); err != nil {
				rows.Close()
				return err
			}
			openings = append(openings, item)
			expiry := item.ExpiresAt
			contributions = append(contributions, aml.Contribution{Class: aml.Low, ExpiresAt: &expiry})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		projection := aml.ProjectInventory(contributions, covered, knownHigh, now)
		snapshot, err := json.MarshalDeterministic(struct {
			Scope      amlScope
			Coverage   []amlInventoryCoverage
			Evidence   []amlInventoryEvidence
			Openings   []openingEvidence
			Projection aml.Projection
		}{scope, coverage, evidence, openings, projection})
		if err != nil {
			return err
		}
		digest := sha256.Sum256(snapshot)
		hash := hex.EncodeToString(digest[:])
		var previous aml.Class
		err = q.QueryRow(ctx, `SELECT risk_class FROM reg_crypto_wallet_risk_states WHERE wallet_id=$1 AND token_id=$2`, walletID, scope.TokenID).Scan(&previous)
		if errors.Is(err, pgx.ErrNoRows) {
			previous = aml.Unknown
		} else if err != nil {
			return err
		}
		var revision int64
		err = q.QueryRow(ctx, `INSERT INTO reg_crypto_wallet_risk_states(wallet_id,token_id,merchant_id,network_id,risk_class,scope_version,inventory_covered,evidence_hash,reason,evaluated_at,expires_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
 ON CONFLICT(wallet_id,token_id) DO UPDATE SET risk_class=EXCLUDED.risk_class,scope_version=EXCLUDED.scope_version,inventory_covered=EXCLUDED.inventory_covered,
 evidence_hash=EXCLUDED.evidence_hash,reason=EXCLUDED.reason,evaluated_at=EXCLUDED.evaluated_at,expires_at=EXCLUDED.expires_at,revision=reg_crypto_wallet_risk_states.revision+1
 WHERE (reg_crypto_wallet_risk_states.risk_class,reg_crypto_wallet_risk_states.scope_version,reg_crypto_wallet_risk_states.inventory_covered,reg_crypto_wallet_risk_states.evidence_hash,reg_crypto_wallet_risk_states.reason,reg_crypto_wallet_risk_states.expires_at)
 IS DISTINCT FROM (EXCLUDED.risk_class,EXCLUDED.scope_version,EXCLUDED.inventory_covered,EXCLUDED.evidence_hash,EXCLUDED.reason,EXCLUDED.expires_at)
 RETURNING revision`, walletID, scope.TokenID, scope.MerchantID, scope.NetworkID, projection.Class, scope.Version, projection.Covered, hash, projection.Reason, now, projection.ExpiresAt).Scan(&revision)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_aml_risk_changes(wallet_id,token_id,merchant_id,network_id,previous_class,risk_class,revision,scope_version,reason,expires_at,inventory_covered)
  VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, walletID, scope.TokenID, scope.MerchantID, scope.NetworkID, previous, projection.Class, revision, scope.Version, projection.Reason, projection.ExpiresAt, projection.Covered); err != nil {
				return err
			}
		}
		if _, err := q.Exec(ctx, `DELETE FROM reg_crypto_aml_projection_queue WHERE wallet_id=$1 AND token_id=$2`, walletID, scope.TokenID); err != nil {
			return err
		}
		if projection.ExpiresAt != nil && now.Before(*projection.ExpiresAt) {
			if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_aml_projection_queue(wallet_id,token_id,merchant_id,network_id,next_attempt_at) VALUES($1,$2,$3,$4,$5)`, walletID, scope.TokenID, scope.MerchantID, scope.NetworkID, *projection.ExpiresAt); err != nil {
				return err
			}
		}
		processed = true
		return nil
	})
	return processed, err
}

func (r *AMLRepository) loadAMLInventoryCoverage(ctx context.Context, wallets []id.ID, tokenID id.ID) ([]amlInventoryCoverage, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `SELECT w.id,b.wallet_id,b.block_number,COALESCE(balance.amount,0)::text,
 (COALESCE(movements.net,0)+COALESCE(opening.amount,0))::text,
 b.wallet_id IS NOT NULL AND fn_crypto_aml_testnet_opening_valid(w.id,$2) AND COALESCE(balance.amount,0)=COALESCE(movements.net,0)+COALESCE(opening.amount,0) AND NOT EXISTS(
 SELECT 1 FROM reg_crypto_balance_movements m WHERE m.wallet_id=w.id AND m.token_id=$2 AND m.record_type='receipt' AND m.created_at>=b.created_at AND NOT(
 (m.recorder_type='CryptoCustodyReceipt' AND EXISTS(SELECT 1 FROM doc_crypto_custody_receipts receipt JOIN doc_crypto_payments p ON p.receipt_id=receipt.id
  JOIN reg_crypto_aml_effects e ON e.wallet_id=p.wallet_id AND e.token_id=p.token_id AND e.network_id=p.network_id
  AND e.tx_hash=p.tx_hash AND e.event_kind=p.event_kind AND e.event_index=p.event_index AND e.block_hash=p.block_hash AND e.amount=p.amount AND e.block_number>b.block_number
  JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
  WHERE receipt.id=m.recorder_id AND receipt.custody_line_id=m.line_id AND EXISTS(SELECT 1 FROM reg_crypto_transfer_verifications proof WHERE proof.id=p.verification_id AND proof.network_id=e.network_id AND proof.tx_hash=e.tx_hash AND proof.event_kind=e.event_kind AND proof.event_index=e.event_index AND proof.block_hash=e.block_hash AND proof.economics_hash=e.economics_hash) AND p.posted AND p.wallet_id=w.id AND p.token_id=$2 AND p.amount=m.amount))

 OR (m.recorder_type='CryptoSweep' AND EXISTS(SELECT 1 FROM crypto_aml_allocatable_transfers transfer
  JOIN reg_crypto_aml_effects e ON e.id=transfer.effect_id
  WHERE transfer.destination_movement_id=m.line_id AND transfer.destination_wallet_id=w.id AND transfer.token_id=$2 AND e.block_number>b.block_number))
 OR (m.recorder_type='CryptoNativeFunding' AND EXISTS(SELECT 1 FROM doc_crypto_native_fundings f JOIN reg_crypto_aml_effects e ON e.wallet_id=f.destination_wallet_id AND e.source_wallet_id=f.source_wallet_id AND e.token_id=f.native_token_id AND e.tx_hash=f.tx_hash AND e.amount=f.amount_minor AND e.block_number>b.block_number
  JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
  WHERE f.id=m.recorder_id AND f.status='settled' AND m.recorder_version=1 AND f.destination_wallet_id=w.id AND f.native_token_id=$2 AND f.amount_minor=m.amount))
 OR (m.recorder_type='AMLOperatingReceipt' AND EXISTS(SELECT 1 FROM reg_crypto_aml_operating_receipts receipt
  JOIN reg_crypto_aml_effects e ON e.id=receipt.effect_id AND e.wallet_id=w.id AND e.token_id=$2 AND e.amount=receipt.amount AND e.block_number>b.block_number
  JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
  WHERE receipt.id=m.recorder_id AND receipt.movement_id=m.line_id AND m.recorder_version=1 AND receipt.wallet_id=w.id AND receipt.token_id=$2 AND receipt.amount=m.amount))
 )),COALESCE(state.risk_class='high',FALSE)
 FROM cat_wallets w LEFT JOIN reg_crypto_aml_wallet_baselines b ON b.wallet_id=w.id AND b.token_id=$2
 LEFT JOIN reg_crypto_aml_testnet_openings opening ON opening.wallet_id=w.id AND opening.token_id=$2
 LEFT JOIN reg_crypto_balance_balances balance ON balance.wallet_id=w.id AND balance.token_id=$2
 LEFT JOIN reg_crypto_wallet_risk_states state ON state.wallet_id=w.id AND state.token_id=$2
 LEFT JOIN LATERAL(SELECT SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END) AS net FROM reg_crypto_balance_movements
  WHERE wallet_id=w.id AND token_id=$2 AND created_at>=b.created_at) movements ON TRUE
 WHERE w.id=ANY($1) ORDER BY w.id`, wallets, tokenID)
	if err != nil {
		return nil, fmt.Errorf("load AML inventory coverage: %w", err)
	}
	defer rows.Close()
	var result []amlInventoryCoverage
	for rows.Next() {
		var entry amlInventoryCoverage
		if err := rows.Scan(&entry.WalletID, &entry.Baseline, &entry.BaselineHeight, &entry.Ledger, &entry.Net, &entry.Covered, &entry.KnownHigh); err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

// Risk facts have their own immutable webhook/outbox events. A risk update
// never reuses payment.confirmed and never implies another financial credit.
func (r *AMLRepository) PublishNextAMLRiskEvent(ctx context.Context) (bool, error) {
	published := false
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var eventID, walletID, tokenID, merchantID, networkID id.ID
		var previous, current, reason string
		var revision, scopeVersion int64
		var covered bool
		var occurred time.Time
		var expires *time.Time
		err := q.QueryRow(ctx, `SELECT e.id,e.wallet_id,e.token_id,e.merchant_id,e.network_id,e.previous_class,e.risk_class,e.revision,e.scope_version,e.reason,e.inventory_covered,e.created_at,e.expires_at
 FROM reg_crypto_aml_risk_changes e WHERE NOT EXISTS(SELECT 1 FROM sys_merchant_events me WHERE me.merchant_id=e.merchant_id AND me.causation_key='aml-risk:'||e.id::text)
 ORDER BY e.created_at,e.id FOR UPDATE OF e SKIP LOCKED LIMIT 1`).Scan(&eventID, &walletID, &tokenID, &merchantID, &networkID, &previous, &current, &revision, &scopeVersion, &reason, &covered, &occurred, &expires)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		err = NewLifecycleWebhookOutbox().PublishLifecycleWebhook(ctx, crypto.LifecycleWebhookEvent{AggregateID: eventID, MerchantID: merchantID, Event: crypto.WebhookWalletRiskChanged, ResourceVersion: 1, CausationKey: "aml-risk:" + eventID.String(), OccurredAt: occurred,
			Data: map[string]any{"walletId": walletID.String(), "tokenId": tokenID.String(), "networkId": networkID.String(), "previousRiskClass": previous, "riskClass": current, "riskRevision": revision, "scopeVersion": scopeVersion, "reason": reason, "inventoryCovered": covered, "expiresAt": expires}})
		if err != nil {
			return err
		}
		published = true
		return nil
	})
	return published, err
}
