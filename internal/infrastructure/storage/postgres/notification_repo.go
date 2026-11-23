package postgres

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/domain/notifications"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/tenantdb"
)

type NotificationRepo struct{}

func NewNotificationRepo() *NotificationRepo {
	return &NotificationRepo{}
}

func (r *NotificationRepo) getQuerier(ctx context.Context) (Querier, error) {
	txm, err := tenant.GetTxManager(ctx)
	if err == nil {
		if postgresTxm, ok := txm.(*TxManager); ok && postgresTxm != nil {
			return postgresTxm.GetQuerier(ctx), nil
		}
		return nil, fmt.Errorf("notification repo: unexpected TxManager type %T", txm)
	}
	if err != tenant.ErrNoTxManager {
		return nil, err
	}
	return tenantdb.GetPool(ctx)
}

func (r *NotificationRepo) Create(ctx context.Context, n *notifications.Notification) error {
	q, err := r.getQuerier(ctx)
	if err != nil {
		return err
	}

	if n.ID == nil {
		newID := id.New()
		n.ID = &newID
	}

	if n.Severity == "" {
		n.Severity = notifications.SeverityInfo
	}

	query, args, err := sq.Insert("sys_notifications").
		Columns("id", "user_id", "title", "message", "severity", "link", "is_read", "attributes").
		Values(n.ID.String(), n.UserID.String(), n.Title, n.Message, string(n.Severity), n.Link, n.IsRead, n.Attributes).
		Suffix("RETURNING created_at, updated_at, version").
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return err
	}

	return q.QueryRow(ctx, query, args...).Scan(&n.CreatedAt, &n.UpdatedAt, &n.Version)
}

// CreateBatch inserts the notifications in one batch, through the current
// transaction when ctx carries one.
func (r *NotificationRepo) CreateBatch(ctx context.Context, notifs []*notifications.Notification) (retErr error) {
	if len(notifs) == 0 {
		return nil
	}
	q, err := r.getQuerier(ctx)
	if err != nil {
		return err
	}

	b := &pgx.Batch{}

	for _, n := range notifs {
		if n.ID == nil {
			newID := id.New()
			n.ID = &newID
		}

		if n.Severity == "" {
			n.Severity = notifications.SeverityInfo
		}

		query, args, err := sq.Insert("sys_notifications").
			Columns("id", "user_id", "title", "message", "severity", "link", "is_read", "attributes").
			Values(n.ID.String(), n.UserID.String(), n.Title, n.Message, string(n.Severity), n.Link, n.IsRead, n.Attributes).
			PlaceholderFormat(sq.Dollar).
			ToSql()
		if err != nil {
			return err
		}
		b.Queue(query, args...)
	}

	results := q.SendBatch(ctx, b)
	defer func() {
		if err := results.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("close notification batch: %w", err)
		}
	}()

	for i := range notifs {
		_, err := results.Exec()
		if err != nil {
			return fmt.Errorf("error inserting notification batch at index %d: %w", i, err)
		}
	}

	return nil
}

