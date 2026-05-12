package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/id"
	"metapus/internal/core/workerjob"
)

// WorkerJobRepo implements workerjob.Repository using a direct pgxpool.Pool.
// Unlike EventLogRepo, the worker has no TxManager — it writes directly via pool.
type WorkerJobRepo struct {
	pool *pgxpool.Pool
}

// NewWorkerJobRepo creates a worker job repository backed by the tenant pool.
// Use in background workers where TxManager is not available.
func NewWorkerJobRepo(pool *pgxpool.Pool) *WorkerJobRepo {
	return &WorkerJobRepo{pool: pool}
}

// WorkerJobReadRepo is a read-only repository variant for use in HTTP handlers.
// It extracts the querier from the TxManager injected by TenantDB middleware.
type WorkerJobReadRepo struct{}

// NewWorkerJobReadRepo creates a read-only worker job repository for HTTP handlers.
func NewWorkerJobReadRepo() *WorkerJobReadRepo {
	return &WorkerJobReadRepo{}
}

// ── WorkerJobReadRepo — HTTP handler variant (uses TxManager from ctx) ─────

func (r *WorkerJobReadRepo) psql() squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
}

func (r *WorkerJobReadRepo) Insert(_ context.Context, _ *workerjob.Job) error { return nil }
func (r *WorkerJobReadRepo) Update(_ context.Context, _ *workerjob.Job) error { return nil }

func (r *WorkerJobReadRepo) List(ctx context.Context, f workerjob.Filter) (workerjob.ListResult, error) {
	var result workerjob.ListResult
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}

	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	conds := buildWorkerJobConditions(f)

	countQ := r.psql().Select("COUNT(*)").From("sys_worker_jobs")
	for _, c := range conds {
		countQ = countQ.Where(c)
	}
	countSQL, countArgs, err := countQ.ToSql()
	if err != nil {
		return result, fmt.Errorf("workerjob read: build count: %w", err)
	}
	if err := querier.QueryRow(ctx, countSQL, countArgs...).Scan(&result.TotalCount); err != nil {
		return result, fmt.Errorf("workerjob read: count: %w", err)
	}

	q := r.psql().
		Select("id", "job_name", "job_category", "status",
			"started_at", "finished_at", "duration_ms", "items_processed",
			"error_message", "metadata").
		From("sys_worker_jobs")
	for _, c := range conds {
		q = q.Where(c)
	}
	if f.After != "" {
		if ts, err := decodeCursorTS(f.After); err == nil {
			q = q.Where(squirrel.Lt{"started_at": ts})
		}
	}
	q = q.OrderBy("started_at DESC").Limit(uint64(limit + 1))

	sqlStr, args, err := q.ToSql()
	if err != nil {
		return result, fmt.Errorf("workerjob read: build list: %w", err)
	}

	rows, err := querier.Query(ctx, sqlStr, args...)
	if err != nil {
		return result, fmt.Errorf("workerjob read: query: %w", err)
	}
	defer rows.Close()

	items := make([]workerjob.Job, 0, limit+1)
	for rows.Next() {
		var job workerjob.Job
		var metaJSON []byte
		if err := rows.Scan(
			&job.ID, &job.JobName, &job.JobCategory, &job.Status,
			&job.StartedAt, &job.FinishedAt, &job.DurationMs, &job.ItemsProcessed,
			&job.ErrorMessage, &metaJSON,
		); err != nil {
			return result, fmt.Errorf("workerjob read: scan: %w", err)
		}
		if metaJSON != nil {
			_ = json.Unmarshal(metaJSON, &job.Metadata)
		}
		items = append(items, job)
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("workerjob read: rows: %w", err)
	}

	if len(items) > limit {
		items = items[:limit]
		result.HasMore = true
		result.NextCursor = encodeCursorTS(items[len(items)-1].StartedAt)
	}
	result.Items = items
	return result, nil
}

func (r *WorkerJobReadRepo) GetStats(ctx context.Context, dateFrom, dateTo time.Time) (workerjob.Stats, error) {
	var stats workerjob.Stats
	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	err := querier.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'success'),
			COUNT(*) FILTER (WHERE status = 'error'),
			COALESCE(AVG(duration_ms) FILTER (WHERE status IN ('success', 'error')), 0)::BIGINT
		FROM sys_worker_jobs
		WHERE started_at >= $1 AND started_at < $2
	`, dateFrom, dateTo).Scan(&stats.Total, &stats.Success, &stats.Error, &stats.AvgDuration)
	if err != nil {
		return stats, fmt.Errorf("workerjob read: stats: %w", err)
	}
	return stats, nil
}

// ── WorkerJobRepo — worker variant (uses pool directly) ────────────────────

func (r *WorkerJobRepo) psql() squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
}

// Insert creates a new job record with its final state.
// All fields are written in a single statement — the recorder always prepares
// the complete Job before calling Insert.
func (r *WorkerJobRepo) Insert(ctx context.Context, job *workerjob.Job) error {
	if id.IsNil(job.ID) {
		job.ID = id.New()
	}

	metaJSON, err := marshalWorkerMeta(job.Metadata)
	if err != nil {
		return fmt.Errorf("workerjob: marshal metadata: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO sys_worker_jobs
			(id, job_name, job_category, status, started_at,
			 finished_at, duration_ms, items_processed, error_message, metadata)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		job.ID, job.JobName, job.JobCategory, string(job.Status), job.StartedAt,
		job.FinishedAt, job.DurationMs, job.ItemsProcessed, job.ErrorMessage, metaJSON,
	)
	if err != nil {
		return fmt.Errorf("workerjob: insert: %w", err)
	}
	return nil
}

// Update finalises an existing running job.
func (r *WorkerJobRepo) Update(ctx context.Context, job *workerjob.Job) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE sys_worker_jobs
		SET
			status          = $1,
			finished_at     = $2,
			duration_ms     = $3,
			items_processed = $4,
			error_message   = $5
		WHERE id = $6
	`,
		string(job.Status), job.FinishedAt, job.DurationMs, job.ItemsProcessed, job.ErrorMessage, job.ID,
	)
	if err != nil {
		return fmt.Errorf("workerjob: update: %w", err)
	}
	return nil
}

