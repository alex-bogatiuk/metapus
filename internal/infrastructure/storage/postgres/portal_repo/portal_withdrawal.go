package portal_repo

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/infrastructure/http/v1/dto"
)

// ── Withdrawal Address Whitelist ──────────────────────────────────────

// ListWhitelistedAddresses returns all active whitelisted addresses for a merchant.
func (r *DashboardRepo) ListWhitelistedAddresses(ctx context.Context, merchantID id.ID) ([]dto.PortalWithdrawalAddress, error) {
	q := querier(ctx)

	const sqlQuery = `
		SELECT wa.id::text, wa.network_id::text, COALESCE(n.name, '') AS network,
		       wa.address, wa.label, wa.created_at
		FROM reg_withdrawal_addresses wa
		LEFT JOIN cat_blockchain_networks n ON n.id = wa.network_id
		WHERE wa.merchant_id = $1 AND wa._deleted_at IS NULL
		ORDER BY wa.created_at DESC
	`

	rows, err := q.Query(ctx, sqlQuery, merchantID)
	if err != nil {
		return nil, fmt.Errorf("list withdrawal addresses: %w", err)
	}
	defer rows.Close()

	items := make([]dto.PortalWithdrawalAddress, 0, 16)
	for rows.Next() {
		var item dto.PortalWithdrawalAddress
		var createdAt time.Time
		if err := rows.Scan(
			&item.ID, &item.NetworkID, &item.Network,
			&item.Address, &item.Label,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan withdrawal address: %w", err)
		}
		item.CreatedAt = createdAt.Format(time.RFC3339)
		items = append(items, item)
	}

	return items, rows.Err()
}

// AddWhitelistedAddress inserts a new whitelisted address.
// Returns the created address ID.
func (r *DashboardRepo) AddWhitelistedAddress(ctx context.Context, merchantID id.ID, req dto.PortalAddWithdrawalAddressRequest) (string, error) {
	q := querier(ctx)

	networkID, err := id.Parse(req.NetworkID)
	if err != nil {
		return "", fmt.Errorf("invalid network id: %w", err)
	}

	var newID string
	err = q.QueryRow(ctx, `
		INSERT INTO reg_withdrawal_addresses (merchant_id, network_id, address, label)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text
	`, merchantID, networkID, req.Address, req.Label).Scan(&newID)
	if err != nil {
		return "", fmt.Errorf("add withdrawal address: %w", err)
	}

	return newID, nil
}

