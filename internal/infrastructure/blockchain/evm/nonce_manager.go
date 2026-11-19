package evm

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

var (
	ErrNonceSourceHeld    = errors.New("EVM nonce source is held")
	ErrNonceLeaseConflict = errors.New("EVM nonce lease conflicts with business intent")
	ErrNonceGenerationGap = errors.New("EVM nonce replacement generation is not consecutive")
	ErrExternalNonceDrift = errors.New("unexpected external EVM nonce drift")
)

type NonceRequest struct {
	NetworkID     id.ID
	SourceAddress string
	BasisType     string
	BasisID       id.ID
	Purpose       crypto.SigningPurpose
	Attempt       int
	Generation    int
	PendingNonce  uint64
}

type NonceLease struct {
	ID         id.ID
	Nonce      uint64
	Generation int
	Status     string
}

type NonceManager interface {
	Reserve(ctx context.Context, request NonceRequest) (NonceLease, error)
	MarkSigned(ctx context.Context, networkID id.ID, sourceAddress string, nonce uint64, generation int, txHash string) error
	MarkBroadcast(ctx context.Context, networkID id.ID, sourceAddress string, nonce uint64, generation int, txHash string) error
	MarkMined(ctx context.Context, networkID id.ID, txHash string, financiallyFinal bool) error
	HoldSource(ctx context.Context, networkID id.ID, sourceAddress, reason string) error
}

func (m *PostgresNonceManager) MarkSigned(ctx context.Context, networkID id.ID, sourceAddress string, nonce uint64, generation int, txHash string) error {
	normalized, err := NormalizeAddress(sourceAddress)
	if err != nil || id.IsNil(networkID) || strings.TrimSpace(txHash) == "" || generation <= 0 {
		return errors.New("invalid EVM signed nonce evidence")
	}
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		var leaseID id.ID
		var from string
		var currentGeneration int
		if err := q.QueryRow(txCtx, `
			SELECT id,status,current_generation FROM reg_evm_nonce_leases
			WHERE network_id=$1 AND source_address=$2 AND nonce=$3
			FOR UPDATE
		`, networkID, normalized, strconv.FormatUint(nonce, 10)).Scan(&leaseID, &from, &currentGeneration); err != nil {
			return fmt.Errorf("lock signed EVM nonce lease: %w", err)
		}
		if currentGeneration != generation {
			return fmt.Errorf("%w: signed generation=%d current=%d", ErrNonceLeaseConflict, generation, currentGeneration)
		}
		if err := rejectDifferentGenerationHash(txCtx, q, leaseID, generation, txHash); err != nil {
			return err
		}
		if from == "mined" || from == "finalized" || from == "broadcast" || from == "signed" {
			return nil
		}
		if from != "reserved" {
			return fmt.Errorf("%w: cannot mark lease signed from status %s", ErrNonceLeaseConflict, from)
		}
		if _, err := q.Exec(txCtx, `UPDATE reg_evm_nonce_leases SET status='signed',signed_at=COALESCE(signed_at,NOW()),updated_at=NOW() WHERE id=$1`, leaseID); err != nil {
			return err
		}
		return appendNonceEvent(txCtx, q, leaseID, generation, from, "signed", txHash, 0, nil)
	})
}

type ProviderNonceSnapshot struct {
	Latest       uint64
	Pending      uint64
	Observations int
	Divergent    bool
}

type NonceStartupObserver interface {
	ObserveNonceState(ctx context.Context, sourceAddress string) (ProviderNonceSnapshot, error)
	AssessReplacement(ctx context.Context, sourceAddress, networkSequence string, txHashes []string) (crypto.ReplacementAssessment, error)
}

type NonceDivergence struct {
	SourceAddress string
	Reason        string
	NextNonce     string
}

// NonceProviderDisagreement is the hold reason for providers that returned
// different nonce state. It is the only hold that startup re-verification may
// release: the other reasons describe chain or DB evidence, not observation.
const NonceProviderDisagreement = "RPC providers disagree on latest or pending nonce"

