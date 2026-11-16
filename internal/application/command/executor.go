// Package command provides durable, application-level command idempotency.
// It deliberately contains no HTTP or PostgreSQL types.
package command

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"metapus/internal/core/json"
	"metapus/internal/core/tx"
)

var (
	// ErrInProgress means another transaction still owns the same command.
	ErrInProgress = errors.New("idempotent command is in progress")
	// ErrKeyReused means a durable key was presented with a different request.
	ErrKeyReused = errors.New("idempotency key reused with another request")
)

// Metadata is the immutable identity of an application command.
type Metadata struct {
	ScopeType          string
	ScopeID            string
	Operation          string
	IdempotencyKey     string
	RequestFingerprint string
}

// Result is a transport-independent command result envelope.
type Result struct {
	Outcome         string          `json:"outcome"`
	ResourceType    string          `json:"resourceType,omitempty"`
	ResourceID      string          `json:"resourceId,omitempty"`
	ResourceVersion int64           `json:"resourceVersion,omitempty"`
	Payload         json.RawMessage `json:"payload,omitempty"`
}

// Record is the repository view required by the executor.
type Record struct {
	Metadata Metadata
	Status   string
	Result   Result
}

// AcquireResult distinguishes ownership from a committed replay.
type AcquireResult struct {
	Acquired bool
	Record   Record
}

// Repository persists command identity and results using the transaction in ctx.
type Repository interface {
	Acquire(ctx context.Context, metadata Metadata) (AcquireResult, error)
	Complete(ctx context.Context, metadata Metadata, result Result) error
}

// Execution is returned both for the first execution and a replay.
type Execution struct {
	Result   Result
	Replayed bool
}

// FailureMapper selects callback failures that are stable application
// outcomes. Mapped failures are stored after the callback savepoint is rolled
// back. Authentication, transient and internal errors must not be mapped.
type FailureMapper func(error) (Result, bool)

// Executor owns the transaction boundary for an idempotent application command.
type Executor struct {
	tx   tx.Manager
	repo Repository
}

func NewExecutor(txManager tx.Manager, repo Repository) *Executor {
	if txManager == nil || repo == nil {
		panic("command.NewExecutor: tx manager and repository are required")
	}
	return &Executor{tx: txManager, repo: repo}
}

// Execute acquires the durable command identity, runs fn and completes the
// result in one transaction. A rollback removes the acquisition, so there is
// no stale-pending reclamation protocol.
func (e *Executor) Execute(
	ctx context.Context,
	metadata Metadata,
	fn func(context.Context) (Result, error),
) (Execution, error) {
	return e.execute(ctx, metadata, fn, nil)
}

// ExecuteWithFailureMapper additionally persists explicitly selected,
// deterministic failures. The business callback is isolated by a savepoint so
// none of its writes can leak into the terminal failure record.
func (e *Executor) ExecuteWithFailureMapper(
	ctx context.Context,
	metadata Metadata,
	fn func(context.Context) (Result, error),
	mapper FailureMapper,
) (Execution, error) {
	if mapper == nil {
		return Execution{}, errors.New("command failure mapper is required")
	}
	if _, ok := e.tx.(tx.SavepointManager); !ok {
		return Execution{}, errors.New("command tx manager does not support savepoints")
	}
	return e.execute(ctx, metadata, fn, mapper)
}

func (e *Executor) execute(
	ctx context.Context,
	metadata Metadata,
	fn func(context.Context) (Result, error),
	mapper FailureMapper,
) (Execution, error) {
	if err := metadata.Validate(); err != nil {
		return Execution{}, err
	}
	if fn == nil {
		return Execution{}, errors.New("command callback is required")
	}

	var execution Execution
	err := e.tx.RunInTransaction(ctx, func(txCtx context.Context) error {
		acquired, err := e.repo.Acquire(txCtx, metadata)
		if err != nil {
			return err
		}
		if !acquired.Acquired {
			if acquired.Record.Metadata.RequestFingerprint != metadata.RequestFingerprint {
				return ErrKeyReused
			}
			if acquired.Record.Status == "pending" {
				return ErrInProgress
			}
			execution = Execution{Result: acquired.Record.Result, Replayed: true}
			return nil
		}

		var result Result
		if mapper == nil {
			result, err = fn(txCtx)
			if err != nil {
				return err
			}
		} else {
			var callbackErr error
			err = e.tx.(tx.SavepointManager).RunInSavepoint(txCtx, func(savepointCtx context.Context) error {
				result, callbackErr = fn(savepointCtx)
				return callbackErr
			})
			if err != nil {
				if callbackErr == nil || !errors.Is(err, callbackErr) {
					return err
				}
				mapped, ok := mapper(callbackErr)
				if !ok {
					return callbackErr
				}
				result = mapped
			}
		}
		if err := result.Validate(); err != nil {
			return err
		}
		if err := e.repo.Complete(txCtx, metadata, result); err != nil {
			return err
		}
		execution = Execution{Result: result}
		return nil
	})
	return execution, err
}

func (m Metadata) Validate() error {
	if !scopeTypePattern.MatchString(m.ScopeType) {
		return fmt.Errorf("invalid command scope type %q", m.ScopeType)
	}
	if strings.TrimSpace(m.ScopeID) == "" || strings.TrimSpace(m.Operation) == "" || strings.TrimSpace(m.IdempotencyKey) == "" {
		return errors.New("command scope, operation and idempotency key are required")
	}
	if len(m.RequestFingerprint) != 64 {
		return errors.New("command request fingerprint must be sha256 hex")
	}
	if _, err := hex.DecodeString(m.RequestFingerprint); err != nil {
		return errors.New("command request fingerprint must be sha256 hex")
	}
	return nil
}

func (r Result) Validate() error {
	if strings.TrimSpace(r.Outcome) == "" {
		return errors.New("command outcome is required")
	}
	if r.ResourceVersion < 0 {
		return errors.New("command resource version cannot be negative")
	}
	if len(r.Payload) > 0 && !json.Valid(r.Payload) {
		return errors.New("command result payload must be valid JSON")
	}
	return nil
}

// scopeTypePattern accepts the platform scope types (user, api_key, system)
// and those a configuration adds through a command scope resolver. It matches
// the sys_idempotency.scope_type check.
var scopeTypePattern = regexp.MustCompile(`^[a-z][a-z_]{0,31}$`)
