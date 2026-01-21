package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"metapus/internal/core/tx"
	"metapus/pkg/logger"
)

var tracer = otel.Tracer("metapus/tx")

// Compile-time check that TxManager implements tx.Manager interface.
var _ tx.Manager = (*TxManager)(nil)

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
	savepoint string
	nested    bool
}

// RunInTransaction executes fn within a transaction.
// If a transaction already exists in ctx, it will be reused (nested transaction).
func (m *TxManager) RunInTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.RunInTransactionWithOptions(ctx, DefaultTxOptions(), fn)
}

// RunInTransactionWithOptions executes fn with custom transaction options.
func (m *TxManager) RunInTransactionWithOptions(ctx context.Context, opts TxOptions, fn func(ctx context.Context) error) error {
	// Start tracing span
	ctx, span := tracer.Start(ctx, "transaction",
		trace.WithAttributes(
			attribute.String("tx.isolation", string(opts.IsolationLevel)),
		))
	defer span.End()

	// Check for existing transaction
	if existing := m.GetTx(ctx); existing != nil {
		return m.handleNestedTransaction(ctx, existing, opts, fn)
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
		_, err = tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", opts.StatementTimeout.Milliseconds()))
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("set statement_timeout: %w", err)
		}
	}

	// Store transaction in context
	wrappedTx := &Tx{Tx: tx, nested: false}
	txCtx := context.WithValue(ctx, txKey{}, wrappedTx)

	// Execute function
	if err := m.executeWithRollbackProtection(txCtx, tx, fn); err != nil {
		return err
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// handleNestedTransaction manages nested transaction (reuses or creates savepoint).
func (m *TxManager) handleNestedTransaction(ctx context.Context, existing *Tx, opts TxOptions, fn func(ctx context.Context) error) error {
	if !opts.UseSavepoint {
		// Reuse existing transaction without savepoint
		return fn(ctx)
	}

	// Create savepoint for true nested transaction behavior
	savepointName := fmt.Sprintf("sp_%d", time.Now().UnixNano())
	_, err := existing.Exec(ctx, "SAVEPOINT "+savepointName)
	if err != nil {
		return fmt.Errorf("create savepoint: %w", err)
	}

	// Execute function
	if err := fn(ctx); err != nil {
		// Rollback to savepoint
		_, rbErr := existing.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepointName)
		if rbErr != nil {
			logger.Error(ctx, "rollback to savepoint failed", "savepoint", savepointName, "error", rbErr)
		}
		return err
	}

	// Release savepoint
	_, err = existing.Exec(ctx, "RELEASE SAVEPOINT "+savepointName)
	if err != nil {
		return fmt.Errorf("release savepoint: %w", err)
	}

	return nil
}

// executeWithRollbackProtection runs fn and handles rollback on error.
// Context cancellation is handled by pgx internally - no goroutine needed.
func (m *TxManager) executeWithRollbackProtection(ctx context.Context, tx pgx.Tx, fn func(ctx context.Context) error) error {
	err := fn(ctx)
	if err != nil {
		// Use background context for rollback to ensure it completes
		// even if the original context was cancelled
		if rbErr := tx.Rollback(context.Background()); rbErr != nil {
			logger.Error(ctx, "rollback failed", "error", rbErr, "original_error", err)
		}
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
}

// GetQuerier returns appropriate querier for context.
func (m *TxManager) GetQuerier(ctx context.Context) Querier {
	if tx := m.GetTx(ctx); tx != nil {
		return tx.Tx
	}
	return m.pool
}

// ReadOnly executes fn in a read-only transaction.
func (m *TxManager) ReadOnly(ctx context.Context, fn func(ctx context.Context) error) error {
	opts := DefaultTxOptions()
	opts.AccessMode = pgx.ReadOnly
	return m.RunInTransactionWithOptions(ctx, opts, fn)
}