// VerifiedNonceHold is a cursor held for provider disagreement whose state a
// later fresh quorum observed as consistent with the DB and its leases.
type VerifiedNonceHold struct {
	SourceAddress string
	NextNonce     string
}

type NonceStartupReport struct {
	Divergences []NonceDivergence
	Verified    []VerifiedNonceHold
}

type StartupNonceManager interface {
	ReconcileStartup(ctx context.Context, networkID id.ID, observer NonceStartupObserver) ([]NonceDivergence, error)
}

// StartupNonceRecoverer releases provider-disagreement holds after the same
// startup checks pass again.
type StartupNonceRecoverer interface {
	ReconcileStartupReport(ctx context.Context, networkID id.ID, observer NonceStartupObserver) (NonceStartupReport, error)
	ReleaseVerifiedHold(ctx context.Context, networkID id.ID, hold VerifiedNonceHold) error
}

type UnsignedTailNonceManager interface {
	ReclaimUnsignedTail(ctx context.Context, request NonceRequest) error
}

type PostgresNonceManager struct{}

func NewPostgresNonceManager() *PostgresNonceManager { return &PostgresNonceManager{} }

func (m *PostgresNonceManager) Reserve(ctx context.Context, request NonceRequest) (NonceLease, error) {
	var lease NonceLease
	normalized, err := NormalizeAddress(request.SourceAddress)
	if err != nil || id.IsNil(request.NetworkID) || id.IsNil(request.BasisID) || strings.TrimSpace(request.BasisType) == "" ||
		!request.Purpose.Valid() || request.Attempt <= 0 || request.Generation <= 0 {
		return lease, errors.New("invalid EVM nonce request")
	}
	request.SourceAddress = normalized
	txm := postgres.MustGetTxManager(ctx)
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		if _, err := q.Exec(txCtx, `
			INSERT INTO reg_evm_nonce_cursors (network_id,source_address,next_nonce)
			VALUES ($1,$2,$3)
			ON CONFLICT (network_id,source_address) DO NOTHING
		`, request.NetworkID, request.SourceAddress, strconv.FormatUint(request.PendingNonce, 10)); err != nil {
			return fmt.Errorf("initialize EVM nonce cursor: %w", err)
		}
		var nextEncoded, holdReason string
		var held bool
		if err := q.QueryRow(txCtx, `
			SELECT next_nonce::text,held,hold_reason
			FROM reg_evm_nonce_cursors
			WHERE network_id=$1 AND source_address=$2
			FOR UPDATE
		`, request.NetworkID, request.SourceAddress).Scan(&nextEncoded, &held, &holdReason); err != nil {
			return fmt.Errorf("lock EVM nonce cursor: %w", err)
		}
		if held {
			return fmt.Errorf("%w: %s", ErrNonceSourceHeld, holdReason)
		}
		var existingNonce string
		var existingGeneration int
		var existingSource, existingStatus string
		var existingID id.ID
		err := q.QueryRow(txCtx, `
			SELECT id,source_address,nonce::text,current_generation,status
			FROM reg_evm_nonce_leases
			WHERE basis_type=$1 AND basis_id=$2 AND purpose=$3 AND attempt=$4
			FOR UPDATE
		`, request.BasisType, request.BasisID, request.Purpose, request.Attempt).Scan(
			&existingID, &existingSource, &existingNonce, &existingGeneration, &existingStatus,
		)
		if err == nil {
			if existingSource != request.SourceAddress {
				return ErrNonceLeaseConflict
			}
			nonce, parseErr := strconv.ParseUint(existingNonce, 10, 64)
			if parseErr != nil {
				return fmt.Errorf("decode persisted EVM nonce: %w", parseErr)
			}
			if request.Generation > existingGeneration+1 {
				return ErrNonceGenerationGap
			}
			if request.Generation < existingGeneration {
				return ErrNonceLeaseConflict
			}
			if request.Generation == existingGeneration+1 {
				if existingStatus != "broadcast" && existingStatus != "signed" && existingStatus != "held" {
					return fmt.Errorf("%w: cannot replace lease in status %s", ErrNonceLeaseConflict, existingStatus)
				}
				if _, err := q.Exec(txCtx, `
					UPDATE reg_evm_nonce_leases
					SET current_generation=$2,status='reserved',hold_reason='',updated_at=NOW()
					WHERE id=$1
				`, existingID, request.Generation); err != nil {
					return fmt.Errorf("advance EVM nonce generation: %w", err)
				}
				if err := appendNonceEvent(txCtx, q, existingID, request.Generation, existingStatus, "reserved", "", 0, map[string]any{"reason": "fee_replacement"}); err != nil {
					return err
				}
				existingGeneration, existingStatus = request.Generation, "reserved"
			}
			lease = NonceLease{ID: existingID, Nonce: nonce, Generation: existingGeneration, Status: existingStatus}
			return nil
		}
		if !isNoRows(err) {
			return fmt.Errorf("find EVM nonce lease: %w", err)
		}
		if request.Generation != 1 {
			return ErrNonceGenerationGap
		}
		nextNonce, err := strconv.ParseUint(nextEncoded, 10, 64)
		if err != nil {
			return fmt.Errorf("decode EVM nonce cursor: %w", err)
		}
		if request.PendingNonce > nextNonce {
			nextNonce = request.PendingNonce
		}
		lease = NonceLease{ID: id.New(), Nonce: nextNonce, Generation: request.Generation, Status: "reserved"}
		if _, err := q.Exec(txCtx, `
			INSERT INTO reg_evm_nonce_leases (
				id,network_id,source_address,nonce,basis_type,basis_id,purpose,attempt,current_generation,status
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'reserved')
		`, lease.ID, request.NetworkID, request.SourceAddress, strconv.FormatUint(lease.Nonce, 10), request.BasisType,
			request.BasisID, request.Purpose, request.Attempt, request.Generation); err != nil {
			return fmt.Errorf("insert EVM nonce lease: %w", err)
		}
		if _, err := q.Exec(txCtx, `
			UPDATE reg_evm_nonce_cursors SET next_nonce=$3,version=version+1,updated_at=NOW()
			WHERE network_id=$1 AND source_address=$2
		`, request.NetworkID, request.SourceAddress, strconv.FormatUint(nextNonce+1, 10)); err != nil {
			return fmt.Errorf("advance EVM nonce cursor: %w", err)
		}
		return appendNonceEvent(txCtx, q, lease.ID, request.Generation, "", "reserved", "", 0, nil)
	})
	return lease, err
}

