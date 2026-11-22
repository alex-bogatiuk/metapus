package crypto_repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/postgres"
)

type amlScope struct {
	MerchantID, NetworkID, TokenID, AccountID id.ID
	PolicyVersion, MappingVersion, Family     string
	Version                                   int64
	DeadlineSeconds                           int
}

// ObserveVerification shares the verified-chain transaction. It observes every
// owned recipient, including hot/retired/dust/unmatched wallets, before invoice
// attribution. Neither this method nor classification writes financial money.
func (r *AMLRepository) ObserveVerification(ctx context.Context, proofID id.ID) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("AML observation requires verified-chain transaction")
	}
	q := tm.GetQuerier(ctx)
	var raw []byte
	if err := q.QueryRow(ctx, `SELECT evidence FROM reg_crypto_transfer_verifications WHERE id=$1`, proofID).Scan(&raw); err != nil {
		return err
	}
	var proof crypto.TransferVerification
	if err := json.Unmarshal(raw, &proof); err != nil {
		return fmt.Errorf("decode AML transfer evidence: %w", err)
	}
	event := proof.Event
	var scope amlScope
	var walletID id.ID
	err := q.QueryRow(ctx, `SELECT w.id,s.merchant_id,s.network_id,s.token_id,s.provider_account_id,s.policy_version,s.mapping_version,s.version,s.job_deadline_seconds,n.chain_family
 FROM cat_wallets w JOIN cat_blockchain_networks n ON n.id=w.network_id
 JOIN cat_tokens t ON t.network_id=w.network_id AND CASE WHEN n.chain_family='evm' THEN LOWER(COALESCE(t.contract_address,''))=LOWER($3) ELSE COALESCE(t.contract_address,'')=$3 END
 JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=w.network_id AND s.token_id=t.id AND s.enabled
 WHERE w.network_id=$1 AND w.address_key=CASE WHEN n.chain_family='evm' THEN LOWER($2) ELSE $2 END
 FOR UPDATE OF s`, event.NetworkID, event.ToAddress, event.TokenContract).Scan(&walletID, &scope.MerchantID, &scope.NetworkID, &scope.TokenID, &scope.AccountID, &scope.PolicyVersion, &scope.MappingVersion, &scope.Version, &scope.DeadlineSeconds, &scope.Family)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := proof.Validate(); err != nil {
		return fmt.Errorf("AML requires valid persisted transfer proof: %w", err)
	}
	if event.Canonicality == crypto.CanonicalityOrphaned {
		rows, err := q.Query(ctx, `UPDATE reg_crypto_aml_effect_chain_states cs SET canonicality='orphaned',verification_id=$6,updated_at=NOW()
   FROM reg_crypto_aml_effects e WHERE e.id=cs.effect_id AND e.network_id=$1 AND e.tx_hash=$2 AND e.event_kind=$3 AND e.event_index=$4 AND e.economics_hash=$5
   AND cs.canonicality<>'orphaned' RETURNING e.wallet_id,e.token_id`, event.NetworkID, event.TxHash, event.EffectiveEventKind(), event.EventIndex, event.EconomicsHash(), proofID)
		if err != nil {
			return err
		}
		type affected struct{ Wallet, Token id.ID }
		var dirty []affected
		for rows.Next() {
			var row affected
			if err := rows.Scan(&row.Wallet, &row.Token); err != nil {
				rows.Close()
				return err
			}
			dirty = append(dirty, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, row := range dirty {
			if _, err := q.Exec(ctx, `SELECT fn_crypto_aml_invalidate_wallet($1,$2,'chain_orphaned')`, row.Wallet, row.Token); err != nil {
				return err
			}
		}
		return nil
	}
	var imported bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_aml_testnet_openings o JOIN reg_crypto_aml_testnet_bootstraps h ON h.id=o.bootstrap_id WHERE o.wallet_id=$1 AND o.token_id=$2 AND h.block_number>=$3)`, walletID, scope.TokenID, event.BlockNumber).Scan(&imported); err != nil {
		return err
	}
	if imported {
		return nil
	}
	var sourceID, expectedID *id.ID
	// Exact expected effect and immutable same-merchant source identify controlled
	// internal provenance. Arbitrary same-tenant/cross-merchant sends do not.
	rows, err := q.Query(ctx, `SELECT sw.id,x.id FROM reg_crypto_expected_chain_effects x
 JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id AND a.signed_envelope IS NOT NULL
 JOIN cat_wallets sw ON sw.network_id=x.network_id AND sw.address_key=x.source_address_key AND sw.custody_merchant_id=$8
 WHERE x.network_id=$1 AND x.tx_hash=$2 AND x.event_kind=$3 AND x.destination_address_key=$4 AND x.source_address_key=$5
 AND x.token_id=$6 AND x.amount=$7 AND a.merchant_id=$8`, event.NetworkID, event.TxHash, event.EffectiveEventKind(), canonicalAMLAddress(event.ToAddress, scope.Family), canonicalAMLAddress(event.FromAddress, scope.Family), scope.TokenID, event.Amount, scope.MerchantID)
	if err != nil {
		return err
	}
	matches := 0
	var src, expected id.ID
	for rows.Next() {
		if err := rows.Scan(&src, &expected); err != nil {
			rows.Close()
			return err
		}
		matches++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if matches == 1 {
		sourceID = &src
		expectedID = &expected
	}
	var effectID id.ID
	err = q.QueryRow(ctx, `INSERT INTO reg_crypto_aml_effects(wallet_id,merchant_id,network_id,token_id,verification_id,tx_hash,event_kind,event_index,block_number,block_hash,economics_hash,amount,source_wallet_id,expected_effect_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
 ON CONFLICT(network_id,tx_hash,event_kind,event_index,block_hash,economics_hash,wallet_id) DO NOTHING RETURNING id`, walletID, scope.MerchantID, scope.NetworkID, scope.TokenID, proofID, event.TxHash, event.EffectiveEventKind(), event.EventIndex, event.BlockNumber, event.BlockHash, event.EconomicsHash(), event.Amount, sourceID, expectedID).Scan(&effectID)
	inserted := err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		if err = q.QueryRow(ctx, `SELECT id FROM reg_crypto_aml_effects WHERE network_id=$1 AND tx_hash=$2 AND event_kind=$3 AND event_index=$4 AND block_hash=$5 AND economics_hash=$6 AND wallet_id=$7`, event.NetworkID, event.TxHash, event.EffectiveEventKind(), event.EventIndex, event.BlockHash, event.EconomicsHash(), walletID).Scan(&effectID); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `INSERT INTO reg_crypto_aml_effect_chain_states(effect_id,canonicality,verification_id,coverage_complete) VALUES($1,'canonical',$2,$3)
 ON CONFLICT(effect_id) DO UPDATE SET canonicality='canonical',verification_id=EXCLUDED.verification_id,updated_at=NOW(),
 coverage_complete=reg_crypto_aml_effect_chain_states.coverage_complete AND EXCLUDED.coverage_complete
 WHERE reg_crypto_aml_effect_chain_states.canonicality<>'canonical' OR
       (reg_crypto_aml_effect_chain_states.coverage_complete AND NOT EXCLUDED.coverage_complete)`, effectID, proofID, proof.MatchingEffects == 1 && event.EffectiveEventKind() != crypto.EventKindInternalValue)
	if err != nil {
		return err
	}
	if !inserted && tag.RowsAffected() == 0 {
		return nil
	}
	if inserted && sourceID == nil {
		jobID, err := r.EnqueueAMLCheck(ctx, EnqueueAMLInput{WalletID: walletID, ProviderAccountID: scope.AccountID, MappingVersion: scope.MappingVersion, DeadlineAt: time.Now().Add(time.Duration(scope.DeadlineSeconds) * time.Second),
			Request: aml.CheckRequest{MerchantID: scope.MerchantID, NetworkID: scope.NetworkID, TokenID: scope.TokenID, Kind: aml.TransferSubject, ContractAddress: event.TokenContract, Address: event.ToAddress, TxHash: event.TxHash, EventKind: string(event.EffectiveEventKind()), EventIndex: event.EventIndex, BlockHash: event.BlockHash, EconomicsHash: event.EconomicsHash(), MatchingEffects: proof.MatchingEffects}})
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_effect_checks(effect_id,subject_id) SELECT $1,subject_id FROM reg_crypto_aml_jobs WHERE id=$2`, effectID, jobID)
		if err != nil {
			return err
		}
	}
	_, err = q.Exec(ctx, `SELECT fn_crypto_aml_invalidate_wallet($1,$2,'verified_incoming')`, walletID, scope.TokenID)
	return err
}

