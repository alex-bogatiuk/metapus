package numerator

import (
	"context"
	"time"
)

// noopGenerator is a Generator that panics on every call.
// Used when numbering is not needed (e.g., Worker doesn't create wallets).
type noopGenerator struct{}

// Noop returns a Generator that panics on any call.
// Safe to pass to services that won't invoke numbering in the current context.
func Noop() Generator {
	return noopGenerator{}
}

func (noopGenerator) GetNextNumber(_ context.Context, _ Config, _ *Options, _ time.Time) (string, error) {
	panic("numerator.Noop: GetNextNumber called — this indicates a bug")
}

func (noopGenerator) SetNextNumber(_ context.Context, _ Config, _ time.Time, _ int64) error {
	panic("numerator.Noop: SetNextNumber called — this indicates a bug")
}