func (m *PostgresNonceManager) MarkBroadcast(ctx context.Context, networkID id.ID, sourceAddress string, nonce uint64, generation int, txHash string) error {
	normalized, err := NormalizeAddress(sourceAddress)
	if err != nil || id.IsNil(networkID) || strings.TrimSpace(txHash) == "" || generation <= 0 {
		return errors.New("invalid EVM broadcast nonce evidence")
	}
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		var leaseID id.ID
		var from string
		var currentGeneration int
		if err := q.QueryRow(txCtx, `
			SELECT id,status,current_generation FROM reg_evm_nonce_leases
			WHERE network_id=$1 AND source_address=$2 AND nonce=$3
			FOR UPDATE
		`, networkID, normalized, strconv.FormatUint(nonce, 10)).Scan(&leaseID, &from, &currentGeneration); err != nil {
			return fmt.Errorf("lock broadcast EVM nonce lease: %w", err)
		}
		if currentGeneration != generation {
			return fmt.Errorf("%w: broadcast generation=%d current=%d", ErrNonceLeaseConflict, generation, currentGeneration)
		}
		if err := rejectDifferentGenerationHash(txCtx, q, leaseID, generation, txHash); err != nil {
			return err
		}
		if from == "mined" || from == "finalized" {
			return nil
		}
		if _, err := q.Exec(txCtx, `UPDATE reg_evm_nonce_leases SET status='broadcast',broadcast_at=COALESCE(broadcast_at,NOW()),updated_at=NOW() WHERE id=$1`, leaseID); err != nil {
			return err
		}
		return appendNonceEvent(txCtx, q, leaseID, generation, from, "broadcast", txHash, 0, nil)
	})
}

