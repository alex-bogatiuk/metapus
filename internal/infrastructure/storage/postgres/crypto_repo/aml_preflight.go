package crypto_repo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

func (r *AMLRepository) RecordAMLAddressCoverage(ctx context.Context, coverage crypto.AddressWatchCoverage, addresses []string) error {
	if id.IsNil(coverage.NetworkID) || id.IsNil(coverage.TokenID) || coverage.StartHeight <= 0 || strings.TrimSpace(coverage.WatcherKey) == "" || strings.TrimSpace(coverage.LeaseOwner) == "" || coverage.FencingToken <= 0 {
		return apperror.NewValidation("invalid address coverage barrier")
	}
	keys := make([]string, 0, len(addresses))
	for _, address := range addresses {
		keys = append(keys, strings.ToLower(address))
	}
	tm := postgres.MustGetTxManager(ctx)
	return tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		// Freeze the current lease until the entire address batch is committed.
		// Validate even when there are no new eligible addresses: a stale refresh
		// must not be acknowledged as evidence from the current watcher.
		var locked bool
		err := q.QueryRow(ctx, `SELECT TRUE FROM sys_chain_watcher_state state
 JOIN cat_blockchain_networks n ON n.id=state.network_id AND n.chain_family='evm'
 JOIN cat_tokens t ON t.id=$2 AND t.network_id=n.id
 WHERE state.network_id=$1 AND state.watcher_key=$3
 AND $3=CASE WHEN t.token_standard='native' THEN 'native_eth' ELSE 'erc20:'||LOWER(t.contract_address) END
 FOR SHARE OF state`, coverage.NetworkID, coverage.TokenID, coverage.WatcherKey).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NewConflict("address coverage watcher or asset changed")
		}
		if err != nil {
			return err
		}
		var current bool
		if err := q.QueryRow(ctx, `SELECT COALESCE(lease_owner=$3 AND fencing_token=$4 AND lease_expires_at>clock_timestamp(),FALSE)
 FROM sys_chain_watcher_state WHERE network_id=$1 AND watcher_key=$2`, coverage.NetworkID, coverage.WatcherKey, coverage.LeaseOwner, coverage.FencingToken).Scan(&current); err != nil {
			return err
		}
		if !current {
			return apperror.NewConflict("address coverage watcher lease changed or expired")
		}
		// This is retained first-coverage evidence, not a command whose height
		// and executor must match on every refresh. Renewed leases and later
		// scan boundaries leave the original evidence untouched.
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_watch_coverage(wallet_id,token_id,network_id,watcher_key,start_height,lease_owner,fencing_token)
 SELECT w.id,t.id,w.network_id,$3,$4,$6,$7 FROM cat_wallets w JOIN cat_blockchain_networks n ON n.id=w.network_id AND n.chain_family='evm'
 JOIN cat_tokens t ON t.id=$2 AND t.network_id=w.network_id
 JOIN sys_chain_watcher_state state ON state.network_id=w.network_id AND state.watcher_key=$3 AND state.lease_owner=$6 AND state.fencing_token=$7 AND state.lease_expires_at>NOW()
 JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=w.custody_merchant_id AND scope.network_id=w.network_id AND scope.token_id=t.id AND scope.enabled
 JOIN reg_crypto_aml_wallet_provisions v ON v.wallet_id=w.id AND v.status='completed' JOIN reg_crypto_aml_pool_intents i ON i.id=v.id
 JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.enabled AND p.automatic_baseline
 WHERE w.network_id=$1 AND w.address_key=ANY($5::text[]) AND w.is_active AND NOT w.deletion_mark
 AND $3=CASE WHEN t.token_standard='native' THEN 'native_eth' ELSE 'erc20:'||LOWER(t.contract_address) END
 ON CONFLICT(wallet_id,token_id,watcher_key) DO NOTHING`, coverage.NetworkID, coverage.TokenID, coverage.WatcherKey, coverage.StartHeight, keys, coverage.LeaseOwner, coverage.FencingToken)
		return err
	})
}

func (r *AMLRepository) SaveAMLPreflight(ctx context.Context, job crypto.AMLPreflightJob, proof crypto.EmptyCheckpoint) error {
	if err := proof.Validate(time.Now()); err != nil {
		return err
	}
	if proof.NetworkID != job.NetworkID || proof.Asset != job.Asset || proof.Address != job.Address {
		return apperror.NewConflict("preflight identity changed")
	}
	payload, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	tm := postgres.MustGetTxManager(ctx)
	return tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		if err := lockAMLPreflightAttempt(ctx, job, true); err != nil {
			return err
		}
		observation := id.New()
		// Retain the RPC clock in proof, and anchor a small positive host/DB
		// clock skew at SQL time so zero reconciliation never lies in the future.
		var observedAt time.Time
		err := q.QueryRow(ctx, `INSERT INTO reg_crypto_aml_preflight_observations(id,job_id,lease_token,wallet_id,token_id,network_id,block_number,block_hash,chain_amount,proof,observed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,LEAST($11::timestamptz,NOW())) RETURNING observed_at`, observation, job.ID, job.LeaseToken, job.WalletID, job.TokenID, job.NetworkID, proof.Height, proof.Hash, proof.Sources[0].Balance, payload, proof.ObservedAt).Scan(&observedAt)
		if err != nil {
			return err
		}
		var history bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_aml_effects WHERE wallet_id=$1 AND token_id=$2) OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE wallet_id=$1 AND token_id=$2) OR EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_observations WHERE wallet_id=$1 AND token_id=$2 AND chain_amount<>0)`, job.WalletID, job.TokenID).Scan(&history); err != nil {
			return err
		}
		state, code := "completed", ""
		if !proof.Empty() || history {
			state, code = "needs_attention", "preflight_inventory_detected"
			_, err = q.Exec(ctx, `SELECT fn_crypto_hold_aml_preflight($1,$2,$3)`, job.WalletID, job.TokenID, observation)
		} else {
			run, check := id.New(), id.New()
			_, err = q.Exec(ctx, `INSERT INTO reg_crypto_reconciliation_runs(id,network_id,status,worker_id,completed_at,wallets_checked) VALUES($1,$2,'completed','aml-preflight',NOW(),1)`, run, job.NetworkID)
			if err == nil {
				_, err = q.Exec(ctx, `INSERT INTO reg_crypto_reconciliation_checks(id,run_id,network_id,wallet_id,token_id,ledger_amount,chain_amount,delta_amount,observed_at) VALUES($1,$2,$3,$4,$5,0,0,0,$6)`, check, run, job.NetworkID, job.WalletID, job.TokenID, observedAt)
			}
			if err == nil {
				err = r.RegisterAMLWalletBaseline(ctx, job.WalletID, job.TokenID, check, proof.Height, proof.Hash, observation.String(), "aml-preflight")
			}
		}
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `UPDATE reg_crypto_aml_preflight_jobs SET status=$2,error_code=$3,lease_token=NULL,lease_expires_at=NULL WHERE id=$1`, job.ID, state, code)
		return err
	})
}

