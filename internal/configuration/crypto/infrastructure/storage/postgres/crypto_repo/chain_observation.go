package crypto_repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/infrastructure/storage/postgres"
	platformtelemetry "metapus/internal/platform/telemetry"
)

// ChainObservation preserves each distinct watcher observation before the
// watcher may acknowledge a block. Processing state is separate from evidence.
type ChainObservation struct {
	ID            id.ID           `db:"id"`
	NetworkID     id.ID           `db:"network_id"`
	Payload       json.RawMessage `db:"payload"`
	Traceparent   string          `db:"traceparent"`
	CorrelationID string          `db:"correlation_id"`
	CausationID   string          `db:"causation_id"`
	Attempts      int             `db:"attempts"`
}

type ChainObservationRepo struct{}

func NewChainObservationRepo() *ChainObservationRepo { return &ChainObservationRepo{} }

func (r *ChainObservationRepo) Capture(ctx context.Context, event crypto.BlockchainEvent) error {
	return r.capture(ctx, event, false)
}

func (r *ChainObservationRepo) CaptureShadow(ctx context.Context, event crypto.BlockchainEvent) error {
	event.ObservationMode = "shadow"
	return r.capture(ctx, event, true)
}

func (r *ChainObservationRepo) capture(ctx context.Context, event crypto.BlockchainEvent, shadow bool) error {
	if id.IsNil(event.NetworkID) || strings.TrimSpace(event.TxHash) == "" {
		return fmt.Errorf("chain observation requires network and transaction hash")
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode chain observation: %w", err)
	}
	hash := sha256.Sum256(payload)
	stream := string(event.EffectiveEventKind()) + ":" + strings.ToLower(event.TokenContract)
	status := "pending"
	if shadow {
		stream = "shadow:" + stream
		status = "processed"
	}
	_, err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		INSERT INTO reg_crypto_chain_observations (
		    network_id,source_stream,tx_hash,event_kind,event_index,
		    block_number,block_hash,payload_hash,payload,traceparent,correlation_id,causation_id,status,processed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12,$13,CASE WHEN $13='processed' THEN NOW() END)
		ON CONFLICT (network_id,payload_hash) DO NOTHING
	`, event.NetworkID, stream, event.TxHash, event.EffectiveEventKind(), event.EventIndex,
		event.BlockNumber, event.BlockHash, hex.EncodeToString(hash[:]), string(payload),
		platformtelemetry.Traceparent(ctx),
		fmt.Sprintf("%s:%s:%s:%d", event.NetworkID, event.TxHash, event.EffectiveEventKind(), event.EventIndex),
		event.BlockHash, status)
	if err != nil {
		return fmt.Errorf("capture chain observation: %w", err)
	}
	return nil
}

func (r *ChainObservationRepo) Claim(ctx context.Context, owner string, limit int, lease time.Duration) ([]ChainObservation, error) {
	if owner == "" || limit <= 0 || lease <= 0 {
		return nil, fmt.Errorf("observation claim requires owner, limit and lease")
	}
	var rows []ChainObservation
	err := pgxscan.Select(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), &rows, `
		UPDATE reg_crypto_chain_observations observation
		SET status='processing',attempts=observation.attempts+1,
		    lease_owner=$1,lease_expires_at=NOW()+$3::interval
		WHERE observation.id IN (
		    SELECT id FROM reg_crypto_chain_observations
		    WHERE (status='pending' AND next_attempt_at<=NOW())
		       OR (status='processing' AND lease_expires_at<NOW())
		    ORDER BY next_attempt_at,id
		    FOR UPDATE SKIP LOCKED LIMIT $2
		)
		RETURNING observation.id,observation.network_id,observation.payload,
		          observation.traceparent,observation.correlation_id,observation.causation_id,
		          observation.attempts
	`, owner, limit, fmt.Sprintf("%d seconds", int(lease.Seconds())))
	if err != nil {
		return nil, fmt.Errorf("claim chain observations: %w", err)
	}
	return rows, nil
}

func (r *ChainObservationRepo) Complete(ctx context.Context, observationID id.ID, owner string) error {
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE reg_crypto_chain_observations
		SET status='processed',processed_at=NOW(),lease_owner=NULL,lease_expires_at=NULL,last_error=''
		WHERE id=$1 AND status='processing' AND lease_owner=$2 AND lease_expires_at>NOW()
	`, observationID, owner)
	if err != nil {
		return fmt.Errorf("complete chain observation: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("chain observation lease lost")
	}
	return nil
}

// Waiting for RPC evidence is an operational retry, not a failed business
// attempt. The claim increment is undone and raw evidence remains immutable.
func (r *ChainObservationRepo) DeferVerification(ctx context.Context, observationID id.ID, owner string) error {
	return r.deferObservation(ctx, observationID, owner, "verification_pending")
}

func (r *ChainObservationRepo) DeferExternalOutflow(ctx context.Context, observationID id.ID, owner string) error {
	return r.deferObservation(ctx, observationID, owner, "external_outflow_busy")
}

func (*ChainObservationRepo) deferObservation(ctx context.Context, observationID id.ID, owner, reason string) error {
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_chain_observations
 SET status='pending',attempts=GREATEST(attempts-1,0),next_attempt_at=NOW()+INTERVAL '5 seconds',lease_owner=NULL,lease_expires_at=NULL,last_error=$3
 WHERE id=$1 AND status='processing' AND lease_owner=$2 AND lease_expires_at>NOW()`, observationID, owner, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("chain observation lease lost")
	}
	return nil
}

// Fail returns true once the observation is exhausted and needs review.
func (r *ChainObservationRepo) Fail(ctx context.Context, observationID id.ID, owner string, processErr error) (bool, error) {
	message := "unknown processing error"
	if processErr != nil {
		message = processErr.Error()
	}
	if len(message) > 1000 {
		message = message[:1000]
	}
	var needsReview bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		UPDATE reg_crypto_chain_observations
		SET status=CASE WHEN attempts>=8 THEN 'needs_review' ELSE 'pending' END,
		    next_attempt_at=NOW() + (LEAST(300,5 * (1 << LEAST(attempts,6))) + FLOOR(RANDOM()*3)) * INTERVAL '1 second',
		    lease_owner=NULL,lease_expires_at=NULL,last_error=$3
		WHERE id=$1 AND status='processing' AND lease_owner=$2 AND lease_expires_at>NOW()
		RETURNING status='needs_review'
	`, observationID, owner, message).Scan(&needsReview)
	if err != nil {
		return false, fmt.Errorf("release failed chain observation: %w", err)
	}
	return needsReview, nil
}
