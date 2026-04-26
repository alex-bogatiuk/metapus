package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/automations"
)

// AutomationChannelRepo implements automations.ChannelRepository.
type AutomationChannelRepo struct{}

// NewAutomationChannelRepo creates a new repository.
func NewAutomationChannelRepo() *AutomationChannelRepo {
	return &AutomationChannelRepo{}
}

const channelSelectCols = `c.id, c.name, c.account_id, c.destination, c.is_active, c.deletion_mark, c.version, c.created_at, c.updated_at`

// List returns all non-deleted channels, optionally filtered by accountID.
func (r *AutomationChannelRepo) List(ctx context.Context, accountID *id.ID) ([]automations.Channel, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := fmt.Sprintf(`
		SELECT %s,
			a.name AS account_name,
			a.account_type,
			(SELECT COUNT(*) FROM sys_automation_subscribers s WHERE s.channel_id = c.id) AS rule_count
		FROM sys_automation_channels c
		JOIN sys_automation_accounts a ON a.id = c.account_id
		WHERE c.deletion_mark = FALSE
			AND ($1::uuid IS NULL OR c.account_id = $1)
		ORDER BY c.created_at DESC
	`, channelSelectCols)

	rows, err := q.Query(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("query list channels: %w", err)
	}
	defer rows.Close()

	var channels []automations.Channel
	for rows.Next() {
		ch, err := scanChannelRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		channels = append(channels, *ch)
	}

	return channels, nil
}

// GetByID retrieves a channel by ID.
func (r *AutomationChannelRepo) GetByID(ctx context.Context, channelID id.ID) (*automations.Channel, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := fmt.Sprintf(`
		SELECT %s,
			a.name AS account_name,
			a.account_type,
			(SELECT COUNT(*) FROM sys_automation_subscribers s WHERE s.channel_id = c.id) AS rule_count
		FROM sys_automation_channels c
		JOIN sys_automation_accounts a ON a.id = c.account_id
		WHERE c.id = $1 AND c.deletion_mark = FALSE
	`, channelSelectCols)

	ch, err := scanChannelRow(q.QueryRow(ctx, query, channelID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewNotFound("sys_automation_channels", channelID)
		}
		return nil, fmt.Errorf("query channel by id: %w", err)
	}

	return ch, nil
}

// Create creates a new channel.
func (r *AutomationChannelRepo) Create(ctx context.Context, req automations.CreateChannelRequest) (*automations.Channel, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	destBytes, err := json.Marshal(req.Destination)
	if err != nil {
		return nil, fmt.Errorf("marshal destination: %w", err)
	}

	query := `
		INSERT INTO sys_automation_channels (name, account_id, destination, is_active)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, account_id, destination, is_active, deletion_mark, version, created_at, updated_at
	`

	var ch automations.Channel
	var destScanBytes []byte
	err = q.QueryRow(ctx, query,
		req.Name, req.AccountID, destBytes, req.IsActive,
	).Scan(
		&ch.ID, &ch.Name, &ch.AccountID, &destScanBytes,
		&ch.IsActive, &ch.DeletionMark, &ch.Version, &ch.CreatedAt, &ch.UpdatedAt,
	)
	if err != nil {
		if IsForeignKeyViolation(err) {
			return nil, apperror.NewValidation("Referenced account does not exist.")
		}
		return nil, fmt.Errorf("create automation channel: %w", err)
	}

	if err := json.Unmarshal(destScanBytes, &ch.Destination); err != nil {
		return nil, fmt.Errorf("unmarshal destination: %w", err)
	}

	return &ch, nil
}

// Update modifies an existing channel.
func (r *AutomationChannelRepo) Update(ctx context.Context, channelID id.ID, req automations.UpdateChannelRequest) (*automations.Channel, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	destBytes, err := json.Marshal(req.Destination)
	if err != nil {
		return nil, fmt.Errorf("marshal destination: %w", err)
	}

	query := `
		UPDATE sys_automation_channels
		SET name = $1, account_id = $2, destination = $3, is_active = $4, version = version + 1
		WHERE id = $5 AND version = $6 AND deletion_mark = FALSE
		RETURNING id, name, account_id, destination, is_active, deletion_mark, version, created_at, updated_at
	`

	var ch automations.Channel
	var destScanBytes []byte
	err = q.QueryRow(ctx, query,
		req.Name, req.AccountID, destBytes, req.IsActive,
		channelID, req.Version,
	).Scan(
		&ch.ID, &ch.Name, &ch.AccountID, &destScanBytes,
		&ch.IsActive, &ch.DeletionMark, &ch.Version, &ch.CreatedAt, &ch.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewConcurrentModification("sys_automation_channels", channelID)
		}
		return nil, fmt.Errorf("update automation channel: %w", err)
	}

	if err := json.Unmarshal(destScanBytes, &ch.Destination); err != nil {
		return nil, fmt.Errorf("unmarshal destination: %w", err)
	}

	return &ch, nil
}

// Delete marks a channel for soft deletion.
func (r *AutomationChannelRepo) Delete(ctx context.Context, channelID id.ID) error {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `UPDATE sys_automation_channels SET deletion_mark = TRUE WHERE id = $1 AND deletion_mark = FALSE`
	cmdTag, err := q.Exec(ctx, query, channelID)
	if err != nil {
		if IsForeignKeyViolation(err) {
			return apperror.NewValidation("Cannot delete channel that is referenced by active subscribers.")
		}
		return fmt.Errorf("delete automation channel: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return apperror.NewNotFound("sys_automation_channels", channelID)
	}

	return nil
}

// scanChannelRow scans a pgx row into a Channel with denormalized account info.
func scanChannelRow(row pgx.Row) (*automations.Channel, error) {
	var ch automations.Channel
	var destBytes []byte
	err := row.Scan(
		&ch.ID, &ch.Name, &ch.AccountID, &destBytes,
		&ch.IsActive, &ch.DeletionMark, &ch.Version, &ch.CreatedAt, &ch.UpdatedAt,
		&ch.AccountName, &ch.AccountType,
		&ch.RuleCount,
	)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(destBytes, &ch.Destination); err != nil {
		return nil, fmt.Errorf("unmarshal destination: %w", err)
	}
	return &ch, nil
}

// Ensure interface compliance
var _ automations.ChannelRepository = (*AutomationChannelRepo)(nil)
