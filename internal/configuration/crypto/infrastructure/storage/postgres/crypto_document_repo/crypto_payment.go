package crypto_document_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/configuration/crypto/domain/documents/crypto_payment"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
)

// CryptoPaymentRepo implements crypto_payment.Repository.
type CryptoPaymentRepo struct {
	*document_repo.BaseDocumentRepo[*crypto_payment.CryptoPayment]
}

// NewCryptoPaymentRepo creates a new crypto payment repository.
func NewCryptoPaymentRepo() *CryptoPaymentRepo {
	repo := &CryptoPaymentRepo{
		BaseDocumentRepo: document_repo.NewDocumentRepoForDefinition[*crypto_payment.CryptoPayment](crypto_payment.Definition,
			postgres.ExtractDBColumns[crypto_payment.CryptoPayment](),
			func() *crypto_payment.CryptoPayment { return &crypto_payment.CryptoPayment{} },
		),
	}

	// Register RLS dimensions
	repo.RegisterRLSDimension("merchant", "merchant_id")

	return repo
}

// FindByTxHash finds a crypto payment by blockchain transaction hash.
func (r *CryptoPaymentRepo) FindByTxHash(ctx context.Context, txHash string) (*crypto_payment.CryptoPayment, error) {
	q := r.Builder().Select("*").
		From(crypto_payment.Definition.Table).
		Where(squirrel.Eq{"tx_hash": txHash}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var p crypto_payment.CryptoPayment
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &p, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("find by tx hash: %w", err)
	}

	return &p, nil
}

func (r *CryptoPaymentRepo) FindByChainEvent(ctx context.Context, networkID id.ID, txHash, eventKind string, eventIndex int64) (*crypto_payment.CryptoPayment, error) {
	q := r.Builder().Select("*").From(crypto_payment.Definition.Table).
		Where(squirrel.Eq{"network_id": networkID, "tx_hash": txHash, "event_kind": eventKind, "event_index": eventIndex, "deletion_mark": false}).
		Limit(1)
	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build chain event query: %w", err)
	}
	var payment crypto_payment.CryptoPayment
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &payment, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("find payment by chain event: %w", err)
	}
	return &payment, nil
}

func (r *CryptoPaymentRepo) FindBySenderNonce(ctx context.Context, networkID id.ID, sourceAddress string, sourceNonce int64) (*crypto_payment.CryptoPayment, error) {
	q := r.Builder().Select("*").From(crypto_payment.Definition.Table).
		Where(squirrel.Eq{"network_id": networkID, "source_address": sourceAddress, "source_nonce": sourceNonce, "deletion_mark": false}).
		Where(squirrel.NotEq{"status": []crypto_payment.PaymentStatus{crypto_payment.PaymentStatusFailed, crypto_payment.PaymentStatusReplaced}}).
		OrderBy("created_at DESC").Limit(1)
	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sender nonce query: %w", err)
	}
	var payment crypto_payment.CryptoPayment
	if err := pgxscan.Get(ctx, r.GetTxManager(ctx).GetQuerier(ctx), &payment, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("find payment by sender nonce: %w", err)
	}
	return &payment, nil
}

// SummarizeByInvoice derives canonical invoice amounts from payment facts. It
// intentionally does not trust the denormalized invoice projection.
func (r *CryptoPaymentRepo) SummarizeByInvoice(ctx context.Context, invoiceID id.ID) (crypto_payment.InvoicePaymentSummary, error) {
	const query = `
		SELECT
			COALESCE(SUM(amount) FILTER (
				WHERE canonicality='canonical'
				  AND status IN ('detected','confirming','confirmed','settled')
			), 0) AS received_amount,
			COALESCE(SUM(amount) FILTER (
				WHERE canonicality='canonical'
				  AND status IN ('confirmed','settled')
			), 0) AS confirmed_amount,
			COALESCE(SUM(amount) FILTER (
				WHERE canonicality='canonical'
				  AND posted=TRUE
				  AND status IN ('confirmed','settled')
			), 0) AS credited_amount,
			MAX(updated_at) AS chain_observed_at
		FROM doc_crypto_payments
		WHERE invoice_id=$1 AND deletion_mark=FALSE`

	var projection struct {
		ReceivedAmount  types.RawAmount `db:"received_amount"`
		ConfirmedAmount types.RawAmount `db:"confirmed_amount"`
		CreditedAmount  types.RawAmount `db:"credited_amount"`
		ChainObservedAt *time.Time      `db:"chain_observed_at"`
	}
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &projection, query, invoiceID); err != nil {
		return crypto_payment.InvoicePaymentSummary{}, fmt.Errorf("summarize invoice payments: %w", err)
	}
	return crypto_payment.InvoicePaymentSummary{
		ReceivedAmount:  projection.ReceivedAmount,
		ConfirmedAmount: projection.ConfirmedAmount,
		CreditedAmount:  projection.CreditedAmount,
		ChainObservedAt: projection.ChainObservedAt,
	}, nil
}

