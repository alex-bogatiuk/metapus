package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/posting"
)

// Sequences of documents (entity.SequenceRule) keep their boundaries in
// sys_sequence_boundaries. A write of a document in a sequence holds the
// sequence's shared advisory lock and a closing its exclusive one, taken
// before the closing reads what it closes: a closing never misses a
// document posted while it is calculated, and the boundary moves back for
// one posted after it.

func lockSequence(ctx context.Context, key string, exclusive bool) error {
	statement := "SELECT pg_advisory_xact_lock_shared(hashtext($1))"
	if exclusive {
		statement = "SELECT pg_advisory_xact_lock(hashtext($1))"
	}
	if _, err := MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, statement, "sequence:"+key); err != nil {
		return fmt.Errorf("lock sequence %s: %w", key, err)
	}
	return nil
}

// SequenceBoundary returns the boundary of the sequence key; ok is false for
// a sequence that was never closed.
func SequenceBoundary(ctx context.Context, key string) (boundary time.Time, ok bool, err error) {
	err = MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx,
		"SELECT boundary FROM sys_sequence_boundaries WHERE sequence = $1", key).Scan(&boundary)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read sequence boundary: %w", err)
	}
	return boundary, true, nil
}

// moveSequenceBack moves the boundary of the sequence key back to at when it
// is later.
func moveSequenceBack(ctx context.Context, key string, at time.Time) error {
	_, err := MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx,
		"UPDATE sys_sequence_boundaries SET boundary = $2, updated_at = now() WHERE sequence = $1 AND boundary > $2", key, at)
	if err != nil {
		return fmt.Errorf("move sequence boundary: %w", err)
	}
	return nil
}

// setSequenceBoundary sets the boundary of the sequence key to at.
func setSequenceBoundary(ctx context.Context, key string, at time.Time) error {
	_, err := MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		INSERT INTO sys_sequence_boundaries (sequence, boundary) VALUES ($1, $2)
		ON CONFLICT (sequence) DO UPDATE SET boundary = EXCLUDED.boundary, updated_at = now()
	`, key, at)
	if err != nil {
		return fmt.Errorf("set sequence boundary: %w", err)
	}
	return nil
}

// SequenceWrite is a document write as its sequences see it: the document's
// date and whether it is posted, before (Stored, for a stored document) and
// after the write.
type SequenceWrite struct {
	Stored       bool
	StoredDate   time.Time
	StoredPosted bool
	Date         time.Time
	Posted       bool
}

// KeepSequences keeps the boundaries of the sequences of def after a write
// of doc (entity.SequenceRule): a posted document of a sequence, before or
// after the write, moves its boundary back to the earlier date; a closing
// posts only while its sequence's boundary is not before its period's
// start, and moves the boundary to the period's end, or back to its start
// when it is unposted.
func KeepSequences(ctx context.Context, def entity.Definition, doc any, write SequenceWrite) error {
	rule := def.Sequence
	storedPosted := write.Stored && write.StoredPosted
	if len(rule.In) > 0 && (write.Posted || storedPosted) {
		at := write.Date
		if !write.Posted || storedPosted && write.StoredDate.Before(at) {
			at = write.StoredDate
		}
		for _, key := range rule.In {
			if err := lockSequence(ctx, key, false); err != nil {
				return err
			}
			if err := moveSequenceBack(ctx, key, at); err != nil {
				return err
			}
		}
	}
	if rule.Restores == "" || !write.Posted && !storedPosted {
		return nil
	}
	closing, ok := doc.(entity.SequencePeriod)
	if !ok {
		return fmt.Errorf("document %s restores sequence %s but has no period", def.Name, rule.Restores)
	}
	from, to := closing.SequencePeriod()
	if err := lockSequence(ctx, rule.Restores, true); err != nil {
		return err
	}
	if !write.Posted {
		return moveSequenceBack(ctx, rule.Restores, from)
	}
	boundary, closed, err := SequenceBoundary(ctx, rule.Restores)
	if err != nil {
		return err
	}
	if closed && boundary.Before(from) {
		return posting.NewSequenceNotRestored(boundary)
	}
	return setSequenceBoundary(ctx, rule.Restores, to)
}

// SequenceLocker is the posting engine's recorder that takes the locks of
// sequences before a document is posted: the exclusive lock of the
// sequence a closing restores, before it reads what it closes, and the
// shared lock of the sequences a document is in.
type SequenceLocker struct{}

// NewSequenceLocker returns the sequence locker.
func NewSequenceLocker() *SequenceLocker { return &SequenceLocker{} }

func (*SequenceLocker) Name() string { return "sequences" }

func (*SequenceLocker) RecordFromSet(context.Context, *posting.MovementSet) error { return nil }

func (*SequenceLocker) ReverseMovements(context.Context, id.ID, int) error { return nil }

func (*SequenceLocker) LockPostingResources(ctx context.Context, doc posting.Postable) error {
	provider, ok := doc.(entity.DefinitionProvider)
	if !ok {
		return nil
	}
	rule := provider.EntityDefinition().Sequence
	if rule.Restores != "" {
		if err := lockSequence(ctx, rule.Restores, true); err != nil {
			return err
		}
	}
	for _, key := range rule.In {
		if err := lockSequence(ctx, key, false); err != nil {
			return err
		}
	}
	return nil
}

func (*SequenceLocker) LockReversalResources(context.Context, id.ID) error { return nil }