func rejectDifferentGenerationHash(ctx context.Context, q postgres.Querier, leaseID id.ID, generation int, txHash string) error {
	var conflict bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM reg_evm_nonce_lease_events
		WHERE lease_id=$1 AND generation=$2 AND tx_hash<>'' AND LOWER(tx_hash)<>LOWER($3)
	)`, leaseID, generation, txHash).Scan(&conflict); err != nil {
		return err
	}
	if conflict {
		return fmt.Errorf("%w: generation %d already owns another transaction hash", ErrNonceLeaseConflict, generation)
	}
	return nil
}

func (m *PostgresNonceManager) MarkMined(ctx context.Context, networkID id.ID, txHash string, financiallyFinal bool) error {
	if id.IsNil(networkID) || strings.TrimSpace(txHash) == "" {
		return errors.New("invalid EVM mined nonce evidence")
	}
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		var leaseID id.ID
		var generation int
		var from string
		if err := q.QueryRow(txCtx, `
			SELECT l.id,e.generation,l.status
			FROM reg_evm_nonce_lease_events e
			JOIN reg_evm_nonce_leases l ON l.id=e.lease_id
			WHERE l.network_id=$1 AND LOWER(e.tx_hash)=LOWER($2)
			ORDER BY e.created_at DESC LIMIT 1 FOR UPDATE OF l
		`, networkID, txHash).Scan(&leaseID, &generation, &from); err != nil {
			if isNoRows(err) {
				return nil
			}
			return err
		}
		to := "mined"
		column := "mined_at"
		if financiallyFinal {
			to, column = "finalized", "finalized_at"
		}
		if from == to || from == "finalized" {
			return nil
		}
		query := fmt.Sprintf("UPDATE reg_evm_nonce_leases SET status=$2,%s=COALESCE(%s,NOW()),updated_at=NOW() WHERE id=$1", column, column)
		if _, err := q.Exec(txCtx, query, leaseID, to); err != nil {
			return err
		}
		return appendNonceEvent(txCtx, q, leaseID, generation, from, to, txHash, 0, nil)
	})
}

func (m *PostgresNonceManager) HoldSource(ctx context.Context, networkID id.ID, sourceAddress, reason string) error {
	normalized, err := NormalizeAddress(sourceAddress)
	if err != nil || strings.TrimSpace(reason) == "" {
		return errors.New("invalid EVM nonce hold")
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	_, err = q.Exec(ctx, `
		UPDATE reg_evm_nonce_cursors SET held=TRUE,hold_reason=$3,version=version+1,updated_at=NOW()
		WHERE network_id=$1 AND source_address=$2
	`, networkID, normalized, reason)
	return err
}

// ReleaseVerifiedHold clears a provider-disagreement hold only while the
// cursor still has the verified next nonce and every lease is fenced below it.
func (m *PostgresNonceManager) ReleaseVerifiedHold(ctx context.Context, networkID id.ID, hold VerifiedNonceHold) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE reg_evm_nonce_cursors c SET held=FALSE,hold_reason='',version=version+1,updated_at=NOW()
		WHERE c.network_id=$1 AND c.source_address=$2 AND c.next_nonce=$3::numeric
		  AND c.held AND c.hold_reason=$4
		  AND NOT EXISTS(SELECT 1 FROM reg_evm_nonce_leases l
		      WHERE l.network_id=c.network_id AND l.source_address=c.source_address AND l.nonce>=c.next_nonce)
	`, networkID, hold.SourceAddress, hold.NextNonce, NonceProviderDisagreement)
	if err != nil {
		return fmt.Errorf("release verified EVM nonce hold: %w", err)
	}
	return nil
}

