package crypto_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

const _watcherStateTable = "sys_chain_watcher_state"
const _watcherLeaseSeconds = 120

// WatcherStateRepo persists family-neutral watcher checkpoints.
type WatcherStateRepo struct {
	builder squirrel.StatementBuilderType
}

// NewWatcherStateRepo creates a new watcher state repository.
func NewWatcherStateRepo() *WatcherStateRepo {
	return &WatcherStateRepo{
		builder: squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}

// Get returns the watcher state for a network watcher stream.
func (r *WatcherStateRepo) Get(ctx context.Context, networkID id.ID, watcherKey string) (*crypto.WatcherState, error) {
	q := r.builder.Select("network_id", "watcher_key", "last_block", "last_timestamp", "fingerprint", "updated_at", "COALESCE(lease_owner,'') AS lease_owner", "fencing_token").
		From(_watcherStateTable).
		Where(squirrel.Eq{"network_id": networkID, "watcher_key": watcherKey}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	var state crypto.WatcherState
	if err := pgxscan.Get(ctx, querier, &state, sql, args...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, crypto.ErrWatcherStateNotFound
		}
		return nil, fmt.Errorf("get watcher state: %w", err)
	}

	return &state, nil
}

func (r *WatcherStateRepo) ListByNetwork(ctx context.Context, networkID id.ID) ([]crypto.WatcherState, error) {
	txm := postgres.MustGetTxManager(ctx)
	var states []crypto.WatcherState
	err := pgxscan.Select(ctx, txm.GetQuerier(ctx), &states, `
		SELECT network_id, watcher_key, last_block, last_timestamp, fingerprint, updated_at,
		       COALESCE(lease_owner,'') AS lease_owner,fencing_token
		FROM `+_watcherStateTable+`
		WHERE network_id = $1
		ORDER BY watcher_key
	`, networkID)
	if err != nil {
		return nil, fmt.Errorf("list watcher state: %w", err)
	}
	return states, nil
}

func (r *WatcherStateRepo) GetBlockFingerprint(ctx context.Context, networkID id.ID, watcherKey string, blockNumber int64) (crypto.WatcherBlockFingerprint, bool, error) {
	var block crypto.WatcherBlockFingerprint
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT block_number,block_hash,block_timestamp
		FROM sys_chain_watcher_block_history
		WHERE network_id=$1 AND watcher_key=$2 AND block_number=$3
	`, networkID, watcherKey, blockNumber).Scan(&block.BlockNumber, &block.BlockHash, &block.Timestamp)
	if errors.Is(err, pgx.ErrNoRows) {
		return block, false, nil
	}
	if err != nil {
		return block, false, fmt.Errorf("get watcher block fingerprint: %w", err)
	}
	return block, true, nil
}

func (r *WatcherStateRepo) SaveBlockFingerprint(ctx context.Context, networkID id.ID, watcherKey string, block crypto.WatcherBlockFingerprint, retainFrom int64) error {
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		if _, err := q.Exec(txCtx, `
			INSERT INTO sys_chain_watcher_block_history (network_id,watcher_key,block_number,block_hash,block_timestamp)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (network_id,watcher_key,block_number) DO UPDATE
			SET block_hash=EXCLUDED.block_hash,block_timestamp=EXCLUDED.block_timestamp
		`, networkID, watcherKey, block.BlockNumber, block.BlockHash, block.Timestamp); err != nil {
			return fmt.Errorf("save watcher block fingerprint: %w", err)
		}
		if _, err := q.Exec(txCtx, `
			DELETE FROM sys_chain_watcher_block_history
			WHERE network_id=$1 AND watcher_key=$2 AND block_number < $3
		`, networkID, watcherKey, retainFrom); err != nil {
			return fmt.Errorf("prune watcher block fingerprints: %w", err)
		}
		return nil
	})
}

func (r *WatcherStateRepo) DeleteBlockFingerprintsAfter(ctx context.Context, networkID id.ID, watcherKey string, blockNumber int64) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		DELETE FROM sys_chain_watcher_block_history
		WHERE network_id=$1 AND watcher_key=$2 AND block_number > $3
	`, networkID, watcherKey, blockNumber)
	if err != nil {
		return fmt.Errorf("delete orphaned watcher block fingerprints: %w", err)
	}
	return nil
}

