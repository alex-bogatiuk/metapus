package crypto_repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"
)

// PlanAMLTestnetBootstrap deterministically assigns the single merchant claim
// to existing owned principal wallets. Remaining native money is operating
// capital; non-native residual must exactly match the existing service fee.
// No SQL or Vault state is changed.
func (r *AMLRepository) PlanAMLTestnetBootstrap(ctx context.Context, plan aml.TestnetBootstrap) (aml.TestnetBootstrap, error) {
	tm := postgres.MustGetTxManager(ctx)
	opts := postgres.DefaultTxOptions()
	opts.AccessMode, opts.IsolationLevel = pgx.ReadOnly, pgx.RepeatableRead
	err := tm.RunInTransactionWithOptions(ctx, opts, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		principal := map[id.ID]types.CryptoAmount{}
		fees := map[id.ID]types.CryptoAmount{}
		rows, err := q.Query(ctx, `SELECT s.token_id,COALESCE(b.amount,0),COALESCE((SELECT SUM(CASE WHEN f.record_type='receipt' THEN f.amount ELSE -f.amount END) FROM reg_crypto_fee_movements f WHERE f.token_id=s.token_id),0) FROM sys_crypto_aml_routing_scopes s LEFT JOIN reg_crypto_merchant_balance_balances b ON b.merchant_id=s.merchant_id AND b.token_id=s.token_id WHERE s.merchant_id=$1 AND s.network_id=$2 AND s.enabled ORDER BY s.token_id`, plan.MerchantID, plan.NetworkID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var token id.ID
			var p, f types.CryptoAmount
			if err := rows.Scan(&token, &p, &f); err != nil {
				rows.Close()
				return err
			}
			principal[token], fees[token] = p, f
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		rows, err = q.Query(ctx, `SELECT w.id,s.token_id,t.token_standard='native',w.wallet_role,COALESCE(b.amount,0),c.id,fn_crypto_aml_ledger_hash(w.id,s.token_id,'infinity')
 FROM sys_crypto_aml_routing_scopes s JOIN cat_wallets w ON w.network_id=s.network_id AND w.custody_merchant_id=s.merchant_id JOIN cat_tokens t ON t.id=s.token_id
 LEFT JOIN reg_crypto_balance_balances b ON b.wallet_id=w.id AND b.token_id=s.token_id
 LEFT JOIN LATERAL(SELECT checkrow.id FROM reg_crypto_reconciliation_checks checkrow WHERE checkrow.wallet_id=w.id AND checkrow.token_id=s.token_id AND checkrow.delta_amount=0 AND checkrow.chain_amount=COALESCE(b.amount,0) AND checkrow.ledger_amount=COALESCE(b.amount,0) AND checkrow.observed_at BETWEEN NOW()-interval '10 minutes' AND NOW() ORDER BY checkrow.observed_at DESC,checkrow.id DESC LIMIT 1)c ON TRUE
 WHERE s.merchant_id=$1 AND s.network_id=$2 AND s.enabled AND (w.is_active AND NOT w.deletion_mark OR COALESCE(b.amount,0)<>0)
 ORDER BY s.token_id,CASE w.wallet_role WHEN 'liquidity_hot' THEN 0 WHEN 'merchant_pool' THEN 1 ELSE 2 END,w.id`, plan.MerchantID, plan.NetworkID)
		if err != nil {
			return err
		}
		plan.Openings = nil
		for rows.Next() {
			var o aml.TestnetOpening
			var native bool
			var role string
			var physical types.CryptoAmount
			var check *id.ID
			if err := rows.Scan(&o.WalletID, &o.TokenID, &native, &role, &physical, &check, &o.LedgerHash); err != nil {
				rows.Close()
				return err
			}
			if check == nil {
				rows.Close()
				return apperror.NewConflict("fresh matching chain reconciliation missing for " + o.WalletID.String() + "/" + o.TokenID.String())
			}
			o.CheckID = *check
			remaining := physical
			if role == "merchant_pool" || role == "liquidity_hot" {
				o.Principal = minAMLBootstrapAmount(remaining, principal[o.TokenID])
				remaining = remaining.Sub(o.Principal)
				principal[o.TokenID] = principal[o.TokenID].Sub(o.Principal)
				o.ServiceFee = minAMLBootstrapAmount(remaining, fees[o.TokenID])
				remaining = remaining.Sub(o.ServiceFee)
				fees[o.TokenID] = fees[o.TokenID].Sub(o.ServiceFee)
			}
			if !native && !remaining.IsZero() {
				rows.Close()
				return apperror.NewConflict("unattributed non-native physical inventory")
			}
			o.Operating = remaining
			plan.Openings = append(plan.Openings, o)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for token, p := range principal {
			if !p.IsZero() || !fees[token].IsZero() {
				return apperror.NewConflict("physical inventory cannot cover merchant principal and service fees")
			}
		}
		return plan.Validate(time.Now())
	})
	return plan, err
}

// ApplyAMLTestnetBootstrap imports only AML component state. The SQL guards
// retain immutable evidence and independently prohibit mainnet and double use.
func (r *AMLRepository) ApplyAMLTestnetBootstrap(ctx context.Context, plan aml.TestnetBootstrap) error {
	if err := plan.Validate(time.Now()); err != nil {
		return err
	}
	raw, err := json.MarshalDeterministic(plan)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	tm := postgres.MustGetTxManager(ctx)
	return tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var previous string
		err := q.QueryRow(ctx, `SELECT request_digest FROM reg_crypto_aml_testnet_bootstraps WHERE id=$1`, plan.ID).Scan(&previous)
		if err == nil {
			if previous != digest {
				return apperror.NewConflict("bootstrap request changed")
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err = q.Exec(ctx, `LOCK TABLE reg_crypto_balance_movements,reg_crypto_merchant_balance_movements,reg_crypto_fee_movements IN SHARE MODE`); err != nil {
			return err
		}
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_testnet_bootstraps(id,merchant_id,network_id,block_number,block_hash,expires_at,actor,request_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, plan.ID, plan.MerchantID, plan.NetworkID, plan.BlockNumber, plan.BlockHash, plan.ExpiresAt, plan.Actor, digest)
		if err != nil {
			return err
		}
		for _, o := range plan.Openings {
			_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_testnet_openings(bootstrap_id,wallet_id,token_id,merchant_id,network_id,reconciliation_check_id,principal,service_fee,operating,ledger_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, plan.ID, o.WalletID, o.TokenID, plan.MerchantID, plan.NetworkID, o.CheckID, o.Principal, o.ServiceFee, o.Operating, o.LedgerHash)
			if err != nil {
				return fmt.Errorf("opening %s/%s: %w", o.WalletID, o.TokenID, err)
			}
			_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_wallet_baselines(wallet_id,token_id,merchant_id,network_id,reconciliation_check_id,block_number,block_hash,evidence_reference,verified_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'testnet-bootstrap')`, o.WalletID, o.TokenID, plan.MerchantID, plan.NetworkID, o.CheckID, plan.BlockNumber, plan.BlockHash, plan.ID.String())
			if err != nil {
				return err
			}
			if _, err = q.Exec(ctx, `SELECT fn_crypto_aml_invalidate_wallet($1,$2,'testnet_bootstrap')`, o.WalletID, o.TokenID); err != nil {
				return err
			}
		}
		var complete bool
		err = q.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s LEFT JOIN reg_crypto_merchant_balance_balances b ON b.merchant_id=s.merchant_id AND b.token_id=s.token_id WHERE s.merchant_id=$1 AND s.network_id=$2 AND s.enabled AND (COALESCE(b.amount,0)<>COALESCE((SELECT SUM(o.principal) FROM reg_crypto_aml_testnet_openings o WHERE o.bootstrap_id=$3 AND o.token_id=s.token_id),0) OR COALESCE((SELECT SUM(CASE WHEN f.record_type='receipt' THEN f.amount ELSE -f.amount END) FROM reg_crypto_fee_movements f WHERE f.token_id=s.token_id),0)<>COALESCE((SELECT SUM(o.service_fee) FROM reg_crypto_aml_testnet_openings o WHERE o.bootstrap_id=$3 AND o.token_id=s.token_id),0))) AND NOT EXISTS(SELECT 1 FROM cat_wallets w JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=$1 AND s.network_id=w.network_id AND s.enabled LEFT JOIN reg_crypto_balance_balances b ON b.wallet_id=w.id AND b.token_id=s.token_id WHERE w.network_id=$2 AND (w.is_active AND NOT w.deletion_mark OR COALESCE(b.amount,0)<>0) AND (w.custody_merchant_id IS DISTINCT FROM $1 OR NOT EXISTS(SELECT 1 FROM reg_crypto_aml_testnet_openings o WHERE o.bootstrap_id=$3 AND o.wallet_id=w.id AND o.token_id=s.token_id)))`, plan.MerchantID, plan.NetworkID, plan.ID).Scan(&complete)
		if err != nil {
			return err
		}
		if !complete {
			return apperror.NewConflict("bootstrap must cover every owned wallet and preserve exact merchant and fee totals")
		}
		return nil
	})
}

func minAMLBootstrapAmount(a, b types.CryptoAmount) types.CryptoAmount {
	if a.Cmp(b) < 0 {
		return a
	}
	return b
}
