package crypto_repo

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const ingestionCutoverColumns = `network_id,start_height,start_hash,start_timestamp,mode,decoder_version,activated_at`

func (r *BlockIngestionRepo) EnsureCutover(ctx context.Context, network crypto.NetworkRef, block crypto.WatcherBlockFingerprint, mode string, assets []crypto.AssetRef) (crypto.IngestionCutover, error) {
	var result crypto.IngestionCutover
	if id.IsNil(network.ID) || block.BlockNumber < 0 || block.BlockNumber == math.MaxInt64 || block.BlockHash == "" || block.Timestamp <= 0 {
		return result, apperror.NewValidation("invalid ingestion network or starting block")
	}
	if mode != "shadow" && mode != "solidified" && mode != "hybrid" && mode != "paused" {
		return result, apperror.NewValidation("invalid block ingestion mode")
	}
	tm := postgres.MustGetTxManager(ctx)
	err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var isolation string
		if err := q.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
			return err
		}
		if isolation != "read committed" {
			return apperror.NewValidation("ingestion registration requires read committed isolation")
		}
		var locked bool
		if err := q.QueryRow(ctx, `SELECT fn_crypto_try_lock_watch_network($1)`, network.ID).Scan(&locked); err != nil {
			return err
		}
		if !locked {
			return crypto.ErrIngestionRegistrationBusy
		}
		var family, identifier string
		if err := q.QueryRow(ctx, `SELECT chain_family,network_identifier FROM cat_blockchain_networks WHERE id=$1 FOR SHARE NOWAIT`, network.ID).Scan(&family, &identifier); err != nil {
			return err
		}
		if family != "tron" || (network.Family != "" && string(network.Family) != family) || (network.NetworkIdentifier != "" && network.NetworkIdentifier != identifier) {
			return apperror.NewConflict("ingestion network identity changed")
		}
		var wallets []id.ID
		// Existing registrations have immutable owners. Lock only owners of
		// new targets, so restarting a network does not lock its entire pool.
		if err := q.QueryRow(ctx, `SELECT ARRAY(SELECT w.id FROM cat_wallets w WHERE w.network_id=$1 AND w.vault_key_id<>'' AND w.address<>''
 AND NOT EXISTS(SELECT 1 FROM sys_crypto_watch_targets t WHERE t.network_id=$1 AND t.target_kind='address' AND t.target_key=w.address AND t.owner_id=w.id) ORDER BY w.id)`, network.ID).Scan(&wallets); err != nil {
			return err
		}
		if len(wallets) > 0 {
			if err := q.QueryRow(ctx, `SELECT fn_crypto_try_lock_custody_wallets($1::uuid[])`, wallets).Scan(&locked); err != nil {
				return err
			}
			if !locked {
				return crypto.ErrIngestionRegistrationBusy
			}
		}
		// Lock metadata before the ingestion row, matching catalog callbacks.
		ids := make([]id.ID, 0, len(assets)+1)
		for _, asset := range assets {
			ids = append(ids, asset.TokenID)
		}
		if !id.IsNil(network.NativeTokenID) {
			ids = append(ids, network.NativeTokenID)
		}
		if _, err := q.Exec(ctx, `SELECT id FROM cat_tokens WHERE id=ANY($1::uuid[]) ORDER BY id FOR SHARE NOWAIT`, ids); err != nil {
			return err
		}
		if !id.IsNil(network.NativeTokenID) {
			var valid bool
			if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_tokens WHERE id=$1 AND network_id=$2 AND token_standard='native' AND decimal_places=$3)`, network.NativeTokenID, network.ID, network.NativeDecimals).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				return apperror.NewConflict("ingestion native asset changed")
			}
		}
		for _, asset := range assets {
			var standard, contract string
			var decimals int
			err := q.QueryRow(ctx, `SELECT token_standard,COALESCE(contract_address,''),decimal_places FROM cat_tokens WHERE id=$1 AND network_id=$2`, asset.TokenID, network.ID).Scan(&standard, &contract, &decimals)
			if errors.Is(err, pgx.ErrNoRows) {
				return apperror.NewConflict("ingestion asset belongs to another network or is missing")
			}
			if err != nil {
				return err
			}
			normalize := func(value string) string { return strings.ToLower(strings.ReplaceAll(value, "-", "")) }
			if contract != asset.ContractAddress || decimals != asset.Decimals || normalize(standard) != normalize(string(asset.Standard)) || (normalize(standard) != "native" && normalize(standard) != "trc20") {
				return apperror.NewConflict("ingestion asset identity or scale changed")
			}
		}
		err := pgxscan.Get(ctx, q, &result, `SELECT `+ingestionCutoverColumns+` FROM sys_crypto_block_ingestion WHERE network_id=$1 FOR UPDATE`, network.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			_, err = q.Exec(ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version,activated_at)
 VALUES($1,$2,$3,$4,$5,2,CASE WHEN $5 IN ('solidified','hybrid') THEN clock_timestamp() END)`, network.ID, block.BlockNumber, block.BlockHash, block.Timestamp, mode)
		} else if err == nil {
			if block.BlockNumber == result.Height && (!strings.EqualFold(block.BlockHash, result.Hash) || block.Timestamp != result.Timestamp) {
				return apperror.NewConflict("ingestion starting block evidence conflicts with the saved boundary")
			}
			if result.ActivatedAt != nil && mode == "shadow" {
				return apperror.NewConflict("activated ingestion cannot downgrade to shadow")
			}
			if result.ActivatedAt == nil && (mode == "solidified" || mode == "hybrid") {
				if block.BlockNumber < result.Height {
					return apperror.NewConflict("financial ingestion cannot begin before its registered boundary")
				}
				_, err = q.Exec(ctx, `UPDATE sys_crypto_block_ingestion SET start_height=$2,start_hash=$3,start_timestamp=$4,mode=$5,activated_at=clock_timestamp(),updated_at=clock_timestamp() WHERE network_id=$1`, network.ID, block.BlockNumber, block.BlockHash, block.Timestamp, mode)
			} else if result.Mode != mode {
				_, err = q.Exec(ctx, `UPDATE sys_crypto_block_ingestion SET mode=$2,updated_at=clock_timestamp() WHERE network_id=$1`, network.ID, mode)
			}
		}
		if err != nil {
			return err
		}
		if err := pgxscan.Get(ctx, q, &result, `SELECT `+ingestionCutoverColumns+` FROM sys_crypto_block_ingestion WHERE network_id=$1`, network.ID); err != nil {
			return err
		}
		_, err = q.Exec(ctx, `INSERT INTO sys_crypto_watch_targets(network_id,target_kind,target_key,owner_id,start_height,source)
 SELECT network_id,'address',address,id,$2,'cutover_inventory' FROM cat_wallets WHERE network_id=$1 AND vault_key_id<>'' AND address<>''
 ORDER BY id ON CONFLICT(network_id,target_kind,target_key) DO NOTHING`, network.ID, result.Height+1)
		if err != nil {
			return err
		}
		for _, asset := range assets {
			if asset.ContractAddress == "" {
				continue
			}
			if _, err := q.Exec(ctx, `INSERT INTO sys_crypto_watch_targets(network_id,target_kind,target_key,owner_id,start_height,source)
 VALUES($1,'asset',$2,$3,$4,'cutover_inventory') ON CONFLICT(network_id,target_kind,target_key) DO NOTHING`, network.ID, asset.ContractAddress, asset.TokenID, result.Height+1); err != nil {
				return err
			}
		}
		return nil
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		return result, crypto.ErrIngestionRegistrationBusy
	}
	return result, err
}
