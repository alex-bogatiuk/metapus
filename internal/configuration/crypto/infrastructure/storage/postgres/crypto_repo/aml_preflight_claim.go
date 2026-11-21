package crypto_repo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/infrastructure/storage/postgres"
)

const amlPreflightCandidates = ` FROM reg_crypto_aml_preflight_jobs j
 JOIN sys_crypto_aml_pool_policies p ON p.id=j.policy_id AND p.enabled AND p.automatic_baseline
 JOIN reg_crypto_aml_wallet_provisions v ON v.wallet_id=j.wallet_id AND v.status='completed'
 JOIN reg_crypto_aml_pool_intents i ON i.id=v.id AND i.policy_id=j.policy_id
 JOIN cat_wallets w ON w.id=j.wallet_id AND w.is_active AND NOT w.deletion_mark AND w.wallet_role=p.wallet_role AND w.custody_merchant_id=p.merchant_id AND w.network_id=p.network_id
 JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.token_id=j.token_id AND s.enabled
 JOIN cat_tokens t ON t.id=j.token_id AND t.network_id=w.network_id AND t.is_active AND NOT t.deletion_mark
 WHERE (p.token_id IS NULL OR p.token_id=s.token_id OR t.token_standard='native') AND (p.wallet_role<>'network_fee' OR t.token_standard='native')
 AND j.attempts<120 AND ((j.status='retry_wait' AND j.next_attempt_at<=clock_timestamp()) OR (j.status='processing' AND j.lease_expires_at<=clock_timestamp()))
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines b WHERE b.wallet_id=j.wallet_id AND b.token_id=j.token_id)`

var errAMLPreflightCandidateBusy = apperror.NewConflict("preflight candidate is busy")

func amlPreflightBasis(job crypto.AMLPreflightJob) ([]byte, error) {
	return json.Marshal(map[string]any{"jobId": job.ID, "walletId": job.WalletID, "merchantId": job.MerchantID,
		"networkId": job.NetworkID, "tokenId": job.TokenID, "policyId": job.PolicyID, "policyVersion": job.PolicyVersion,
		"scopeVersion": job.ScopeVersion, "address": job.Address, "asset": job.Asset})
}

