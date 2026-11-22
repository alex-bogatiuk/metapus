// Package command runs idempotent commands: an Execution is acquired by
// its key and fingerprint, and its result is replayed.
package command

import (
	core "metapus/internal/application/command"
	"metapus/internal/core/json"
	"metapus/internal/core/tx"
)

type (
	// AcquireResult distinguishes ownership from a committed replay.
	AcquireResult = core.AcquireResult
	// Execution is returned both for the first execution and a replay.
	Execution = core.Execution
	// Executor owns the transaction boundary for an idempotent application command.
	Executor = core.Executor
	// Metadata is the immutable identity of an application command.
	Metadata = core.Metadata
	// Repository persists command identity and results using the transaction in ctx.
	Repository = core.Repository
	// Result is a transport-independent command result envelope.
	Result = core.Result
)

var (
	// ErrInProgress means another transaction still owns the same command.
	ErrInProgress = core.ErrInProgress
	// ErrKeyReused means a durable key was presented with a different request.
	ErrKeyReused = core.ErrKeyReused
)

// Fingerprint returns a deterministic SHA-256 over a validated business DTO
// envelope. encoding/json sorts map keys, while structs make the intended
// business fields explicit at each use-case boundary.
func Fingerprint(value any) (string, error) { return core.Fingerprint(value) }

// MustPayload serializes a result DTO. It is intended for application
// services immediately after they have constructed a known-valid response.
func MustPayload(value any) json.RawMessage { return core.MustPayload(value) }

func NewExecutor(txManager tx.Manager, repo core.Repository) *core.Executor {
	return core.NewExecutor(txManager, repo)
}
