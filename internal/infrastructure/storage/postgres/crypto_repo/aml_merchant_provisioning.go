package crypto_repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

func (r *AMLRepository) ClaimMerchantWalletProvision(ctx context.Context, request crypto.MerchantWalletRequest, worker string, lease time.Duration) (crypto.MerchantWalletProvision, error) {
	if postgres.MustGetTxManager(ctx).GetTx(ctx) != nil {
		return crypto.MerchantWalletProvision{}, apperror.NewConflict("merchant provisioning cannot run inside a SQL transaction")
	}
	return r.claimMerchantWalletProvision(ctx, request, worker, lease)
}

func (r *AMLRepository) claimMerchantWalletProvision(ctx context.Context, request crypto.MerchantWalletRequest, worker string, lease time.Duration) (crypto.MerchantWalletProvision, error) {
	plan := crypto.MerchantWalletProvision{Request: request}
	if err := request.Validate(); err != nil {
		return plan, err
	}
	if worker == "" || lease <= 0 || lease > 5*time.Minute {
		return plan, apperror.NewValidation("bounded merchant provisioning lease required")
	}
	raw, err := json.MarshalDeterministic(request)
	if err != nil {
		return plan, err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	err = postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		if err := q.QueryRow(ctx, `SELECT vault_namespace FROM sys_crypto_aml_provisioning_identity WHERE id AND database_name=current_database()`).Scan(&plan.VaultNamespace); err != nil {
			return err
		}
		if err := q.QueryRow(ctx, `SELECT n.id,n.code,n.chain_family,n.network_identifier FROM cat_blockchain_networks n JOIN sys_crypto_custody_policies custody ON custody.network_id=n.id AND custody.enforced WHERE n.id=$1 FOR UPDATE OF custody`, request.NetworkID).Scan(&plan.Network.ID, &plan.Network.Code, &plan.Network.Family, &plan.Network.NetworkIdentifier); err != nil {
			return err
		}
		if _, err := crypto.MerchantCustodyNetwork(plan.Network); err != nil {
			return err
		}
		poolPolicy, poolStats, pooled, err := r.merchantPoolAdmission(ctx, request)
		if err != nil {
			return err
		}
		var stored, status string
		var leaseValid, due bool
		err = q.QueryRow(ctx, `SELECT request_digest,status,derivation_path,wallet_id,COALESCE(lease_expires_at>clock_timestamp(),FALSE),next_attempt_at<=clock_timestamp() FROM reg_crypto_aml_wallet_provisions WHERE id=$1 FOR UPDATE`, request.ID).Scan(&stored, &status, &plan.Job.DerivationPath, &plan.Job.WalletID, &leaseValid, &due)
		if err == nil {
			if stored != digest {
				return apperror.NewConflict("merchant wallet request identity changed")
			}
			if status == "completed" {
				return nil
			}
			if pooled && !poolPolicy.Enabled {
				return apperror.NewConflict("merchant pool provisioning is paused")
			}
			if (status == "processing" && leaseValid) || !due {
				return apperror.NewConflict("merchant wallet provision is leased or waiting for retry")
			}
		} else if errors.Is(err, pgx.ErrNoRows) {
			if pooled && (!poolPolicy.Enabled || poolStats.Started >= poolPolicy.MaxTotal || poolStats.Ready+poolStats.Pending >= poolPolicy.TargetReady) {
				return apperror.NewConflict("merchant pool has no admission capacity")
			}
			var namespace, cursorNamespace string
			var next int64
			var maxTotal, total int
			if err := q.QueryRow(ctx, `SELECT p.derivation_namespace,c.derivation_namespace,c.next_index,p.max_total_wallets FROM sys_crypto_wallet_pool_policies p JOIN sys_crypto_wallet_derivation_cursors c ON c.network_id=p.network_id WHERE p.network_id=$1 AND NOT p.needs_review AND NOT c.needs_review FOR UPDATE OF p,c`, request.NetworkID).Scan(&namespace, &cursorNamespace, &next, &maxTotal); err != nil {
				return err
			}
			if namespace == "" || namespace != cursorNamespace || next >= 2147483648 {
				return apperror.NewConflict("merchant derivation cursor unavailable or exhausted")
			}
			if err := q.QueryRow(ctx, `SELECT fn_crypto_aml_network_wallet_commitments($1,$2)`, request.NetworkID, request.ID).Scan(&total); err != nil {
				return err
			}
			if total >= maxTotal {
				return apperror.NewConflict("merchant wallet provisioning network capacity exhausted")
			}
			if request.Role == "network_fee" {
				var exists bool
				if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_wallets WHERE network_id=$1 AND custody_merchant_id=$2 AND wallet_role='network_fee' AND is_active AND NOT deletion_mark)`, request.NetworkID, request.MerchantID).Scan(&exists); err != nil {
					return err
				}
				if exists {
					return apperror.NewConflict("merchant already has an active network fee wallet")
				}
			}
			plan.Job.DerivationPath = fmt.Sprintf("%s/%d", namespace, next)
			if _, err := q.Exec(ctx, `INSERT INTO sys_crypto_wallet_derivations(network_id,derivation_path,source) VALUES($1,$2,'provisioning')`, request.NetworkID, plan.Job.DerivationPath); err != nil {
				return err
			}
			if _, err := q.Exec(ctx, `UPDATE sys_crypto_wallet_derivation_cursors SET next_index=next_index+1,version=version+1 WHERE network_id=$1`, request.NetworkID); err != nil {
				return err
			}
			plan.Job.ClaimToken = id.New()
			_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_wallet_provisions(id,merchant_id,network_id,wallet_role,derivation_path,request_digest,requested_by,claimed_by,claim_token,lease_expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW()+$10*interval '1 second')`, request.ID, request.MerchantID, request.NetworkID, request.Role, plan.Job.DerivationPath, digest, request.Actor, worker, plan.Job.ClaimToken, int64(lease.Seconds()))
			if err != nil {
				return err
			}
			plan.Job.ID = request.ID
			plan.Job.NetworkID = request.NetworkID
			plan.Job.NetworkCode = plan.Network.Code
			plan.Job.ClaimedBy = worker
			return nil
		} else {
			return err
		}
		plan.Job.ClaimToken = id.New()
		_, err = q.Exec(ctx, `UPDATE reg_crypto_aml_wallet_provisions SET status='processing',attempts=attempts+1,claimed_by=$2,claim_token=$3,lease_expires_at=NOW()+$4*interval '1 second',last_error='' WHERE id=$1`, request.ID, worker, plan.Job.ClaimToken, int64(lease.Seconds()))
		plan.Job.ID = request.ID
		plan.Job.NetworkID = request.NetworkID
		plan.Job.NetworkCode = plan.Network.Code
		plan.Job.ClaimedBy = worker
		return err
	})
	return plan, err
}

