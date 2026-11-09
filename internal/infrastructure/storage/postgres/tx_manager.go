package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	coretx "metapus/internal/core/tx"
	"metapus/pkg/logger"
)

var tracer = otel.Tracer("metapus/tx")

// Compile-time check that TxManager implements tx.Manager interface.
var _ coretx.Manager = (*TxManager)(nil)
var _ coretx.SavepointManager = (*TxManager)(nil)

// TxOptions configures transaction behavior.
type TxOptions struct {
	// IsolationLevel: pgx.Serializable, pgx.RepeatableRead, pgx.ReadCommitted
	IsolationLevel pgx.TxIsoLevel

	// AccessMode: pgx.ReadWrite, pgx.ReadOnly
	AccessMode pgx.TxAccessMode

	// StatementTimeout protects against long-running queries (default 30s)
	StatementTimeout time.Duration

	// UseSavepoint creates savepoint for nested transactions
	// WARNING: Savepoints are expensive, use only when needed
	UseSavepoint bool
}

// DefaultTxOptions returns production-safe defaults.
func DefaultTxOptions() TxOptions {
	return TxOptions{
		IsolationLevel:   pgx.ReadCommitted,
		AccessMode:       pgx.ReadWrite,
		StatementTimeout: 30 * time.Second,
		UseSavepoint:     false,
	}
}

// SerializableTxOptions for critical operations requiring serializable isolation.
func SerializableTxOptions() TxOptions {
	opts := DefaultTxOptions()
	opts.IsolationLevel = pgx.Serializable
	return opts
}

// TxManager manages database transactions with support for:
// - Nested transactions (with optional savepoints)
// - Statement timeout protection
// - Context cancellation handling
// - Distributed tracing integration
type TxManager struct {
	pool *pgxpool.Pool
}

// NewTxManager creates a new transaction manager.
func NewTxManager(pool *Pool) *TxManager {
	return &TxManager{pool: pool.Pool}
}

// NewTxManagerFromRawPool creates a new transaction manager from raw pgxpool.Pool.
func NewTxManagerFromRawPool(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

// txKey is the context key for active transaction.
type txKey struct{}

// Tx wraps pgx.Tx with metadata.
type Tx struct {
	pgx.Tx
	options TxOptions
}

// RunInTransaction executes fn within a transaction.
// If a transaction already exists in ctx, it will be reused (nested transaction).
func (m *TxManager) RunInTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.runInTransaction(ctx, DefaultTxOptions(), false, fn)
}

// RunInTransactionWithOptions executes fn with custom transaction options.
func (m *TxManager) RunInTransactionWithOptions(ctx context.Context, opts TxOptions, fn func(ctx context.Context) error) error {
	return m.runInTransaction(ctx, opts, true, fn)
}

// RunInSavepoint creates an explicit rollback boundary inside the current
// transaction. It is intentionally part of the infrastructure adapter so the
// application layer never needs to know PostgreSQL savepoint syntax.
func (m *TxManager) RunInSavepoint(ctx context.Context, fn func(ctx context.Context) error) error {
	opts := DefaultTxOptions()
	opts.UseSavepoint = true
	return m.RunInTransactionWithOptions(ctx, opts, fn)
}

func (m *TxManager) runInTransaction(ctx context.Context, opts TxOptions, enforceNestedOptions bool, fn func(ctx context.Context) error) error {
	// Start tracing span
	ctx, span := tracer.Start(ctx, "transaction",
		trace.WithAttributes(
			attribute.String("tx.isolation", string(opts.IsolationLevel)),
		))
	defer span.End()

	// Check for existing transaction
	if existing := m.GetTx(ctx); existing != nil {
		return m.handleNestedTransaction(ctx, existing, opts, enforceNestedOptions, fn)
	}

	// Start new transaction
	return m.startNewTransaction(ctx, opts, fn)
}

// startNewTransaction begins a new database transaction.
func (m *TxManager) startNewTransaction(ctx context.Context, opts TxOptions, fn func(ctx context.Context) error) error {
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   opts.IsolationLevel,
		AccessMode: opts.AccessMode,
	})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// Set statement timeout for protection against runaway queries
	if opts.StatementTimeout > 0 {
		if err = setLocalStatementTimeout(ctx, tx, opts.StatementTimeout); err != nil {
			rollbackTransaction(ctx, tx, err)
			return fmt.Errorf("set statement_timeout: %w", err)
		}
	}

	// Store transaction in context
	wrappedTx := &Tx{Tx: tx, options: opts}
	txCtx := context.WithValue(ctx, txKey{}, wrappedTx)
	txCtx, completion := coretx.WithCommitScope(txCtx)
	defer completion.Discard()

	// Execute function
	if err := m.executeWithRollbackProtection(txCtx, tx, fn); err != nil {
		return err
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	completion.Commit(context.WithoutCancel(ctx))

	return nil
}