// RemoveWhitelistedAddress soft-deletes a whitelisted address.
func (r *DashboardRepo) RemoveWhitelistedAddress(ctx context.Context, merchantID id.ID, addressID id.ID) error {
	q := querier(ctx)

	tag, err := q.Exec(ctx, `
		UPDATE reg_withdrawal_addresses
		SET _deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND merchant_id = $2 AND _deleted_at IS NULL
	`, addressID, merchantID)
	if err != nil {
		return fmt.Errorf("remove withdrawal address: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("withdrawal address not found")
	}
	return nil
}

// ── Withdrawal Requests ───────────────────────────────────────────────

// CreateWithdrawalRequest inserts a new withdrawal request.
// docID must come from the domain model (entity.NewDocument) — same ID used by Engine.Post() for movements.
// The document is created with posted=true and posted_version=1 (debit-first pattern).
func (r *DashboardRepo) CreateWithdrawalRequest(
	ctx context.Context,
	docID id.ID,
	merchantID id.ID,
	tokenID id.ID,
	amount int64,
	destAddress string,
	addressID id.ID,
	number string,
	posted bool,
	postedVersion int,
) error {
	q := querier(ctx)

	_, err := q.Exec(ctx, `
		INSERT INTO doc_withdrawal_requests (id, number, merchant_id, token_id, amount, dest_address, address_id, status, posted, posted_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending_approval', $8, $9)
	`, docID, number, merchantID, tokenID, amount, destAddress, addressID, posted, postedVersion)
	if err != nil {
		return fmt.Errorf("create withdrawal request: %w", err)
	}

	return nil
}

// ListWithdrawalRequests returns all withdrawal requests for the scoped merchants.
func (r *DashboardRepo) ListWithdrawalRequests(ctx context.Context, merchantIDs []id.ID) ([]dto.PortalWithdrawalRequestItem, int, error) {
	q := querier(ctx)

	const sqlQuery = `
		SELECT wr.id::text, wr.number, wr.status, wr.amount,
		       t.symbol, COALESCE(n.name, '') AS network, t.decimal_places,
		       wr.dest_address, wr.rejection_reason,
		       wr.created_at, wr.approved_at
		FROM doc_withdrawal_requests wr
		JOIN cat_tokens t ON t.id = wr.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE wr.merchant_id = ANY($1) AND wr._deleted_at IS NULL
		ORDER BY wr.created_at DESC
		LIMIT 200
	`

	rows, err := q.Query(ctx, sqlQuery, merchantIDs)
	if err != nil {
		return nil, 0, fmt.Errorf("list withdrawal requests: %w", err)
	}
	defer rows.Close()

	items := make([]dto.PortalWithdrawalRequestItem, 0, 32)
	for rows.Next() {
		var item dto.PortalWithdrawalRequestItem
		var amount int64
		var createdAt time.Time
		var approvedAt *time.Time
		var rejectionReason string
		if err := rows.Scan(
			&item.ID, &item.Number, &item.Status, &amount,
			&item.Symbol, &item.Network, &item.DecimalPlaces,
			&item.DestAddress, &rejectionReason,
			&createdAt, &approvedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan withdrawal request: %w", err)
		}
		item.Amount = fmt.Sprintf("%d", amount)
		item.CreatedAt = createdAt.Format(time.RFC3339)
		if approvedAt != nil {
			s := approvedAt.Format(time.RFC3339)
			item.ApprovedAt = &s
		}
		if rejectionReason != "" {
			item.RejectionReason = &rejectionReason
		}
		items = append(items, item)
	}

	return items, len(items), rows.Err()
}

// GetWhitelistedAddress returns a single whitelisted address by ID, validating merchant ownership.
func (r *DashboardRepo) GetWhitelistedAddress(ctx context.Context, merchantID id.ID, addressID id.ID) (dto.PortalWithdrawalAddress, error) {
	q := querier(ctx)

	var item dto.PortalWithdrawalAddress
	var createdAt time.Time
	err := q.QueryRow(ctx, `
		SELECT wa.id::text, wa.network_id::text, COALESCE(n.name, '') AS network,
		       wa.address, wa.label, wa.created_at
		FROM reg_withdrawal_addresses wa
		LEFT JOIN cat_blockchain_networks n ON n.id = wa.network_id
		WHERE wa.id = $1 AND wa.merchant_id = $2 AND wa._deleted_at IS NULL
	`, addressID, merchantID).Scan(
		&item.ID, &item.NetworkID, &item.Network,
		&item.Address, &item.Label,
		&createdAt,
	)
	if err != nil {
		return item, fmt.Errorf("get whitelisted address: %w", err)
	}
	item.CreatedAt = createdAt.Format(time.RFC3339)
	return item, nil
}

// GetWithdrawalRequestForStorno returns a withdrawal request with fields needed for storno.
// Uses FOR UPDATE to prevent concurrent storno race condition (CWE-362).
// MUST be called inside a transaction — the lock is released on COMMIT/ROLLBACK.
func (r *DashboardRepo) GetWithdrawalRequestForStorno(
	ctx context.Context, merchantIDs []id.ID, requestID id.ID,
) (docID id.ID, postedVersion int, status string, err error) {
	q := querier(ctx)

	err = q.QueryRow(ctx, `
		SELECT id, posted_version, status
		FROM doc_withdrawal_requests
		WHERE id = $1 AND merchant_id = ANY($2) AND _deleted_at IS NULL
		FOR UPDATE
	`, requestID, merchantIDs).Scan(&docID, &postedVersion, &status)
	if err != nil {
		return id.ID{}, 0, "", fmt.Errorf("get withdrawal request: %w", err)
	}
	return docID, postedVersion, status, nil
}

// RejectWithdrawalRequest updates a withdrawal request status to 'rejected'.
func (r *DashboardRepo) RejectWithdrawalRequest(
	ctx context.Context, requestID id.ID, reason string,
) error {
	q := querier(ctx)

	tag, err := q.Exec(ctx, `
		UPDATE doc_withdrawal_requests
		SET status = 'rejected', rejection_reason = $2, updated_at = NOW()
		WHERE id = $1 AND status = 'pending_approval'
	`, requestID, reason)
	if err != nil {
		return fmt.Errorf("reject withdrawal request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("withdrawal request not found or already processed")
	}
	return nil
}