func (r *AMLRepository) CompleteMerchantWalletProvision(ctx context.Context, plan crypto.MerchantWalletProvision, derived crypto.WalletDerivation, enrollment crypto.MerchantWalletEnrollment) (id.ID, error) {
	if err := crypto.ValidateWalletDerivation(plan.Job, derived); err != nil {
		return id.Nil(), err
	}
	pluginNetwork, err := crypto.MerchantCustodyNetwork(plan.Network)
	if err != nil {
		return id.Nil(), err
	}
	if enrollment.SchemaVersion != "merchant_custody.v1" || enrollment.Network != pluginNetwork || enrollment.MerchantID != plan.Request.MerchantID.String() || enrollment.Role != plan.Request.Role ||
		(plan.Network.Family == crypto.ChainFamilyEVM && !strings.EqualFold(enrollment.Address, derived.Address)) || (plan.Network.Family != crypto.ChainFamilyEVM && enrollment.Address != derived.Address) {
		return id.Nil(), apperror.NewConflict("merchant enrollment proof mismatch")
	}
	raw, err := json.MarshalDeterministic(enrollment)
	if err != nil {
		return id.Nil(), err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	err = postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var namespace id.ID
		if err := q.QueryRow(ctx, `SELECT identity.vault_namespace FROM sys_crypto_aml_provisioning_identity identity JOIN cat_blockchain_networks network ON network.id=$1 AND network.chain_family=$2 AND network.network_identifier=$3 WHERE identity.id AND identity.database_name=current_database()`, plan.Network.ID, plan.Network.Family, plan.Network.NetworkIdentifier).Scan(&namespace); err != nil {
			return err
		}
		if namespace != plan.VaultNamespace {
			return apperror.NewConflict("provisioning namespace changed")
		}
		if _, err := q.Exec(ctx, `SELECT network_id FROM sys_crypto_custody_policies WHERE network_id=$1 AND enforced FOR UPDATE`, plan.Network.ID); err != nil {
			return err
		}
		var valid bool
		if err := q.QueryRow(ctx, `SELECT status='processing' AND claimed_by=$2 AND claim_token=$3 AND lease_expires_at>clock_timestamp() AND merchant_id=$4 AND network_id=$5 AND wallet_role=$6 AND derivation_path=$7 FROM reg_crypto_aml_wallet_provisions WHERE id=$1 FOR UPDATE`, plan.Request.ID, plan.Job.ClaimedBy, plan.Job.ClaimToken, plan.Request.MerchantID, plan.Network.ID, plan.Request.Role, derived.DerivationPath).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return apperror.NewConflict("merchant provision lease or identity changed")
		}
		tier := "hot"
		if plan.Request.Role == "merchant_pool" {
			tier = "pool"
		}
		activation := "active"
		if plan.Network.Family == crypto.ChainFamilyTRON {
			activation = "unknown"
		}
		_, err := q.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,derivation_path,vault_key_id,tier,wallet_role,status,allocation_mode,is_active,chain_activation_status,custody_merchant_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'free','transient',TRUE,$10,$11)`, plan.Request.ID, walletCode(plan.Network.Code, derived.DerivationPath), "Merchant "+plan.Request.Role, plan.Network.ID, derived.Address, derived.DerivationPath, derived.VaultKeyID, tier, plan.Request.Role, activation, plan.Request.MerchantID)
		if err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_wallet_allocation_holds(wallet_id,reason,owner_type,owner_id,details,held_by) VALUES($1,'chain_balance_check_pending','wallet_reconciliation',$1::uuid::text,jsonb_build_object('merchantProvisionId',$1::uuid::text),'aml-control-plane')`, plan.Request.ID); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_wallet_allocation_holds(wallet_id,reason,owner_type,owner_id,details,held_by)
 SELECT $1,'aml_baseline_pending','aml_pool_baseline',$1::uuid::text,jsonb_build_object('intentId',$1::uuid::text),'aml-control-plane' FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND (p.wallet_role IN ('merchant_pool','liquidity_hot') OR (p.wallet_role='network_fee' AND p.automatic_baseline)) WHERE i.id=$1`, plan.Request.ID); err != nil {
			return err
		}
		_, err = q.Exec(ctx, `UPDATE reg_crypto_aml_wallet_provisions SET status='completed',wallet_id=id,enrollment_digest=$2,completed_at=NOW(),claim_token=NULL,claimed_by='',lease_expires_at=NULL WHERE id=$1`, plan.Request.ID, digest)
		return err
	})
	if err != nil {
		return id.Nil(), err
	}
	return plan.Request.ID, nil
}

func (r *AMLRepository) RetryMerchantWalletProvision(ctx context.Context, plan crypto.MerchantWalletProvision) error {
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_aml_wallet_provisions SET status='retry_wait',last_error='derive_enrollment_or_publication_failed',next_attempt_at=NOW()+interval '30 seconds',claim_token=NULL,claimed_by='',lease_expires_at=NULL WHERE id=$1 AND status='processing' AND claimed_by=$2 AND claim_token=$3`, plan.Request.ID, plan.Job.ClaimedBy, plan.Job.ClaimToken)
	if err == nil && tag.RowsAffected() != 1 {
		return apperror.NewConflict("merchant provisioning retry lease lost")
	}
	return err
}