func (r *AMLRepository) ClaimAMLPreflight(ctx context.Context, worker string, lease time.Duration) (crypto.AMLPreflightJob, bool, error) {
	var job crypto.AMLPreflightJob
	if strings.TrimSpace(worker) == "" || lease < time.Second || lease > time.Minute {
		return job, false, apperror.NewValidation("preflight requires worker and bounded lease")
	}
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) != nil {
		return job, false, apperror.NewConflict("preflight RPC must run outside SQL transaction")
	}
	q := tm.GetQuerier(ctx)
	_, err := q.Exec(ctx, `UPDATE reg_crypto_aml_preflight_jobs SET status='needs_attention',lease_token=NULL,lease_expires_at=NULL,error_code='checkpoint_attempts_exhausted'
 WHERE status='processing' AND attempts>=120 AND lease_expires_at<=clock_timestamp()`)
	if err != nil {
		return job, false, err
	}
	// Queue creation commits before claiming so a batch does not retain locks
	// on up to 100 other wallets for the chosen RPC attempt.
	err = tm.RunInTransaction(ctx, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `INSERT INTO reg_crypto_aml_preflight_jobs(wallet_id,token_id,policy_id,policy_version,scope_version)
 SELECT w.id,s.token_id,p.id,p.version,s.version FROM sys_crypto_aml_pool_policies p
 JOIN reg_crypto_aml_pool_intents i ON i.policy_id=p.id JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id AND v.status='completed'
 JOIN cat_wallets w ON w.id=v.wallet_id AND w.is_active AND NOT w.deletion_mark AND w.wallet_role=p.wallet_role AND w.custody_merchant_id=p.merchant_id AND w.network_id=p.network_id
 JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.enabled
 JOIN cat_tokens t ON t.id=s.token_id AND t.network_id=w.network_id AND t.is_active AND NOT t.deletion_mark
 WHERE p.enabled AND p.automatic_baseline AND (p.token_id IS NULL OR p.token_id=s.token_id OR t.token_standard='native')
 AND (p.wallet_role<>'network_fee' OR t.token_standard='native')
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines b WHERE b.wallet_id=w.id AND b.token_id=s.token_id)
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_jobs existing WHERE existing.wallet_id=w.id AND existing.token_id=s.token_id)
 ORDER BY w.id,s.token_id LIMIT 100 ON CONFLICT(wallet_id,token_id) DO NOTHING`)
		return err
	})
	if err != nil {
		return job, false, err
	}
	claimed := false
	err = tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		rows, err := q.Query(ctx, `SELECT j.id,j.wallet_id`+amlPreflightCandidates+` ORDER BY j.next_attempt_at,j.id LIMIT 100`)
		if err != nil {
			return err
		}
		type candidate struct{ key, wallet id.ID }
		var candidates []candidate
		for rows.Next() {
			var c candidate
			if err := rows.Scan(&c.key, &c.wallet); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, c := range candidates {
			err := tm.RunInSavepoint(ctx, func(ctx context.Context) error {
				var locked bool
				if err := q.QueryRow(ctx, `SELECT fn_crypto_try_lock_custody_wallets(ARRAY[$1::uuid])`, c.wallet).Scan(&locked); err != nil {
					return err
				}
				if !locked {
					return errAMLPreflightCandidateBusy
				}
				err := q.QueryRow(ctx, `SELECT j.id,j.wallet_id,p.merchant_id,p.network_id,j.token_id,p.id,w.address,p.version,s.version,t.code,t.name,t.symbol,t.token_standard,COALESCE(t.contract_address,''),t.decimal_places`+
					amlPreflightCandidates+` AND j.id=$1 FOR UPDATE OF j SKIP LOCKED`, c.key).Scan(&job.ID, &job.WalletID, &job.MerchantID, &job.NetworkID, &job.TokenID, &job.PolicyID, &job.Address, &job.PolicyVersion, &job.ScopeVersion, &job.Asset.Code, &job.Asset.Name, &job.Asset.Symbol, &job.Asset.Standard, &job.Asset.ContractAddress, &job.Asset.Decimals)
				if errors.Is(err, pgx.ErrNoRows) {
					return errAMLPreflightCandidateBusy
				}
				if err != nil {
					return err
				}
				job.Asset.TokenID, job.LeaseToken = job.TokenID, id.New()
				basis, err := amlPreflightBasis(job)
				if err != nil {
					return err
				}
				_, err = q.Exec(ctx, `UPDATE reg_crypto_aml_preflight_jobs SET status='processing',lease_token=$2,lease_expires_at=clock_timestamp()+$3::interval,worker_id=$4,attempts=attempts+1,scope_version=$5,policy_version=$6 WHERE id=$1`, job.ID, job.LeaseToken, lease.String(), worker, job.ScopeVersion, job.PolicyVersion)
				if err != nil {
					return err
				}
				_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_preflight_attempts(job_id,lease_token,basis,worker_id,lease_expires_at)
 SELECT id,lease_token,$3::jsonb,worker_id,lease_expires_at FROM reg_crypto_aml_preflight_jobs WHERE id=$1 AND lease_token=$2`, job.ID, job.LeaseToken, string(basis))
				return err
			})
			var pgErr *pgconn.PgError
			if errors.Is(err, errAMLPreflightCandidateBusy) || (errors.As(err, &pgErr) && pgErr.Code == "55P03") {
				continue
			}
			if err != nil {
				return err
			}
			claimed = true
			break
		}
		return nil
	})
	if err != nil || !claimed {
		return crypto.AMLPreflightJob{}, false, err
	}
	return job, true, nil
}

func lockAMLPreflightAttempt(ctx context.Context, job crypto.AMLPreflightJob, currentPolicy bool) error {
	basis, err := amlPreflightBasis(job)
	if err != nil {
		return err
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var exact bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_attempts WHERE job_id=$1 AND lease_token=$2 AND basis=$3::jsonb)`, job.ID, job.LeaseToken, string(basis)).Scan(&exact); err != nil {
		return err
	}
	if !exact {
		return apperror.NewConflict("preflight attempt differs from its recorded basis")
	}
	var active bool
	if err := q.QueryRow(ctx, `SELECT fn_crypto_aml_preflight_attempt_active($1,$2,$3)`, job.ID, job.LeaseToken, currentPolicy).Scan(&active); err != nil {
		return err
	}
	if !active {
		return apperror.NewConflict("preflight attempt expired or changed")
	}
	return nil
}
