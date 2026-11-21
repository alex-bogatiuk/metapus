package crypto_repo

import (
	"context"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

// ListIngestionBacklogs excludes completed history from both queue count and age.
// The partial inbox index covers pending/processing; needs_review is also visible.
func (r *ChainEventInboxRepo) ListIngestionBacklogs(ctx context.Context) (map[id.ID]crypto.IngestionInboxBacklog, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `
	 SELECT network_id,
	  count(*) FILTER (WHERE status='pending'),
	  count(*) FILTER (WHERE status='processing'),
	  count(*) FILTER (WHERE status='needs_review'),
	  COALESCE(GREATEST(extract(epoch FROM clock_timestamp()-min(received_at) FILTER (WHERE status IN ('pending','processing'))),0)::bigint,0)
	 FROM reg_crypto_chain_observations WHERE status IN ('pending','processing','needs_review')
	 GROUP BY network_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[id.ID]crypto.IngestionInboxBacklog)
	for rows.Next() {
		var network id.ID
		var backlog crypto.IngestionInboxBacklog
		if err := rows.Scan(&network, &backlog.Pending, &backlog.Processing, &backlog.NeedsReview, &backlog.OldestAgeSeconds); err != nil {
			return nil, err
		}
		result[network] = backlog
	}
	return result, rows.Err()
}
