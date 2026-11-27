package entity

import (
	"fmt"
	"slices"
	"time"
)

// SequenceRule places a document in sequences of documents
// (последовательности документов): the order in which their posting must be
// processed by a period's closing, such as the month closing that costs the
// stock. Each sequence has a boundary, the moment up to which it is
// restored: every document before it was taken into account by a posted
// closing.
//
// The document repository keeps the boundary on every write. Posting,
// reposting or unposting a document of In dated before the boundary moves
// the boundary back to its date, so the closings from there on are to be
// posted again. A document that Restores a sequence closes a period
// (SequencePeriod): it posts only while the boundary is not before the
// period's start, and its posting moves the boundary to the period's end.
type SequenceRule struct {
	// In are the keys of the sequences the document belongs to.
	In []string
	// Restores is the key of the sequence the document closes; its model
	// implements SequencePeriod.
	Restores string
}

// SequencePeriod is the period [From, To) a document restoring a sequence
// closes.
type SequencePeriod interface {
	SequencePeriod() (from, to time.Time)
}

// Declared reports whether the rule places the document in any sequence.
func (r SequenceRule) Declared() bool { return len(r.In) > 0 || r.Restores != "" }

func (r SequenceRule) validate(entityName string) error {
	for i, key := range r.In {
		if !definitionKey.MatchString(key) || slices.Contains(r.In[:i], key) {
			return fmt.Errorf("entity %q: sequence %q must be a snake_case key listed once", entityName, key)
		}
	}
	if r.Restores != "" && (!definitionKey.MatchString(r.Restores) || slices.Contains(r.In, r.Restores)) {
		return fmt.Errorf("entity %q: a document restores a sequence %q it is not in, by a snake_case key", entityName, r.Restores)
	}
	return nil
}
