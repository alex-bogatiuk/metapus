package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/alex-bogatiuk/metapus/internal/application/command"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
)

// CommandRepository is the PostgreSQL adapter for durable application command
// identity. Every method requires the transaction context owned by Executor.
type CommandRepository struct {
	lockTimeoutMilliseconds int
}

func NewCommandRepository() *CommandRepository {
	return &CommandRepository{lockTimeoutMilliseconds: 3000}
}

func (r *CommandRepository) Acquire(ctx context.Context, metadata command.Metadata) (command.AcquireResult, error) {
	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	var previousLockTimeout string
	if err := querier.QueryRow(ctx, "SELECT current_setting('lock_timeout')").Scan(&previousLockTimeout); err != nil {
		return command.AcquireResult{}, fmt.Errorf("read command lock timeout: %w", err)
	}
	if _, err := querier.Exec(ctx, "SELECT set_config('lock_timeout',$1,true)", fmt.Sprintf("%dms", r.lockTimeoutMilliseconds)); err != nil {
		return command.AcquireResult{}, fmt.Errorf("set command lock timeout: %w", err)
	}

	var inserted bool
	err := querier.QueryRow(ctx, `
		INSERT INTO sys_idempotency (
			scope_type, scope_id, operation, idempotency_key,
			request_fingerprint, status
		) VALUES ($1,$2,$3,$4,$5,'pending')
		ON CONFLICT (scope_type,scope_id,operation,idempotency_key) DO NOTHING
		RETURNING TRUE
	`, metadata.ScopeType, metadata.ScopeID, metadata.Operation, metadata.IdempotencyKey, metadata.RequestFingerprint).Scan(&inserted)
	if err == nil && inserted {
		if restoreErr := restoreCommandLockTimeout(ctx, querier, previousLockTimeout); restoreErr != nil {
			return command.AcquireResult{}, restoreErr
		}
		return command.AcquireResult{Acquired: true}, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		if isCommandLockTimeout(err) {
			return command.AcquireResult{}, command.ErrInProgress
		}
		return command.AcquireResult{}, fmt.Errorf("acquire command identity: %w", err)
	}
	if restoreErr := restoreCommandLockTimeout(ctx, querier, previousLockTimeout); restoreErr != nil {
		return command.AcquireResult{}, restoreErr
	}

	var record command.Record
	var resultPayload []byte
	record.Metadata = metadata
	err = querier.QueryRow(ctx, `
		SELECT request_fingerprint,status,outcome,resource_type,resource_id,
		       resource_version,application_result
		FROM sys_idempotency
		WHERE scope_type=$1 AND scope_id=$2 AND operation=$3 AND idempotency_key=$4
	`, metadata.ScopeType, metadata.ScopeID, metadata.Operation, metadata.IdempotencyKey).Scan(
		&record.Metadata.RequestFingerprint,
		&record.Status,
		&record.Result.Outcome,
		&record.Result.ResourceType,
		&record.Result.ResourceID,
		&record.Result.ResourceVersion,
		&resultPayload,
	)
	if err != nil {
		return command.AcquireResult{}, fmt.Errorf("load command result: %w", err)
	}
	if record.Metadata.RequestFingerprint != metadata.RequestFingerprint {
		return command.AcquireResult{}, command.ErrKeyReused
	}
	if record.Status == "pending" {
		return command.AcquireResult{}, command.ErrInProgress
	}
	if len(resultPayload) > 0 && string(resultPayload) != "{}" && string(resultPayload) != "null" {
		record.Result.Payload = json.RawMessage(resultPayload)
	}
	return command.AcquireResult{Record: record}, nil
}

func restoreCommandLockTimeout(ctx context.Context, querier Querier, value string) error {
	if _, err := querier.Exec(ctx, "SELECT set_config('lock_timeout',$1,true)", value); err != nil {
		return fmt.Errorf("restore command lock timeout: %w", err)
	}
	return nil
}

func (r *CommandRepository) Complete(ctx context.Context, metadata command.Metadata, result command.Result) error {
	payload := result.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	status := "success"
	if strings.HasPrefix(result.Outcome, "error:") {
		status = "failed"
	}
	tag, err := MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE sys_idempotency
		SET status=$11, outcome=$5, resource_type=$6, resource_id=$7,
		    resource_version=$8, application_result=$9, completed_at=NOW()
		WHERE scope_type=$1 AND scope_id=$2 AND operation=$3
		  AND idempotency_key=$4 AND request_fingerprint=$10 AND status='pending'
	`, metadata.ScopeType, metadata.ScopeID, metadata.Operation, metadata.IdempotencyKey,
		result.Outcome, result.ResourceType, result.ResourceID, result.ResourceVersion,
		payload, metadata.RequestFingerprint, status)
	if err != nil {
		return fmt.Errorf("complete command result: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("complete command result: pending command identity not found")
	}
	return nil
}

func isCommandLockTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "55P03" || pgErr.Code == "57014")
}

var _ command.Repository = (*CommandRepository)(nil)