// UsersNotifiedWith lists the users who have a notification whose attribute
// key holds value, including notifications they read or deleted, so that a
// caller delivering one event can skip the users a previous delivery reached.
func (r *NotificationRepo) UsersNotifiedWith(ctx context.Context, key, value string) ([]id.ID, error) {
	q, err := r.getQuerier(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT DISTINCT user_id FROM sys_notifications WHERE attributes->>$1 = $2`, key, value)
	if err != nil {
		return nil, fmt.Errorf("list notified users: %w", err)
	}
	defer rows.Close()
	users := []id.ID{}
	for rows.Next() {
		var user id.ID
		if err := rows.Scan(&user); err != nil {
			return nil, fmt.Errorf("scan notified user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list notified users: %w", err)
	}
	return users, nil
}

func (r *NotificationRepo) GetByID(ctx context.Context, notifID id.ID) (*notifications.Notification, error) {
	pool, err := tenantdb.GetPool(ctx)
	if err != nil {
		return nil, err
	}

	query, args, err := sq.Select("id", "user_id", "title", "message", "severity", "link", "is_read", "attributes", "version", "deletion_mark", "created_at", "updated_at").
		From("sys_notifications").
		Where(sq.Eq{"id": notifID.String(), "deletion_mark": false}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return nil, err
	}

	var n notifications.Notification
	var rawAttributes map[string]any
	err = pool.QueryRow(ctx, query, args...).Scan(
		&n.ID, &n.UserID, &n.Title, &n.Message, &n.Severity, &n.Link, &n.IsRead, &rawAttributes, &n.Version, &n.DeletionMark, &n.CreatedAt, &n.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	n.Attributes = rawAttributes

	return &n, nil
}

func (r *NotificationRepo) List(ctx context.Context, filter *notifications.NotificationFilter) ([]*notifications.Notification, error) {
	pool, err := tenantdb.GetPool(ctx)
	if err != nil {
		return nil, err
	}

	q := sq.Select("id", "user_id", "title", "message", "severity", "link", "is_read", "attributes", "version", "deletion_mark", "created_at", "updated_at").
		From("sys_notifications").
		Where(sq.Eq{"user_id": filter.UserID.String(), "deletion_mark": false})

	if filter.UnreadOnly {
		q = q.Where(sq.Eq{"is_read": false})
	}

	q = q.OrderBy("created_at DESC")

	if filter.Limit > 0 {
		q = q.Limit(uint64(filter.Limit))
	} else {
		q = q.Limit(50) // default limit
	}
	if filter.Offset > 0 {
		q = q.Offset(uint64(filter.Offset))
	}

	query, args, err := q.PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notifs []*notifications.Notification
	for rows.Next() {
		var n notifications.Notification
		var rawAttributes map[string]any
		if err := rows.Scan(
			&n.ID, &n.UserID, &n.Title, &n.Message, &n.Severity, &n.Link, &n.IsRead, &rawAttributes, &n.Version, &n.DeletionMark, &n.CreatedAt, &n.UpdatedAt,
		); err != nil {
			return nil, err
		}
		n.Attributes = rawAttributes
		notifs = append(notifs, &n)
	}

	return notifs, rows.Err()
}

func (r *NotificationRepo) CountUnread(ctx context.Context, userID id.ID) (int, error) {
	pool, err := tenantdb.GetPool(ctx)
	if err != nil {
		return 0, err
	}

	query, args, err := sq.Select("COUNT(*)").
		From("sys_notifications").
		Where(sq.Eq{
			"user_id":       userID.String(),
			"is_read":       false,
			"deletion_mark": false,
		}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return 0, err
	}

	var count int
	err = pool.QueryRow(ctx, query, args...).Scan(&count)
	return count, err
}

func (r *NotificationRepo) MarkAsRead(ctx context.Context, notifID id.ID, userID id.ID) error {
	pool, err := tenantdb.GetPool(ctx)
	if err != nil {
		return err
	}

	query, args, err := sq.Update("sys_notifications").
		Set("is_read", true).
		Set("version", sq.Expr("version + 1")).
		Where(sq.Eq{
			"id":      notifID.String(),
			"user_id": userID.String(), // verify ownership
		}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return err
	}

	cmd, err := pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("notification not found or access denied")
	}

	return nil
}

func (r *NotificationRepo) MarkAsUnread(ctx context.Context, notifID id.ID, userID id.ID) error {
	pool, err := tenantdb.GetPool(ctx)
	if err != nil {
		return err
	}

	query, args, err := sq.Update("sys_notifications").
		Set("is_read", false).
		Set("version", sq.Expr("version + 1")).
		Where(sq.Eq{
			"id":      notifID.String(),
			"user_id": userID.String(),
		}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return err
	}

	cmd, err := pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("notification not found or access denied")
	}

	return nil
}

func (r *NotificationRepo) MarkAllAsRead(ctx context.Context, userID id.ID) error {
	pool, err := tenantdb.GetPool(ctx)
	if err != nil {
		return err
	}

	query, args, err := sq.Update("sys_notifications").
		Set("is_read", true).
		Set("version", sq.Expr("version + 1")).
		Where(sq.Eq{
			"user_id":       userID.String(),
			"is_read":       false,
			"deletion_mark": false,
		}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, query, args...)
	return err
}

func (r *NotificationRepo) Delete(ctx context.Context, notifID id.ID, userID id.ID) error {
	pool, err := tenantdb.GetPool(ctx)
	if err != nil {
		return err
	}

	query, args, err := sq.Update("sys_notifications").
		Set("deletion_mark", true).
		Set("version", sq.Expr("version + 1")).
		Where(sq.Eq{
			"id":      notifID.String(),
			"user_id": userID.String(),
		}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return err
	}

	cmd, err := pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("notification not found or access denied")
	}

	return nil
}