// Save advances a checkpoint only while the caller still owns its fencing token.
func (r *WatcherStateRepo) Save(ctx context.Context, state *crypto.WatcherState) error {
	return r.saveCheckpoint(ctx, state, nil)
}
func (r *WatcherStateRepo) CommitWindow(ctx context.Context, commit crypto.WatcherWindowCommit) error {
	state := commit.Candidate
	if commit.ExpectedBlock < 0 || commit.ExpectedTimestamp < 0 || state.LastBlock < commit.ExpectedBlock || state.LastTimestamp < commit.ExpectedTimestamp || state.Fingerprint != "" {
		return crypto.ErrWatcherCheckpointConflict
	}
	return r.saveCheckpoint(ctx, &state, &commit)
}
func (r *WatcherStateRepo) saveCheckpoint(ctx context.Context, state *crypto.WatcherState, expected *crypto.WatcherWindowCommit) error {
	if state.LeaseOwner == "" || state.FencingToken <= 0 {
		return crypto.ErrWatcherLeaseLost
	}
	var expectedBlock, expectedTimestamp int64
	if expected != nil {
		expectedBlock, expectedTimestamp = expected.ExpectedBlock, expected.ExpectedTimestamp
	}
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
 UPDATE sys_chain_watcher_state
 SET last_block=$4,last_timestamp=$5,fingerprint=$6,
     updated_at=CASE WHEN last_block<$4 OR last_timestamp<$5 THEN NOW() ELSE updated_at END,
     lease_expires_at=NOW()+($8 * INTERVAL '1 second')
 WHERE network_id=$1 AND watcher_key=$2 AND lease_owner=$3
   AND fencing_token=$7 AND lease_expires_at>NOW()
   AND last_block<=$4 AND last_timestamp<=$5
   AND (NOT $9::boolean OR (last_block=$10 AND last_timestamp=$11))
 RETURNING updated_at
 `, state.NetworkID, state.WatcherKey, state.LeaseOwner, state.LastBlock, state.LastTimestamp, state.Fingerprint, state.FencingToken, _watcherLeaseSeconds, expected != nil, expectedBlock, expectedTimestamp).Scan(&state.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		if expected != nil {
			return errors.Join(crypto.ErrWatcherLeaseLost, crypto.ErrWatcherCheckpointConflict)
		}
		return crypto.ErrWatcherLeaseLost
	}
	if err != nil {
		return fmt.Errorf("save watcher checkpoint: %w", err)
	}
	return nil
}

func (r *WatcherStateRepo) Acquire(ctx context.Context, candidate *crypto.WatcherState, owner string) (*crypto.WatcherState, error) {
	if candidate == nil || owner == "" {
		return nil, fmt.Errorf("watcher lease requires candidate and owner")
	}
	var state crypto.WatcherState
	err := pgxscan.Get(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), &state, `
		INSERT INTO sys_chain_watcher_state (
		    network_id,watcher_key,last_block,last_timestamp,fingerprint,
		    lease_owner,fencing_token,lease_expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,1,NOW()+($7 * INTERVAL '1 second'))
		ON CONFLICT (network_id,watcher_key) DO UPDATE
		SET lease_owner=EXCLUDED.lease_owner,
		    fencing_token=sys_chain_watcher_state.fencing_token+1,
		    lease_expires_at=EXCLUDED.lease_expires_at
		WHERE sys_chain_watcher_state.lease_expires_at IS NULL
		   OR sys_chain_watcher_state.lease_expires_at<=NOW()
		RETURNING network_id,watcher_key,last_block,last_timestamp,fingerprint,updated_at,
		          lease_owner,fencing_token
	`, candidate.NetworkID, candidate.WatcherKey, candidate.LastBlock,
		candidate.LastTimestamp, candidate.Fingerprint, owner, _watcherLeaseSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, crypto.ErrWatcherLeaseHeld
	}
	if err != nil {
		return nil, fmt.Errorf("acquire watcher lease: %w", err)
	}
	return &state, nil
}

func (r *WatcherStateRepo) Renew(ctx context.Context, state *crypto.WatcherState) error {
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE sys_chain_watcher_state
		SET lease_expires_at=NOW()+($5 * INTERVAL '1 second')
		WHERE network_id=$1 AND watcher_key=$2 AND lease_owner=$3
		  AND fencing_token=$4 AND lease_expires_at>NOW()
	`, state.NetworkID, state.WatcherKey, state.LeaseOwner, state.FencingToken, _watcherLeaseSeconds)
	if err != nil {
		return fmt.Errorf("renew watcher lease: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return crypto.ErrWatcherLeaseLost
	}
	return nil
}

