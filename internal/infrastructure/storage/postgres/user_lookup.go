package postgres

import (
	"context"
	"fmt"

	"metapus/internal/core/id"
)

// UserCard is how a configuration shows a platform user its own rows
// reference, such as the approver of a document.
type UserCard struct {
	ID    id.ID
	Email string
	// DisplayName is the user's first and last name, or the email when
	// both are blank.
	DisplayName string
}

// UserCards returns the cards of the users among userIDs that are not marked
// for deletion; a missing or marked user has no card.
func UserCards(ctx context.Context, userIDs []id.ID) (map[id.ID]UserCard, error) {
	cards := make(map[id.ID]UserCard, len(userIDs))
	if len(userIDs) == 0 {
		return cards, nil
	}
	rows, err := MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `
		SELECT id, email, COALESCE(NULLIF(BTRIM(CONCAT_WS(' ', first_name, last_name)), ''), email)
		FROM auth_users WHERE id = ANY($1) AND deletion_mark = FALSE
	`, userIDs)
	if err != nil {
		return nil, fmt.Errorf("read user cards: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var card UserCard
		if err := rows.Scan(&card.ID, &card.Email, &card.DisplayName); err != nil {
			return nil, fmt.Errorf("scan user card: %w", err)
		}
		cards[card.ID] = card
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read user cards: %w", err)
	}
	return cards, nil
}

// ActiveAdminIDs lists, in ID order, the system administrators who can sign
// in: active and not marked for deletion.
func ActiveAdminIDs(ctx context.Context) ([]id.ID, error) {
	rows, err := MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `
		SELECT id FROM auth_users
		WHERE is_admin = TRUE AND is_active = TRUE AND deletion_mark = FALSE
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("list active administrators: %w", err)
	}
	defer rows.Close()
	admins := []id.ID{}
	for rows.Next() {
		var admin id.ID
		if err := rows.Scan(&admin); err != nil {
			return nil, fmt.Errorf("scan active administrator: %w", err)
		}
		admins = append(admins, admin)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active administrators: %w", err)
	}
	return admins, nil
}