func (r *AMLRepository) RetryAMLPreflight(ctx context.Context, job crypto.AMLPreflightJob, code string) error {
	if code != "checkpoint_unavailable" && code != "checkpoint_rejected" {
		return apperror.NewValidation("invalid preflight error code")
	}
	tm := postgres.MustGetTxManager(ctx)
	return tm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := lockAMLPreflightAttempt(ctx, job, false); err != nil {
			return err
		}
		_, err := tm.GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_aml_preflight_jobs SET status=CASE WHEN attempts>=120 THEN 'needs_attention' ELSE 'retry_wait' END,next_attempt_at=clock_timestamp()+interval '30 seconds',error_code=$3,lease_token=NULL,lease_expires_at=NULL WHERE id=$1 AND lease_token=$2 AND status='processing'`, job.ID, job.LeaseToken, code)
		return err
	})
}

const amlPoolActivationCandidates = ` FROM sys_crypto_aml_pool_policies p JOIN crypto_aml_pool_runtime runtime ON runtime.id=p.id
 JOIN reg_crypto_aml_pool_intents i ON i.policy_id=p.id JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id AND v.status='completed'
 JOIN cat_wallets w ON w.id=v.wallet_id AND w.is_active AND NOT w.deletion_mark AND w.chain_activation_status='unknown' AND w.wallet_role=p.wallet_role AND w.custody_merchant_id=p.merchant_id AND w.network_id=p.network_id
 JOIN cat_blockchain_networks n ON n.id=p.network_id AND n.chain_family='tron'
 JOIN sys_crypto_custody_policies custody ON custody.network_id=p.network_id AND custody.enforced
 WHERE p.automatic_baseline
 AND EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s JOIN cat_tokens t ON t.id=s.token_id AND t.token_standard='native' JOIN reg_crypto_aml_wallet_baselines b ON b.wallet_id=w.id AND b.token_id=t.id WHERE s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.enabled)
 AND (p.wallet_role='network_fee' OR fn_crypto_aml_pool_baselines_ready(i.id))
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=w.id AND status='open')
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_account_activations WHERE wallet_id=w.id)`

// QueueNextAMLPoolActivation is a queue hint, not a replay of an existing task.
// Both selection and publication require readiness; an unready older network
// must not prevent another network from progressing.
func (r *AMLRepository) QueueNextAMLPoolActivation(ctx context.Context) (bool, error) {
	queued := false
	tm := postgres.MustGetTxManager(ctx)
	err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var network, wallet id.ID
		err := q.QueryRow(ctx, `SELECT p.network_id,w.id`+amlPoolActivationCandidates+`
 ORDER BY i.created_at,i.id FOR UPDATE OF custody SKIP LOCKED LIMIT 1`).Scan(&network, &wallet)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := lockMerchantPoolNetwork(ctx, network); err != nil {
			return err
		}
		var locked bool
		if err := q.QueryRow(ctx, `SELECT fn_crypto_try_lock_custody_wallets(ARRAY[$1::uuid])`, wallet).Scan(&locked); err != nil {
			return err
		}
		if !locked {
			return nil
		}
		tag, err := q.Exec(ctx, `INSERT INTO reg_crypto_account_activations(id,wallet_id,network_id,status)
 SELECT $2,w.id,w.network_id,'claimed'`+amlPoolActivationCandidates+` AND w.id=$1
 ON CONFLICT(network_id,wallet_id) WHERE status IN ('claimed','signing','broadcast','confirmed','held') DO NOTHING`, wallet, id.New())
		queued = err == nil && tag.RowsAffected() == 1
		return err
	})
	return queued, err
}
