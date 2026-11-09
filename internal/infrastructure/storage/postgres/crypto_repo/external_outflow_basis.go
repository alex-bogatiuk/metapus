package crypto_repo

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

func lockExternalOutflowIdentity(ctx context.Context, network id.ID, hash string, wallet *id.ID) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("external outflow write requires a transaction")
	}
	var locked bool
	if err := tm.GetQuerier(ctx).QueryRow(ctx, `SELECT fn_crypto_try_lock_external_outflow($1,$2,$3)`, network, hash, wallet).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return crypto.ErrExternalOutflowBusy
	}
	return nil
}

// Read the immutable target before taking any workflow row. The SQL prefix is
// nonblocking even when a parent command already owns another resource.
func lockExternalOutflowResources(ctx context.Context, outflow id.ID) error {
	var network id.ID
	var hash string
	var wallet *id.ID
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT network_id,tx_hash,source_wallet_id FROM reg_crypto_external_outflows WHERE id=$1`, outflow).Scan(&network, &hash, &wallet)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NewNotFound("external outflow", outflow.String())
	}
	if err != nil {
		return err
	}
	return lockExternalOutflowIdentity(ctx, network, hash, wallet)
}

func validateExternalOutflowSource(ctx context.Context, run id.ID, detected cryptoapp.DetectedExternalOutflow, event crypto.BlockchainEvent) error {
	if err := lockExternalOutflowResources(ctx, detected.OutflowID); err != nil {
		return err
	}
	var valid bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM reg_crypto_external_outflows o JOIN cat_blockchain_networks n ON n.id=o.network_id
 JOIN reg_crypto_reconciliation_runs r ON r.id=$1
 WHERE o.id=$2 AND o.network_id=$3 AND r.network_id=o.network_id AND r.status='completed'
 AND o.source_wallet_id=$4 AND o.token_id IS NOT DISTINCT FROM $5::uuid AND o.tx_hash=$6 AND o.event_kind=$7 AND o.event_index=$8 AND o.amount=$9 AND o.status=$10
 AND o.source_address_key=CASE WHEN n.chain_family='evm' THEN lower($11) ELSE $11 END
 AND o.destination_address_key=CASE WHEN n.chain_family='evm' THEN lower($12) ELSE $12 END
 AND o.contract_address_key=CASE WHEN n.chain_family='evm' THEN lower($13) ELSE $13 END)`,
		run, detected.OutflowID, detected.NetworkID, detected.WalletID, detected.TokenID, event.TxHash, event.EffectiveEventKind(), event.EventIndex, event.Amount,
		detected.Status, event.FromAddress, event.ToAddress, event.TokenContract).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid || event.NetworkID != detected.NetworkID {
		return apperror.NewConflict("external outflow observation differs from its observed source")
	}
	return nil
}

type outflowProjection struct {
	Height                  int64
	Hash                    string
	Confirmations, Required int
	Finality                string
}

func nextOutflowProjection(current outflowProjection, event crypto.BlockchainEvent) (outflowProjection, error) {
	next := current
	next.Required = max(current.Required, event.RequiredConfs)
	if current.Hash != "" && event.BlockHash != "" && strings.EqualFold(current.Hash, event.BlockHash) && current.Height != event.BlockNumber {
		return current, apperror.NewConflict("external outflow block hash was reused at a different height")
	}
	if event.BlockNumber < current.Height || (current.Hash != "" && event.BlockHash == "") {
		return next, nil
	}
	finality := externalOutflowFinality(event)
	sameBlock := current.Height == event.BlockNumber && strings.EqualFold(current.Hash, event.BlockHash)
	orphaned := current.Finality == "removed" || current.Finality == "reorged"
	if !sameBlock {
		// Retain the block that caused the unresolved reorg state. A later
		// canonical inclusion is journaled, not treated as automatic recovery.
		if orphaned && finality != "removed" && finality != "reorged" {
			return next, nil
		}
		next.Height, next.Hash, next.Confirmations, next.Finality = event.BlockNumber, event.BlockHash, event.Confirmations, finality
		return next, nil
	}
	next.Confirmations = max(current.Confirmations, event.Confirmations)
	switch {
	case finality == "removed" || finality == "reorged":
		next.Finality = finality
	case orphaned:
	case current.Finality == "finalized" || finality == "finalized":
		next.Finality = "finalized"
	case current.Finality == "confirmed" || finality == "confirmed":
		next.Finality = "confirmed"
	default:
		next.Finality = "observed"
	}
	return next, nil
}