// Incoming captures use catalog-normalized EVM keys, while TRON preserves case.
// Contract shape is not used to infer the family; SQL resolves it from network.
func canonicalAMLAddress(address, family string) string {
	if family == "evm" {
		return strings.ToLower(address)
	}
	return address
}

func (r *AMLRepository) invalidateAMLSubject(ctx context.Context, subjectID id.ID, reason string) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `SELECT fn_crypto_aml_invalidate_wallet(wallet_id,token_id,$2) FROM reg_crypto_aml_subjects WHERE id=$1`, subjectID, reason)
	return err
}

// CaptureAMLBacklog catches proofs persisted before scope enablement or a
// restart. Idempotent effect identity prevents another paid job on replay.
func (r *AMLRepository) CaptureAMLBacklog(ctx context.Context, limit int) (int, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `SELECT v.id FROM reg_crypto_transfer_verifications v
 JOIN cat_blockchain_networks n ON n.id=v.network_id JOIN cat_wallets w ON w.network_id=v.network_id
 AND w.address_key=CASE WHEN n.chain_family='evm' THEN LOWER(v.evidence->'event'->>'toAddress') ELSE v.evidence->'event'->>'toAddress' END
 JOIN cat_tokens t ON t.network_id=v.network_id AND CASE WHEN n.chain_family='evm' THEN LOWER(COALESCE(t.contract_address,''))=LOWER(v.evidence->'event'->>'tokenContract') ELSE COALESCE(t.contract_address,'')=v.evidence->'event'->>'tokenContract' END
 JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=v.network_id AND s.token_id=t.id AND s.enabled
 WHERE v.evidence->'event'->>'canonicality'='canonical'
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_testnet_openings o JOIN reg_crypto_aml_testnet_bootstraps h ON h.id=o.bootstrap_id WHERE o.wallet_id=w.id AND o.token_id=t.id AND h.block_number>=(v.evidence->'event'->>'blockNumber')::bigint)
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_transfer_verifications newer WHERE newer.network_id=v.network_id AND newer.tx_hash=v.tx_hash AND newer.event_kind=v.event_kind AND newer.event_index=v.event_index AND newer.economics_hash=v.economics_hash AND (newer.verified_at,newer.id)>(v.verified_at,v.id))
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_effects e WHERE e.network_id=v.network_id AND e.tx_hash=v.tx_hash AND e.event_kind=v.event_kind AND e.event_index=v.event_index AND e.block_hash=v.block_hash AND e.economics_hash=v.economics_hash AND e.wallet_id=w.id)
 ORDER BY v.verified_at,v.id LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	var ids []id.ID
	for rows.Next() {
		var value id.ID
		if err := rows.Scan(&value); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, value := range ids {
		if err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error { return r.ObserveVerification(ctx, value) }); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// RegisterAMLWalletBaseline is control-plane only. The existing zero balance
// check is necessary but not a substitute for its explicit checkpoint evidence.
func (r *AMLRepository) RegisterAMLWalletBaseline(ctx context.Context, walletID, tokenID, checkID id.ID, height int64, hash, reference, actor string) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var merchant, network id.ID
		if err := q.QueryRow(ctx, `SELECT s.merchant_id,s.network_id FROM sys_crypto_aml_routing_scopes s JOIN cat_wallets w ON w.custody_merchant_id=s.merchant_id AND w.network_id=s.network_id WHERE w.id=$1 AND s.token_id=$2 FOR UPDATE OF s`, walletID, tokenID).Scan(&merchant, &network); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, walletID); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `INSERT INTO reg_crypto_aml_wallet_baselines(wallet_id,token_id,merchant_id,network_id,reconciliation_check_id,block_number,block_hash,evidence_reference,verified_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, walletID, tokenID, merchant, network, checkID, height, hash, reference, actor)
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `SELECT fn_crypto_aml_invalidate_wallet($1,$2,'baseline_registered')`, walletID, tokenID)
		return err
	})
}
