package crypto_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

type ShadowControlRepo struct{}

func NewShadowControlRepo() *ShadowControlRepo { return &ShadowControlRepo{} }

func (*ShadowControlRepo) GetShadowControl(ctx context.Context, network id.ID) (crypto.ShadowControlState, error) {
	state := crypto.ShadowControlState{NetworkID: network}
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
 SELECT n.chain_family,COALESCE(i.mode,''),i.activated_at,COALESCE(i.shadow_paused,FALSE),COALESCE(i.shadow_control_version,0),
 COALESCE(s.lease_owner<>'' AND s.lease_expires_at>NOW(),FALSE),s.last_block,COALESCE(h.created_at,s.updated_at),rs.observed_at,COALESCE(i.last_error_code,'')
 FROM cat_blockchain_networks n LEFT JOIN sys_crypto_block_ingestion i ON i.network_id=n.id
 LEFT JOIN sys_chain_watcher_state s ON s.network_id=n.id AND s.watcher_key='tron:shadow'
 LEFT JOIN LATERAL (SELECT created_at FROM sys_chain_watcher_block_history WHERE network_id=n.id AND watcher_key='tron:shadow' AND block_number=s.last_block ORDER BY created_at DESC LIMIT 1) h ON TRUE
 LEFT JOIN sys_crypto_network_runtime_status rs ON rs.network_id=n.id
 WHERE n.id=$1 AND n.is_active AND NOT n.deletion_mark`, network).Scan(
		&state.Family, &state.Mode, &state.ActivatedAt, &state.Paused, &state.Version, &state.LeaseActive, &state.CheckpointHeight, &state.LastProgressAt, &state.WorkerObservedAt, &state.LastErrorCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, apperror.NewNotFound("blockchain_network", network.String())
	}
	if err != nil {
		return state, fmt.Errorf("read shadow control: %w", err)
	}
	return state, nil
}

func (r *ShadowControlRepo) SetShadowPaused(ctx context.Context, network id.ID, version int, paused bool) (crypto.ShadowControlState, error) {
	// CaptureBlock and financial activation lock this same row. Once pause
	// commits, no later shadow capture can advance its checkpoint.
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var mode string
	var activated bool
	var currentPaused bool
	var current int
	err := q.QueryRow(ctx, `SELECT mode,activated_at IS NOT NULL,shadow_control_version,shadow_paused FROM sys_crypto_block_ingestion WHERE network_id=$1 FOR UPDATE`, network).Scan(&mode, &activated, &current, &currentPaused)
	if err != nil {
		return crypto.ShadowControlState{}, fmt.Errorf("lock shadow control: %w", err)
	}
	if activated || mode != "shadow" {
		return crypto.ShadowControlState{}, apperror.NewValidation("financial ingestion cannot be changed by shadow controls")
	}
	if current != version {
		return crypto.ShadowControlState{}, apperror.NewConcurrentModification("crypto_shadow_control", network.String())
	}
	if currentPaused == paused {
		return r.GetShadowControl(ctx, network)
	}
	_, err = q.Exec(ctx, `UPDATE sys_crypto_block_ingestion SET shadow_paused=$2,shadow_control_version=shadow_control_version+1,updated_at=NOW() WHERE network_id=$1`, network, paused)
	if err != nil {
		return crypto.ShadowControlState{}, fmt.Errorf("update shadow control: %w", err)
	}
	return r.GetShadowControl(ctx, network)
}
