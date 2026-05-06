package crypto_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/infrastructure/blockchain/tron"
	"metapus/internal/infrastructure/storage/postgres"
)

const _watcherStateTable = "sys_chain_watcher_state"

// WatcherStateRepo implements tron.WatcherStateRepository.
type WatcherStateRepo struct {
	builder squirrel.StatementBuilderType
}

// NewWatcherStateRepo creates a new watcher state repository.
func NewWatcherStateRepo() *WatcherStateRepo {
	return &WatcherStateRepo{
		builder: squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}

// Get returns the watcher state for a network.
func (r *WatcherStateRepo) Get(ctx context.Context, networkID id.ID) (*tron.WatcherState, error) {
	q := r.builder.Select("network_id", "last_block", "last_timestamp", "fingerprint", "updated_at").
		From(_watcherStateTable).
		Where(squirrel.Eq{"network_id": networkID}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	var state tron.WatcherState
	if err := pgxscan.Get(ctx, querier, &state, sql, args...); err != nil {
		return nil, fmt.Errorf("get watcher state: %w", err)
	}

	return &state, nil
}

// Save upserts the watcher state checkpoint.
func (r *WatcherStateRepo) Save(ctx context.Context, state *tron.WatcherState) error {
	state.UpdatedAt = time.Now().UTC()

	q := r.builder.Insert(_watcherStateTable).
		Columns("network_id", "last_block", "last_timestamp", "fingerprint", "updated_at").
		Values(state.NetworkID, state.LastBlock, state.LastTimestamp, state.Fingerprint, state.UpdatedAt).
		Suffix("ON CONFLICT (network_id) DO UPDATE SET last_block = EXCLUDED.last_block, last_timestamp = EXCLUDED.last_timestamp, fingerprint = EXCLUDED.fingerprint, updated_at = EXCLUDED.updated_at")

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	if _, err := querier.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("save watcher state: %w", err)
	}

	return nil
}