// ClaimForConfirmation atomically leases a bounded batch of payments for confirmation polling.
func (r *CryptoPaymentRepo) ClaimForConfirmation(
	ctx context.Context,
	statuses []crypto_payment.PaymentStatus,
	workerID string,
	limit int,
	leaseTTL time.Duration,
) ([]*crypto_payment.CryptoPayment, error) {
	if len(statuses) == 0 {
		return nil, fmt.Errorf("claim confirmation payments: statuses are required")
	}
	if workerID == "" {
		return nil, fmt.Errorf("claim confirmation payments: worker ID is required")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("claim confirmation payments: positive limit is required")
	}
	if leaseTTL <= 0 {
		return nil, fmt.Errorf("claim confirmation payments: positive lease TTL is required")
	}

	statusValues := make([]string, 0, len(statuses))
	for _, status := range statuses {
		statusValues = append(statusValues, string(status))
	}
	leaseSeconds := int64(leaseTTL / time.Second)
	if leaseTTL%time.Second != 0 {
		leaseSeconds++
	}
	if leaseSeconds <= 0 {
		leaseSeconds = 1
	}

	sql := `
WITH candidates AS (
	SELECT id
	FROM doc_crypto_payments
	WHERE status = ANY($1::TEXT[])
	  AND deletion_mark = FALSE
	  AND next_confirmation_check_at <= NOW()
	  AND (
		confirmation_lease_expires_at IS NULL
		OR confirmation_lease_expires_at <= NOW()
	  )
	ORDER BY next_confirmation_check_at ASC, created_at ASC, id ASC
	LIMIT $3
	FOR UPDATE SKIP LOCKED
)
UPDATE doc_crypto_payments AS p
SET confirmation_claimed_by = $2,
	confirmation_claimed_at = NOW(),
	confirmation_lease_expires_at = NOW() + ($4::BIGINT * INTERVAL '1 second'),
	updated_at = NOW(),
	version = version + 1
FROM candidates
WHERE p.id = candidates.id
RETURNING p.*`

	var payments []*crypto_payment.CryptoPayment
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &payments, sql, statusValues, workerID, limit, leaseSeconds); err != nil {
		return nil, fmt.Errorf("claim confirmation payments: %w", err)
	}

	return payments, nil
}

// ReleaseConfirmationClaim schedules the next poll and clears this worker's lease.
func (r *CryptoPaymentRepo) ReleaseConfirmationClaim(ctx context.Context, paymentID id.ID, workerID string) error {
	if workerID == "" {
		return fmt.Errorf("release confirmation claim: worker ID is required")
	}

	sql := `
UPDATE doc_crypto_payments
SET confirmation_claimed_by = '',
	confirmation_claimed_at = NULL,
	confirmation_lease_expires_at = NULL,
	last_confirmation_checked_at = NOW(),
	confirmation_check_attempts = confirmation_check_attempts + 1,
	next_confirmation_check_at = NOW() +
		(CASE
			WHEN confirmation_check_attempts = 0 THEN 5
			WHEN confirmation_check_attempts = 1 THEN 15
			WHEN confirmation_check_attempts = 2 THEN 30
			WHEN confirmation_check_attempts = 3 THEN 60
			WHEN confirmation_check_attempts = 4 THEN 120
			ELSE 300
		 END + FLOOR(random() * 3)::INT) * INTERVAL '1 second',
	updated_at = NOW(),
	version = version + 1
WHERE id = $1
  AND confirmation_claimed_by = $2`

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if _, err := querier.Exec(ctx, sql, paymentID, workerID); err != nil {
		return fmt.Errorf("release confirmation claim: %w", err)
	}
	return nil
}

// GetByIDForUpdate retrieves a payment with an exclusive row lock (SELECT FOR UPDATE).
// Serializes concurrent confirmation updates: if two goroutines call this for the
// same payment, the second blocks until the first's transaction commits/rolls back.
// Safe from deadlocks: single-row lock on PK, deterministic order.
func (r *CryptoPaymentRepo) GetByIDForUpdate(ctx context.Context, docID id.ID) (*crypto_payment.CryptoPayment, error) {
	q := r.Builder().Select(r.SelectColumns()...).
		From(crypto_payment.Definition.Table).
		Where(squirrel.Eq{"id": docID}).
		Suffix("FOR UPDATE")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var p crypto_payment.CryptoPayment
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &p, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound(crypto_payment.Definition.Table, docID.String())
		}
		return nil, fmt.Errorf("get by id for update: %w", err)
	}

	return &p, nil
}