func (r *WatcherStateRepo) Release(ctx context.Context, state *crypto.WatcherState) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE sys_chain_watcher_state SET lease_owner=NULL,lease_expires_at=NULL
		WHERE network_id=$1 AND watcher_key=$2 AND lease_owner=$3 AND fencing_token=$4
	`, state.NetworkID, state.WatcherKey, state.LeaseOwner, state.FencingToken)
	return err
}

func (r *WatcherStateRepo) Rewind(ctx context.Context, state *crypto.WatcherState, evidence string) error {
	if evidence == "" {
		return fmt.Errorf("watcher rewind requires common ancestor evidence")
	}
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		var oldBlock int64
		err := q.QueryRow(txCtx, `SELECT last_block FROM sys_chain_watcher_state
			WHERE network_id=$1 AND watcher_key=$2 AND lease_owner=$3
			  AND fencing_token=$4 AND lease_expires_at>NOW() AND last_block>$5
			FOR UPDATE`, state.NetworkID, state.WatcherKey, state.LeaseOwner,
			state.FencingToken, state.LastBlock).Scan(&oldBlock)
		if errors.Is(err, pgx.ErrNoRows) {
			return crypto.ErrWatcherLeaseLost
		}
		if err != nil {
			return fmt.Errorf("lock watcher checkpoint for rewind: %w", err)
		}
		tag, err := q.Exec(txCtx, `
			UPDATE sys_chain_watcher_state
			SET last_block=$5,last_timestamp=$6,fingerprint=$7,updated_at=NOW(),
			    lease_expires_at=NOW()+($8 * INTERVAL '1 second')
			WHERE network_id=$1 AND watcher_key=$2 AND lease_owner=$3
			  AND fencing_token=$4 AND lease_expires_at>NOW() AND last_block>$5
		`, state.NetworkID, state.WatcherKey, state.LeaseOwner, state.FencingToken,
			state.LastBlock, state.LastTimestamp, state.Fingerprint, _watcherLeaseSeconds)
		if err != nil {
			return fmt.Errorf("rewind watcher checkpoint: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return crypto.ErrWatcherLeaseLost
		}
		_, err = q.Exec(txCtx, `INSERT INTO sys_chain_watcher_rewinds
			(network_id,watcher_key,fencing_token,old_block,new_block,evidence)
			VALUES ($1,$2,$3,$4,$5,$6)`, state.NetworkID, state.WatcherKey,
			state.FencingToken, oldBlock, state.LastBlock, evidence)
		return err
	})
}

var _ crypto.WatcherStateRepository = (*WatcherStateRepo)(nil)
var _ crypto.WatcherLeaseRepository = (*WatcherStateRepo)(nil)
var _ crypto.WatcherBlockHistoryRepository = (*WatcherStateRepo)(nil)

func (r *WatcherStateRepo) CommitBlock(ctx context.Context, commit crypto.WatcherBlockCommit) error {
	state := commit.Candidate
	if state.LeaseOwner == "" || state.FencingToken <= 0 {
		return crypto.ErrWatcherLeaseLost
	}
	if state.LastBlock != commit.ExpectedBlock+1 || commit.Block.BlockNumber != state.LastBlock || commit.Block.BlockHash != state.Fingerprint || commit.Block.Timestamp != state.LastTimestamp || state.Fingerprint == "" || commit.RetainFrom < 0 || commit.RetainFrom > state.LastBlock {
		return crypto.ErrWatcherCheckpointConflict
	}
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		tag, err := txm.GetQuerier(txCtx).Exec(txCtx, `
   UPDATE sys_chain_watcher_state
   SET last_block=$4,last_timestamp=$5,fingerprint=$6,updated_at=NOW(),
       lease_expires_at=NOW()+($8 * INTERVAL '1 second')
   WHERE network_id=$1 AND watcher_key=$2 AND lease_owner=$3
     AND fencing_token=$7 AND lease_expires_at>NOW()
     AND last_block=$9 AND fingerprint=$10 AND last_timestamp<=$5
  `, state.NetworkID, state.WatcherKey, state.LeaseOwner, state.LastBlock, state.LastTimestamp, state.Fingerprint, state.FencingToken, _watcherLeaseSeconds, commit.ExpectedBlock, commit.ExpectedHash)
		if err != nil {
			return fmt.Errorf("commit watcher checkpoint: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return errors.Join(crypto.ErrWatcherLeaseLost, crypto.ErrWatcherCheckpointConflict)
		}
		return r.SaveBlockFingerprint(txCtx, state.NetworkID, state.WatcherKey, commit.Block, commit.RetainFrom)
	})
}

var _ crypto.WatcherBlockCommitter = (*WatcherStateRepo)(nil)

// updated_at is checkpoint progress, never lease acquisition/renewal or an
// unchanged save. Block streams additionally retain their committed history time.
func (r *WatcherStateRepo) ListIngestionCheckpoints(ctx context.Context, networkID id.ID) ([]crypto.IngestionCheckpoint, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `
  SELECT s.network_id,s.watcher_key,s.last_block,s.last_timestamp,s.fingerprint,s.updated_at,
   COALESCE(s.lease_owner,''),s.fencing_token,COALESCE(h.created_at,s.updated_at),s.lease_expires_at
  FROM sys_chain_watcher_state s
  LEFT JOIN sys_chain_watcher_block_history h
   ON h.network_id=s.network_id AND h.watcher_key=s.watcher_key AND h.block_number=s.last_block
  WHERE s.network_id=$1 ORDER BY s.watcher_key`, networkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []crypto.IngestionCheckpoint
	for rows.Next() {
		var c crypto.IngestionCheckpoint
		err = rows.Scan(&c.State.NetworkID, &c.State.WatcherKey, &c.State.LastBlock, &c.State.LastTimestamp, &c.State.Fingerprint, &c.State.UpdatedAt, &c.State.LeaseOwner, &c.State.FencingToken, &c.LastProgressAt, &c.LeaseUntil)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

var _ crypto.WatcherWindowCommitter = (*WatcherStateRepo)(nil)
