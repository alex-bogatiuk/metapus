package portal_repo

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/application/portalapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/domain/documents/withdrawal_request"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	platformtelemetry "metapus/internal/platform/telemetry"
)

// ── Withdrawal Address Whitelist ──────────────────────────────────────

// ListWhitelistedAddresses returns all active whitelisted addresses for a merchant.
func (r *DashboardRepo) ListWhitelistedAddresses(ctx context.Context, merchantID id.ID) ([]portalapp.PortalWithdrawalAddress, error) {
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

	items := make([]portalapp.PortalWithdrawalAddress, 0, 16)
	for rows.Next() {
		var item portalapp.PortalWithdrawalAddress
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
func (r *DashboardRepo) AddWithdrawalAddress(ctx context.Context, merchantID, networkID id.ID, address, addressKey, label string) (id.ID, error) {
	q := querier(ctx)
	if addressKey == "" {
		addressKey = address
	}
	var newID id.ID
	err := q.QueryRow(ctx, `
		INSERT INTO reg_withdrawal_addresses (merchant_id, network_id, address, address_key, label)
		SELECT $1, $2, $3, $4, $5
		WHERE NOT EXISTS (
			SELECT 1
			FROM cat_wallets w
			JOIN cat_blockchain_networks n ON n.id = w.network_id
			WHERE w.network_id = $2
			  AND w.deletion_mark = FALSE
			  AND w.address_key = $4
		)
		RETURNING id::text
	`, merchantID, networkID, address, addressKey, label).Scan(&newID)
	if err == pgx.ErrNoRows {
		return id.Nil(), apperror.NewValidation("withdrawal destination must be external to managed custody").WithDetail("field", "address")
	}
	if err != nil {
		return id.Nil(), fmt.Errorf("add withdrawal address: %w", err)
	}

	return newID, nil
}

// RemoveWhitelistedAddress soft-deletes a whitelisted address.
func (r *DashboardRepo) RemoveWithdrawalAddress(ctx context.Context, merchantID id.ID, addressID id.ID) error {
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
func (r *DashboardRepo) CreateWithdrawalRequest(ctx context.Context, doc *withdrawal_request.WithdrawalRequest, riskClass aml.Class) error {
	q := querier(ctx)
	_, err := q.Exec(ctx, `
		INSERT INTO doc_withdrawal_requests (id, number, merchant_id, token_id, amount, dest_address, address_id, status, posted, posted_version,risk_class,basis_type,basis_id,description)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending_signature', $8, $9,NULLIF($10,''),$11,$12,$13)
	`, doc.GetID(), doc.Number, doc.MerchantID, doc.TokenID, doc.Amount, doc.DestAddress, doc.AddressID, doc.IsPosted(), doc.GetPostedVersion(), riskClass, doc.BasisType, doc.BasisID, doc.Description)
	if err != nil {
		return fmt.Errorf("create withdrawal request: %w", err)
	}
	return nil
}

// refundablePaymentQuery reads a credited payment with what its refunds have
// already committed: net request debits, so a rejected or failed refund that
// restored its debit frees its amount again. The database guard on
// doc_withdrawal_requests enforces the same cap.
const refundablePaymentQuery = `SELECT p.id,p.invoice_id,COALESCE(i.number,''),p.token_id,p.amount,p.from_address,
 COALESCE((SELECT SUM(CASE WHEN m.record_type='expense' THEN m.amount ELSE -m.amount END)
   FROM doc_withdrawal_requests r JOIN reg_crypto_merchant_balance_movements m ON m.recorder_id=r.id AND m.recorder_type='WithdrawalRequest'
   WHERE r.basis_type='CryptoPayment' AND r.basis_id=p.id),0)
 FROM doc_crypto_payments p LEFT JOIN doc_crypto_invoices i ON i.id=p.invoice_id
 WHERE p.id=$2 AND p.merchant_id=$1 AND p.posted AND p.canonicality='canonical' AND p.status IN ('confirmed','settled')
   AND NOT p.deletion_mark AND p._deleted_at IS NULL`

// GetRefundablePayment reads what is left to refund of a merchant's payment.
func (r *DashboardRepo) GetRefundablePayment(ctx context.Context, merchantID, paymentID id.ID) (cryptoapp.RefundablePayment, error) {
	return scanRefundablePayment(querier(ctx).QueryRow(ctx, refundablePaymentQuery, merchantID, paymentID))
}

// LockRefundablePayment locks the payment before the refund debit is posted,
// so concurrent refunds of one payment are checked one after another.
func (r *DashboardRepo) LockRefundablePayment(ctx context.Context, merchantID, paymentID id.ID) (cryptoapp.RefundablePayment, error) {
	return scanRefundablePayment(querier(ctx).QueryRow(ctx, refundablePaymentQuery+` FOR UPDATE OF p`, merchantID, paymentID))
}

func scanRefundablePayment(row pgx.Row) (cryptoapp.RefundablePayment, error) {
	var p cryptoapp.RefundablePayment
	if err := row.Scan(&p.PaymentID, &p.InvoiceID, &p.InvoiceNumber, &p.TokenID, &p.Amount, &p.FromAddress, &p.Committed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return p, apperror.NewNotFound("crypto_payment", "").WithCause(err)
		}
		return p, fmt.Errorf("read refundable payment: %w", err)
	}
	return p, nil
}

func (r *DashboardRepo) EnqueueApprovalSigning(ctx context.Context, intent crypto.MoneyMovementIntent) error {
	payload, err := json.MarshalDeterministic(intent)
	if err != nil {
		return fmt.Errorf("encode withdrawal signing intent: %w", err)
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(payload))
	_, err = querier(ctx).Exec(ctx, `INSERT INTO reg_money_movement_signing_jobs
		(basis_type,basis_id,intent_payload,intent_fingerprint,traceparent,correlation_id,causation_id)
		VALUES ('WithdrawalRequest',$1,$2::jsonb,$3,$4,$5,$6)`,
		intent.BasisID, string(payload), fingerprint,
		platformtelemetry.Traceparent(ctx), intent.IntentID, intent.BasisID.String())
	if err != nil {
		return fmt.Errorf("enqueue withdrawal approval signing: %w", err)
	}
	return nil
}

// MarkWithdrawalRequestApprovedByPolicy transitions a request when the signed
// approval policy requires no admin principal, e.g. risk-only withdrawals.
func (r *DashboardRepo) MarkWithdrawalRequestApprovedByPolicy(ctx context.Context, requestID id.ID) error {
	q := querier(ctx)
	tag, err := q.Exec(ctx, `
		UPDATE doc_withdrawal_requests
		SET status = 'approved',
		    approved_by = NULL,
		    approved_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
		  AND status = 'pending_signature'
		  AND _deleted_at IS NULL
	`, requestID)
	if err != nil {
		return fmt.Errorf("policy approve withdrawal request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("withdrawal request not found or not pending approval")
	}
	return nil
}

// ListWithdrawalRequests returns all withdrawal requests for the scoped merchants.
func (r *DashboardRepo) ListWithdrawalRequests(ctx context.Context, merchantIDs []id.ID) ([]portalapp.PortalWithdrawalRequestItem, int, error) {
	q := querier(ctx)

	const sqlQuery = `
		SELECT wr.id::text, wr.number, wr.status, wr.amount,
		       t.symbol, COALESCE(n.name, '') AS network, t.decimal_places,
		       wr.dest_address, wr.rejection_reason,
		       wr.created_at, wr.approved_at,` + amlWithdrawalHistoryColumns + `,
		       CASE WHEN wr.basis_type='CryptoPayment' THEN wr.basis_id::text END, CASE WHEN wr.basis_type='CryptoPayment' THEN wr.description END
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

	items := make([]portalapp.PortalWithdrawalRequestItem, 0, 32)
	for rows.Next() {
		var item portalapp.PortalWithdrawalRequestItem
		var amount types.RawAmount
		var createdAt time.Time
		var approvedAt *time.Time
		var rejectionReason string
		var riskSources []byte
		if err := rows.Scan(
			&item.ID, &item.Number, &item.Status, &amount,
			&item.Symbol, &item.Network, &item.DecimalPlaces,
			&item.DestAddress, &rejectionReason,
			&createdAt, &approvedAt, &item.RiskClass, &item.RiskAccepted, &riskSources,
			&item.RefundPaymentID, &item.RefundReason,
		); err != nil {
			return nil, 0, fmt.Errorf("scan withdrawal request: %w", err)
		}
		if err := json.Unmarshal(riskSources, &item.Sources); err != nil {
			return nil, 0, err
		}
		item.Amount = amount.String()
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

// ListAdminWithdrawalRequests returns withdrawal requests for ERP/admin approval and monitoring.
func (r *DashboardRepo) ListAdminWithdrawalRequests(ctx context.Context, status string, limit int) ([]portalapp.AdminWithdrawalRequestItem, int, error) {
	q := querier(ctx)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if status == "" {
		status = "all"
	}

	const baseQuery = `
		SELECT wr.id::text, wr.number, wr.date, wr.status,
		       wr.merchant_id::text, COALESCE(m.name, '') AS merchant_name,
		       wr.token_id::text, t.symbol, COALESCE(n.name, '') AS network,
		       wr.amount, t.decimal_places, wr.dest_address,
		       wr.approved_by::text,
		       NULLIF(COALESCE(NULLIF(BTRIM(CONCAT_WS(' ', au.first_name, au.last_name)), ''), au.email, ''), '') AS approved_by_name,
		       wr.approved_at, wr.rejection_reason,
		       wr.withdrawal_id::text, wr.created_at,
		       COUNT(*) OVER() AS total_count,` + amlWithdrawalHistoryColumns + `
		FROM doc_withdrawal_requests wr
		JOIN cat_merchants m ON m.id = wr.merchant_id
		JOIN cat_tokens t ON t.id = wr.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		LEFT JOIN auth_users au ON au.id = wr.approved_by AND au.deletion_mark = FALSE
		WHERE wr._deleted_at IS NULL
		  AND (
		    $1 = 'all'
		    OR ($1 = 'active' AND wr.status NOT IN ('confirmed','rejected','failed'))
		    OR wr.status = $1
		  )
		ORDER BY wr.created_at DESC
		LIMIT $2
	`

	rows, err := q.Query(ctx, baseQuery, status, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list admin withdrawal requests: %w", err)
	}
	defer rows.Close()

	items := make([]portalapp.AdminWithdrawalRequestItem, 0, limit)
	total := 0
	for rows.Next() {
		item, rowTotal, err := scanAdminWithdrawalRequestListItem(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan admin withdrawal request: %w", err)
		}
		total = rowTotal
		items = append(items, item)
	}

	return items, total, rows.Err()
}

// GetAdminWithdrawalRequest returns one ERP/admin withdrawal request read model.
func (r *DashboardRepo) GetAdminWithdrawalRequest(ctx context.Context, requestID id.ID) (portalapp.AdminWithdrawalRequestDetail, error) {
	q := querier(ctx)

	const sqlQuery = `
		SELECT wr.id::text, wr.deletion_mark, wr.version, wr.attributes,
		       wr.created_at, wr.updated_at, wr.created_by::text, wr.updated_by::text,
		       wr.number, wr.date, wr.posted, wr.posted_version,
		       wr.basis_type, wr.basis_id::text, wr.description, wr.status,
		       wr.merchant_id::text, COALESCE(m.name, '') AS merchant_name,
		       wr.token_id::text, t.symbol, COALESCE(n.name, '') AS network,
		       wr.amount, t.decimal_places, wr.dest_address,
		       wr.address_id::text,
		       wr.approved_by::text,
		       NULLIF(COALESCE(NULLIF(BTRIM(CONCAT_WS(' ', au.first_name, au.last_name)), ''), au.email, ''), '') AS approved_by_name,
		       wr.approved_at, wr.rejection_reason,
		       wr.withdrawal_id::text,` + amlWithdrawalHistoryColumns + `
		FROM doc_withdrawal_requests wr
		JOIN cat_merchants m ON m.id = wr.merchant_id
		JOIN cat_tokens t ON t.id = wr.token_id
		LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id
		LEFT JOIN auth_users au ON au.id = wr.approved_by AND au.deletion_mark = FALSE
		WHERE wr.id = $1 AND wr._deleted_at IS NULL
	`

	item, err := scanAdminWithdrawalRequestDetail(q.QueryRow(ctx, sqlQuery, requestID))
	if err != nil {
		return item, fmt.Errorf("get admin withdrawal request: %w", err)
	}
	return item, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAdminWithdrawalRequestListItem(row rowScanner) (portalapp.AdminWithdrawalRequestItem, int, error) {
	var item portalapp.AdminWithdrawalRequestItem
	var amount types.RawAmount
	var approvedByID *string
	var approvedByName *string
	var approvedAt *time.Time
	var rejectionReason string
	var withdrawalID *string
	var createdAt time.Time
	var date time.Time
	var total int
	var riskSources []byte
	if err := row.Scan(
		&item.ID, &item.Number, &date, &item.Status,
		&item.MerchantID, &item.MerchantName,
		&item.TokenID, &item.Symbol, &item.Network,
		&amount, &item.DecimalPlaces, &item.DestAddress,
		&approvedByID, &approvedByName, &approvedAt, &rejectionReason,
		&withdrawalID, &createdAt, &total, &item.RiskClass, &item.RiskAccepted, &riskSources,
	); err != nil {
		return item, 0, err
	}
	if err := json.Unmarshal(riskSources, &item.Sources); err != nil {
		return item, 0, err
	}
	item.Amount = amount.String()
	item.ApprovedByID = approvedByID
	item.ApprovedByName = approvedByName
	item.WithdrawalID = withdrawalID
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.Date = date.Format(time.RFC3339)
	if approvedAt != nil {
		s := approvedAt.Format(time.RFC3339)
		item.ApprovedAt = &s
	}
	if rejectionReason != "" {
		item.RejectionReason = &rejectionReason
	}
	return item, total, nil
}

func scanAdminWithdrawalRequestDetail(row rowScanner) (portalapp.AdminWithdrawalRequestDetail, error) {
	var item portalapp.AdminWithdrawalRequestDetail
	var amount types.RawAmount
	var createdAt, updatedAt, date time.Time
	var approvedAt *time.Time
	var rejectionReason string
	var riskSources []byte
	if err := row.Scan(
		&item.ID, &item.DeletionMark, &item.Version, &item.Attributes,
		&createdAt, &updatedAt, &item.CreatedBy, &item.UpdatedBy,
		&item.Number, &date, &item.Posted, &item.PostedVersion,
		&item.BasisType, &item.BasisID, &item.Description, &item.Status,
		&item.MerchantID, &item.MerchantName,
		&item.TokenID, &item.Symbol, &item.Network,
		&amount, &item.DecimalPlaces, &item.DestAddress,
		&item.AddressID, &item.ApprovedByID, &item.ApprovedByName, &approvedAt, &rejectionReason,
		&item.WithdrawalID, &item.RiskClass, &item.RiskAccepted, &riskSources,
	); err != nil {
		return item, err
	}
	if err := json.Unmarshal(riskSources, &item.Sources); err != nil {
		return item, err
	}
	item.Amount = amount.String()
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.Date = date.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	item.ApprovedBy = item.ApprovedByID
	if approvedAt != nil {
		s := approvedAt.Format(time.RFC3339)
		item.ApprovedAt = &s
	}
	if rejectionReason != "" {
		item.RejectionReason = &rejectionReason
	}
	return item, nil
}

// ApproveWithdrawalRequest atomically transitions a pending request to approved.
func (r *DashboardRepo) ApproveWithdrawalRequest(ctx context.Context, requestID id.ID, approvedBy id.ID) error {
	q := querier(ctx)

	tag, err := q.Exec(ctx, `
		UPDATE doc_withdrawal_requests
		SET status = 'approved',
		    approved_by = $2,
		    approved_at = NOW(),
		    rejection_reason = '',
		    updated_at = NOW(),
		    version = version + 1
		WHERE id = $1
		  AND status = 'pending_approval'
		  AND _deleted_at IS NULL
	`, requestID, approvedBy)
	if err != nil {
		return fmt.Errorf("approve withdrawal request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("withdrawal request not found or not pending approval")
	}
	return nil
}

func (r *DashboardRepo) LockWithdrawalRequestForApproval(ctx context.Context, requestID id.ID) (withdrawal_request.Status, error) {
	var status withdrawal_request.Status
	if err := crypto_repo.NewAMLRepository().LockAMLWithdrawalRequest(ctx, requestID); err != nil {
		return "", err
	}
	err := querier(ctx).QueryRow(ctx, `
		SELECT status
		FROM doc_withdrawal_requests
		WHERE id = $1 AND _deleted_at IS NULL
		FOR UPDATE
	`, requestID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperror.NewNotFound("withdrawal request", requestID.String())
	}
	if err != nil {
		return "", fmt.Errorf("lock withdrawal request for approval: %w", err)
	}
	return status, nil
}

func (r *DashboardRepo) GetWithdrawalTokenCurrency(ctx context.Context, tokenID id.ID) (*id.ID, error) {
	var raw *string
	err := querier(ctx).QueryRow(ctx, `SELECT currency_id::text FROM cat_tokens WHERE id = $1 AND deletion_mark = FALSE`, tokenID).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("get withdrawal token currency: %w", err)
	}
	if raw == nil {
		return nil, nil
	}
	currencyID, err := id.Parse(*raw)
	if err != nil {
		return nil, fmt.Errorf("parse withdrawal token currency: %w", err)
	}
	return &currencyID, nil
}

func (r *DashboardRepo) LockWithdrawalRequestForAdminRejection(ctx context.Context, requestID id.ID) (id.ID, int, withdrawal_request.Status, error) {
	if err := crypto_repo.NewAMLRepository().LockAMLWithdrawalRequest(ctx, requestID); err != nil {
		return id.Nil(), 0, "", err
	}
	var docID id.ID
	var postedVersion int
	var status withdrawal_request.Status
	err := querier(ctx).QueryRow(ctx, `
		SELECT id, posted_version, status FROM doc_withdrawal_requests
		WHERE id = $1 AND _deleted_at IS NULL FOR UPDATE
	`, requestID).Scan(&docID, &postedVersion, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return id.ID{}, 0, "", apperror.NewNotFound("withdrawal request", requestID.String())
	}
	if err != nil {
		return id.ID{}, 0, "", fmt.Errorf("lock withdrawal request for admin rejection: %w", err)
	}
	return docID, postedVersion, status, nil
}

func (r *DashboardRepo) RejectAdminWithdrawalRequest(ctx context.Context, requestID, actorID id.ID, reason string) error {
	tag, err := querier(ctx).Exec(ctx, `
		UPDATE doc_withdrawal_requests
		SET status = 'rejected', rejection_reason = $3, updated_by = $2,
		    updated_at = NOW(), version = version + 1
		WHERE id = $1 AND status IN ('pending_signature','pending_approval') AND _deleted_at IS NULL
	`, requestID, actorID, reason)
	if err != nil {
		return fmt.Errorf("reject admin withdrawal request: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apperror.NewConflict("withdrawal request is not pending approval")
	}
	return nil
}

// GetWhitelistedAddress returns a single whitelisted address by ID, validating merchant ownership.
func (r *DashboardRepo) GetWhitelistedAddress(ctx context.Context, merchantID id.ID, addressID id.ID) (portalapp.PortalWithdrawalAddress, error) {
	q := querier(ctx)

	var item portalapp.PortalWithdrawalAddress
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

// GetWithdrawalAddressContext implements the application withdrawal port
// without leaking HTTP DTOs into the use case.
func (r *DashboardRepo) GetWithdrawalAddressContext(ctx context.Context, merchantID id.ID, addressID id.ID) (cryptoapp.PortalWithdrawalAddress, error) {
	q := querier(ctx)
	var result cryptoapp.PortalWithdrawalAddress
	err := q.QueryRow(ctx, `
		SELECT network_id, address
		FROM reg_withdrawal_addresses
		WHERE id = $1 AND merchant_id = $2 AND _deleted_at IS NULL
	`, addressID, merchantID).Scan(&result.NetworkID, &result.Address)
	if err != nil {
		return result, fmt.Errorf("get withdrawal address context: %w", err)
	}
	return result, nil
}

// GetWithdrawalTokenContext implements the application withdrawal port.
func (r *DashboardRepo) GetWithdrawalTokenContext(ctx context.Context, tokenID id.ID) (cryptoapp.PortalWithdrawalToken, error) {
	q := querier(ctx)
	var result cryptoapp.PortalWithdrawalToken
	err := q.QueryRow(ctx, `
		SELECT t.network_id, COALESCE(n.code, ''), t.decimal_places,
		       t.token_standard, COALESCE(t.contract_address, '')
		FROM cat_tokens t
		JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE t.id = $1 AND t.deletion_mark = FALSE
	`, tokenID).Scan(&result.NetworkID, &result.NetworkCode, &result.DecimalPlaces, &result.TokenStandard, &result.ContractAddress)
	if err != nil {
		return result, fmt.Errorf("get withdrawal token context: %w", err)
	}
	return result, nil
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

// LockWithdrawalRequestForRejection locks a merchant-scoped request for the
// compensating application command.
func (r *DashboardRepo) LockWithdrawalRequestForRejection(
	ctx context.Context, merchantID id.ID, requestID id.ID,
) (docID id.ID, postedVersion int, status withdrawal_request.Status, err error) {
	q := querier(ctx)
	if err := crypto_repo.NewAMLRepository().LockAMLWithdrawalRequest(ctx, requestID); err != nil {
		return id.Nil(), 0, "", err
	}
	err = q.QueryRow(ctx, `
		SELECT id, posted_version, status
		FROM doc_withdrawal_requests
		WHERE id = $1 AND merchant_id = $2 AND _deleted_at IS NULL
		FOR UPDATE
	`, requestID, merchantID).Scan(&docID, &postedVersion, &status)
	if err != nil {
		return id.ID{}, 0, "", fmt.Errorf("lock withdrawal request for rejection: %w", err)
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
		WHERE id = $1 AND status IN ('pending_signature','pending_approval')
	`, requestID, reason)
	if err != nil {
		return fmt.Errorf("reject withdrawal request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("withdrawal request not found or already processed")
	}
	return nil
}