func (m *PostgresNonceManager) ReconcileStartup(ctx context.Context, networkID id.ID, observer NonceStartupObserver) ([]NonceDivergence, error) {
	report, err := m.ReconcileStartupReport(ctx, networkID, observer)
	return report.Divergences, err
}

func (m *PostgresNonceManager) ReconcileStartupReport(ctx context.Context, networkID id.ID, observer NonceStartupObserver) (report NonceStartupReport, err error) {
	divergences, verified, err := m.reconcileStartup(ctx, networkID, observer)
	return NonceStartupReport{Divergences: divergences, Verified: verified}, err
}

func (m *PostgresNonceManager) reconcileStartup(ctx context.Context, networkID id.ID, observer NonceStartupObserver) ([]NonceDivergence, []VerifiedNonceHold, error) {
	if id.IsNil(networkID) || observer == nil {
		return nil, nil, errors.New("EVM startup nonce reconciliation requires network and observer")
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `
		SELECT source_address,next_nonce::text,held,hold_reason
		FROM reg_evm_nonce_cursors WHERE network_id=$1 ORDER BY source_address
	`, networkID)
	if err != nil {
		return nil, nil, fmt.Errorf("list EVM nonce cursors: %w", err)
	}
	defer rows.Close()
	type cursor struct {
		source, next, holdReason string
		held                     bool
	}
	var cursors []cursor
	for rows.Next() {
		var value cursor
		if err := rows.Scan(&value.source, &value.next, &value.held, &value.holdReason); err != nil {
			return nil, nil, err
		}
		cursors = append(cursors, value)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var divergences []NonceDivergence
	var verified []VerifiedNonceHold
	for _, cursor := range cursors {
		before := len(divergences)
		next, err := strconv.ParseUint(cursor.next, 10, 64)
		if err != nil {
			divergences = append(divergences, NonceDivergence{SourceAddress: cursor.source, Reason: "database nonce cursor exceeds uint64"})
			continue
		}
		snapshot, err := observer.ObserveNonceState(ctx, cursor.source)
		if err != nil {
			return divergences, nil, fmt.Errorf("observe EVM nonce state for %s: %w", cursor.source, err)
		}
		// An active worker may reserve while RPC is read. Such a changed
		// snapshot is retryable, not evidence of an unfenced/consumed nonce.
		stable := func() error {
			var current string
			if err := q.QueryRow(ctx, "SELECT next_nonce::text FROM reg_evm_nonce_cursors WHERE network_id=$1 AND source_address=$2", networkID, cursor.source).Scan(&current); err != nil {
				return err
			}
			if current != cursor.next {
				return errors.New("startup nonce state changed during observation; retry")
			}
			return nil
		}
		if err := stable(); err != nil {
			return divergences, nil, err
		}
		if snapshot.Divergent {
			divergences = append(divergences, NonceDivergence{SourceAddress: cursor.source, Reason: NonceProviderDisagreement, NextNonce: cursor.next})
			continue
		}
		if snapshot.Pending > next || snapshot.Latest > next {
			divergences = append(divergences, NonceDivergence{SourceAddress: cursor.source,
				Reason: fmt.Sprintf("chain nonce advanced beyond DB cursor: latest=%d pending=%d db_next=%d", snapshot.Latest, snapshot.Pending, next)})
			continue
		}
		leases, err := m.loadStartupLeases(ctx, q, networkID, cursor.source)
		if err != nil {
			return divergences, nil, err
		}
		if err := stable(); err != nil {
			return divergences, nil, err
		}
		for _, lease := range leases {
			if lease.Nonce >= next {
				divergences = append(divergences, NonceDivergence{SourceAddress: cursor.source,
					Reason: fmt.Sprintf("DB cursor %d does not fence leased nonce %d", next, lease.Nonce)})
				break
			}
			if len(lease.TxHashes) == 0 {
				if snapshot.Pending > lease.Nonce && lease.Status != "mined" && lease.Status != "finalized" {
					divergences = append(divergences, NonceDivergence{SourceAddress: cursor.source,
						Reason: fmt.Sprintf("nonce %d was consumed without a persisted transaction hash", lease.Nonce)})
					break
				}
				continue
			}
			assessment, err := observer.AssessReplacement(ctx, cursor.source, strconv.FormatUint(lease.Nonce, 10), lease.TxHashes)
			if err != nil {
				return divergences, nil, fmt.Errorf("assess startup nonce %d: %w", lease.Nonce, err)
			}
			switch assessment.Disposition {
			case crypto.ReplacementNonceConsumed:
				divergences = append(divergences, NonceDivergence{SourceAddress: cursor.source, Reason: assessment.Reason})
			case crypto.ReplacementUncertain:
				if strings.Contains(strings.ToLower(assessment.Reason), "disagree") || strings.Contains(strings.ToLower(assessment.Reason), "different mined") {
					divergences = append(divergences, NonceDivergence{SourceAddress: cursor.source, Reason: assessment.Reason})
				} else {
					return divergences, nil, fmt.Errorf("startup nonce evidence is uncertain: %s", assessment.Reason)
				}
			}
			if len(divergences) > 0 && divergences[len(divergences)-1].SourceAddress == cursor.source {
				break
			}
		}
		if len(divergences) == before && cursor.held && cursor.holdReason == NonceProviderDisagreement {
			verified = append(verified, VerifiedNonceHold{SourceAddress: cursor.source, NextNonce: cursor.next})
		}
	}
	for _, divergence := range divergences {
		recordNonceDivergence(ctx, networkID, divergence.Reason)
	}
	return divergences, verified, nil
}

func (m *PostgresNonceManager) ReclaimUnsignedTail(ctx context.Context, request NonceRequest) error {
	normalized, err := NormalizeAddress(request.SourceAddress)
	if err != nil || id.IsNil(request.NetworkID) || id.IsNil(request.BasisID) || request.BasisType == "" || !request.Purpose.Valid() || request.Attempt <= 0 {
		return errors.New("invalid unsigned EVM nonce reclaim request")
	}
	request.SourceAddress = normalized
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		var leaseID id.ID
		var nonceEncoded, status string
		var generation int
		err := q.QueryRow(txCtx, `SELECT id,nonce::text,current_generation,status
			FROM reg_evm_nonce_leases
			WHERE network_id=$1 AND source_address=$2 AND basis_type=$3 AND basis_id=$4 AND purpose=$5 AND attempt=$6
			FOR UPDATE`, request.NetworkID, request.SourceAddress, request.BasisType, request.BasisID, request.Purpose, request.Attempt).
			Scan(&leaseID, &nonceEncoded, &generation, &status)
		if isNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if generation != 1 || status != "reserved" {
			return fmt.Errorf("EVM nonce lease is not an unsigned initial generation")
		}
		var authorizationExists bool
		if err := q.QueryRow(txCtx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations
			WHERE basis_type=$1 AND basis_id=$2 AND purpose=$3 AND attempt=$4)`, request.BasisType, request.BasisID, request.Purpose, request.Attempt).
			Scan(&authorizationExists); err != nil {
			return err
		}
		if authorizationExists {
			return fmt.Errorf("EVM nonce cannot be reclaimed after signing authorization persistence")
		}
		nonce, err := strconv.ParseUint(nonceEncoded, 10, 64)
		if err != nil {
			return err
		}
		var nextEncoded string
		if err := q.QueryRow(txCtx, `SELECT next_nonce::text FROM reg_evm_nonce_cursors
			WHERE network_id=$1 AND source_address=$2 FOR UPDATE`, request.NetworkID, request.SourceAddress).Scan(&nextEncoded); err != nil {
			return err
		}
		next, err := strconv.ParseUint(nextEncoded, 10, 64)
		if err != nil || next != nonce+1 {
			return fmt.Errorf("EVM nonce lease is not the cursor tail")
		}
		var laterExists bool
		if err := q.QueryRow(txCtx, `SELECT EXISTS(SELECT 1 FROM reg_evm_nonce_leases
			WHERE network_id=$1 AND source_address=$2 AND nonce>$3)`, request.NetworkID, request.SourceAddress, nonceEncoded).Scan(&laterExists); err != nil {
			return err
		}
		if laterExists {
			return fmt.Errorf("EVM nonce lease has a later owner")
		}
		if err := appendNonceEvent(txCtx, q, leaseID, generation, status, "reclaimed", "", 0, map[string]any{"reason": "unsigned_tail_reclaim"}); err != nil {
			return err
		}
		if tag, err := q.Exec(txCtx, `DELETE FROM reg_evm_nonce_leases WHERE id=$1 AND status='reserved'`, leaseID); err != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("delete unsigned EVM nonce lease: %w", err)
		}
		_, err = q.Exec(txCtx, `UPDATE reg_evm_nonce_cursors SET next_nonce=$3,version=version+1,updated_at=NOW()
			WHERE network_id=$1 AND source_address=$2`, request.NetworkID, request.SourceAddress, nonceEncoded)
		return err
	})
}

type startupNonceLease struct {
	Nonce    uint64
	Status   string
	TxHashes []string
}

func (m *PostgresNonceManager) loadStartupLeases(ctx context.Context, q postgres.Querier, networkID id.ID, source string) ([]startupNonceLease, error) {
	rows, err := q.Query(ctx, `
		SELECT l.nonce::text,l.status,
		       COALESCE(array_agg(DISTINCT evidence.tx_hash) FILTER (WHERE evidence.tx_hash<>''),ARRAY[]::text[])
		FROM reg_evm_nonce_leases l
		LEFT JOIN LATERAL (
			SELECT e.tx_hash FROM reg_evm_nonce_lease_events e WHERE e.lease_id=l.id
			UNION
			SELECT a.tx_hash FROM reg_crypto_signing_authorizations a
			WHERE a.basis_type=l.basis_type AND a.basis_id=l.basis_id
			  AND a.purpose=l.purpose AND a.attempt=l.attempt
		) evidence ON TRUE
		WHERE l.network_id=$1 AND l.source_address=$2
		  AND l.status IN ('reserved','signed','broadcast','mined','held')
		GROUP BY l.id,l.nonce,l.status ORDER BY l.nonce
	`, networkID, source)
	if err != nil {
		return nil, fmt.Errorf("load EVM startup nonce leases: %w", err)
	}
	defer rows.Close()
	var result []startupNonceLease
	for rows.Next() {
		var encoded string
		var lease startupNonceLease
		if err := rows.Scan(&encoded, &lease.Status, &lease.TxHashes); err != nil {
			return nil, err
		}
		nonce, err := strconv.ParseUint(encoded, 10, 64)
		if err != nil {
			return nil, err
		}
		lease.Nonce = nonce
		result = append(result, lease)
	}
	return result, rows.Err()
}

func appendNonceEvent(ctx context.Context, q postgres.Querier, leaseID id.ID, generation int, from, to, txHash string, maxFee uint64, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	_, err := q.Exec(ctx, `
		INSERT INTO reg_evm_nonce_lease_events (lease_id,generation,from_status,to_status,tx_hash,max_fee_minor,metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, leaseID, generation, from, to, txHash, strconv.FormatUint(maxFee, 10), metadata)
	return err
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

var _ NonceManager = (*PostgresNonceManager)(nil)
var _ StartupNonceManager = (*PostgresNonceManager)(nil)
var _ UnsignedTailNonceManager = (*PostgresNonceManager)(nil)