// List returns cursor-paginated job runs.
// Simple offset-based pagination is acceptable here — ops teams rarely go past page 1.
func (r *WorkerJobRepo) List(ctx context.Context, f workerjob.Filter) (workerjob.ListResult, error) {
	var result workerjob.ListResult

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}

	conds := buildWorkerJobConditions(f)

	// Count
	countQ := r.psql().Select("COUNT(*)").From("sys_worker_jobs")
	for _, c := range conds {
		countQ = countQ.Where(c)
	}
	countSQL, countArgs, err := countQ.ToSql()
	if err != nil {
		return result, fmt.Errorf("workerjob: build count: %w", err)
	}
	if err := r.pool.QueryRow(ctx, countSQL, countArgs...).Scan(&result.TotalCount); err != nil {
		return result, fmt.Errorf("workerjob: count: %w", err)
	}

	// Items (keyset cursor: after = WHERE started_at < $cursor ORDER BY started_at DESC)
	q := r.psql().
		Select("id", "job_name", "job_category", "status",
			"started_at", "finished_at", "duration_ms", "items_processed",
			"error_message", "metadata").
		From("sys_worker_jobs")
	for _, c := range conds {
		q = q.Where(c)
	}
	if f.After != "" {
		// Cursor is a base64-encoded started_at timestamp
		ts, err := decodeCursorTS(f.After)
		if err == nil {
			q = q.Where(squirrel.Lt{"started_at": ts})
		}
	}
	q = q.OrderBy("started_at DESC").Limit(uint64(limit + 1))

	sqlStr, args, err := q.ToSql()
	if err != nil {
		return result, fmt.Errorf("workerjob: build list: %w", err)
	}

	rows, err := r.pool.Query(ctx, sqlStr, args...)
	if err != nil {
		return result, fmt.Errorf("workerjob: query: %w", err)
	}
	defer rows.Close()

	items := make([]workerjob.Job, 0, limit+1)
	for rows.Next() {
		var job workerjob.Job
		var metaJSON []byte
		if err := rows.Scan(
			&job.ID, &job.JobName, &job.JobCategory, &job.Status,
			&job.StartedAt, &job.FinishedAt, &job.DurationMs, &job.ItemsProcessed,
			&job.ErrorMessage, &metaJSON,
		); err != nil {
			return result, fmt.Errorf("workerjob: scan: %w", err)
		}
		if metaJSON != nil {
			_ = json.Unmarshal(metaJSON, &job.Metadata)
		}
		items = append(items, job)
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("workerjob: rows: %w", err)
	}

	if len(items) > limit {
		items = items[:limit]
		result.HasMore = true
		result.NextCursor = encodeCursorTS(items[len(items)-1].StartedAt)
	}
	result.Items = items
	return result, nil
}

// GetStats returns 24h KPI counts.
func (r *WorkerJobRepo) GetStats(ctx context.Context, dateFrom, dateTo time.Time) (workerjob.Stats, error) {
	var stats workerjob.Stats
	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'success'),
			COUNT(*) FILTER (WHERE status = 'error'),
			COALESCE(AVG(duration_ms) FILTER (WHERE status IN ('success', 'error')), 0)::BIGINT
		FROM sys_worker_jobs
		WHERE started_at >= $1 AND started_at < $2
	`, dateFrom, dateTo).Scan(&stats.Total, &stats.Success, &stats.Error, &stats.AvgDuration)
	if err != nil {
		return stats, fmt.Errorf("workerjob: stats: %w", err)
	}
	return stats, nil
}

// CleanupOld deletes records older than maxAge. Called by the hourly cleanup task.
func (r *WorkerJobRepo) CleanupOld(ctx context.Context, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-maxAge)
	tag, err := r.pool.Exec(ctx, `DELETE FROM sys_worker_jobs WHERE started_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("workerjob: cleanup: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildWorkerJobConditions creates WHERE clauses from a filter (shared by both repo variants).
func buildWorkerJobConditions(f workerjob.Filter) []squirrel.Sqlizer {
	conds := make([]squirrel.Sqlizer, 0, 4)
	if f.JobName != "" {
		conds = append(conds, squirrel.Eq{"job_name": f.JobName})
	}
	if f.JobCategory != "" {
		conds = append(conds, squirrel.Eq{"job_category": f.JobCategory})
	}
	if f.Status != "" {
		conds = append(conds, squirrel.Eq{"status": f.Status})
	}
	if f.DateFrom != nil {
		conds = append(conds, squirrel.GtOrEq{"started_at": *f.DateFrom})
	}
	if f.DateTo != nil {
		conds = append(conds, squirrel.LtOrEq{"started_at": *f.DateTo})
	}
	return conds
}

func marshalWorkerMeta(m map[string]any) ([]byte, error) {
	if len(m) == 0 {
		return nil, nil
	}
	return json.Marshal(m)
}

// Cursor encoding: simple base64 of RFC3339Nano timestamp.
// Production-grade: replace with keyset package when needed.
func encodeCursorTS(t time.Time) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano)))
}

func decodeCursorTS(s string) (time.Time, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, string(b))
}