// handleNestedTransaction manages nested transaction (reuses or creates savepoint).
func (m *TxManager) handleNestedTransaction(ctx context.Context, existing *Tx, opts TxOptions, enforceOptions bool, fn func(ctx context.Context) error) error {
	if enforceOptions {
		if err := validateNestedOptions(existing.options, opts); err != nil {
			return err
		}
	}

	scopeTimeout := enforceOptions && nestedTimeoutRequiresScope(existing.options, opts)
	if !opts.UseSavepoint && !scopeTimeout {
		// Reuse existing transaction without savepoint
		return fn(ctx)
	}

	// Create savepoint for true nested transaction behavior
	savepointName := "sp_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := existing.Exec(ctx, "SAVEPOINT "+savepointName)
	if err != nil {
		return fmt.Errorf("create savepoint: %w", err)
	}
	if scopeTimeout {
		if err := setLocalStatementTimeout(ctx, existing.Tx, opts.StatementTimeout); err != nil {
			return rollbackNestedSavepoint(ctx, existing.Tx, savepointName, fmt.Errorf("set nested statement_timeout: %w", err))
		}
	}

	// Rolled-back savepoints must discard only their own local callbacks.
	nestedCtx, completion := coretx.WithCommitScope(ctx)
	defer completion.Discard()
	if err := fn(nestedCtx); err != nil {
		return rollbackNestedSavepoint(ctx, existing.Tx, savepointName, err)
	}
	if scopeTimeout {
		if err := setLocalStatementTimeout(ctx, existing.Tx, existing.options.StatementTimeout); err != nil {
			return rollbackNestedSavepoint(ctx, existing.Tx, savepointName, fmt.Errorf("restore outer statement_timeout: %w", err))
		}
	}

	// Release savepoint
	_, err = existing.Exec(ctx, "RELEASE SAVEPOINT "+savepointName)
	if err != nil {
		return fmt.Errorf("release savepoint: %w", err)
	}

	return completion.Merge(ctx)
}

func validateNestedOptions(outer, requested TxOptions) error {
	if isolationRank(outer.IsolationLevel) < isolationRank(requested.IsolationLevel) {
		return fmt.Errorf(
			"nested transaction requires isolation %s, outer transaction uses %s",
			requested.IsolationLevel,
			outer.IsolationLevel,
		)
	}
	if outer.AccessMode != requested.AccessMode {
		return fmt.Errorf(
			"nested transaction requires access mode %s, outer transaction uses %s",
			requested.AccessMode,
			outer.AccessMode,
		)
	}
	if requested.StatementTimeout < 0 {
		return fmt.Errorf("nested transaction statement timeout cannot be negative")
	}
	if requested.StatementTimeout != outer.StatementTimeout && !timeoutAtLeastAsStrict(requested.StatementTimeout, outer.StatementTimeout) {
		return fmt.Errorf(
			"nested transaction requests statement timeout %s, outer transaction enforces stricter timeout %s",
			requested.StatementTimeout,
			outer.StatementTimeout,
		)
	}
	return nil
}

func nestedTimeoutRequiresScope(outer, requested TxOptions) bool {
	return requested.StatementTimeout != outer.StatementTimeout
}

func timeoutAtLeastAsStrict(requested, outer time.Duration) bool {
	if outer <= 0 {
		return requested > 0
	}
	return requested > 0 && requested <= outer
}

func setLocalStatementTimeout(ctx context.Context, transaction pgx.Tx, timeout time.Duration) error {
	if timeout <= 0 {
		_, err := transaction.Exec(ctx, "SET LOCAL statement_timeout = 0")
		return err
	}
	_, err := transaction.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", max(timeout.Milliseconds(), 1)))
	return err
}

func rollbackNestedSavepoint(ctx context.Context, transaction pgx.Tx, savepointName string, originalErr error) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := transaction.Exec(rollbackCtx, "ROLLBACK TO SAVEPOINT "+savepointName); err != nil {
		logger.Error(ctx, "rollback to savepoint failed", "savepoint", savepointName, "error", err)
	}
	return originalErr
}

func rollbackTransaction(ctx context.Context, transaction pgx.Tx, originalErr error) {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := transaction.Rollback(rollbackCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		args := []any{"error", err}
		if originalErr != nil {
			args = append(args, "original_error", originalErr)
		}
		logger.Error(ctx, "rollback failed", args...)
	}
}

func isolationRank(level pgx.TxIsoLevel) int {
	switch level {
	case pgx.Serializable:
		return 3
	case pgx.RepeatableRead:
		return 2
	case pgx.ReadCommitted, pgx.ReadUncommitted, "":
		return 1
	default:
		return 0
	}
}

// executeWithRollbackProtection runs fn and handles rollback on error.
// Context cancellation is handled by pgx internally - no goroutine needed.
func (m *TxManager) executeWithRollbackProtection(ctx context.Context, tx pgx.Tx, fn func(ctx context.Context) error) error {
	returned := false
	defer func() {
		// testing.T.FailNow, runtime.Goexit, and panics do not return from fn.
		// Roll back synchronously before stack unwinding can strand a pooled
		// connection in an open transaction.
		if !returned {
			rollbackTransaction(ctx, tx, nil)
		}
	}()

	err := fn(ctx)
	returned = true
	if err != nil {
		rollbackTransaction(ctx, tx, err)
		return err
	}
	return nil
}

// GetTx returns the current transaction from context, or nil if none.
func (m *TxManager) GetTx(ctx context.Context) *Tx {
	if tx, ok := ctx.Value(txKey{}).(*Tx); ok {
		return tx
	}
	return nil
}

// GetConn returns transaction if in context, otherwise acquires from pool.
// This allows repos to work both inside and outside transactions.
type Querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// GetQuerier returns appropriate querier for context.
func (m *TxManager) GetQuerier(ctx context.Context) Querier {
	if tx := m.GetTx(ctx); tx != nil {
		return tx.Tx
	}
	return m.pool
}

// QueryRow lets infrastructure components that only need a narrow query port
// participate in the active transaction without depending on PostgreSQL's
// full Querier type. Outside a transaction it safely falls back to the pool.
func (m *TxManager) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return m.GetQuerier(ctx).QueryRow(ctx, sql, args...)
}

// ReadOnly executes fn in a read-only transaction.
func (m *TxManager) ReadOnly(ctx context.Context, fn func(ctx context.Context) error) error {
	opts := DefaultTxOptions()
	opts.AccessMode = pgx.ReadOnly
	return m.RunInTransactionWithOptions(ctx, opts, fn)
}
