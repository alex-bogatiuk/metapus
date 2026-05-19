package document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/infrastructure/storage/postgres"
)

const _cryptoPaymentsTable = "doc_crypto_payments"

// CryptoPaymentRepo implements crypto_payment.Repository.
type CryptoPaymentRepo struct {
	*BaseDocumentRepo[*crypto_payment.CryptoPayment]
}

// NewCryptoPaymentRepo creates a new crypto payment repository.
func NewCryptoPaymentRepo() *CryptoPaymentRepo {
	repo := &CryptoPaymentRepo{
		BaseDocumentRepo: NewBaseDocumentRepo[*crypto_payment.CryptoPayment](
			_cryptoPaymentsTable,
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
		From(_cryptoPaymentsTable).
		Where(squirrel.Eq{"tx_hash": txHash}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var p crypto_payment.CryptoPayment
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &p, sql, args...); err != nil {
		return nil, fmt.Errorf("find by tx hash: %w", err)
	}

	return &p, nil
}

// ListByStatus returns all crypto payments in a given status (e.g. "confirming").
func (r *CryptoPaymentRepo) ListByStatus(ctx context.Context, status crypto_payment.PaymentStatus) ([]*crypto_payment.CryptoPayment, error) {
	q := r.Builder().Select("*").
		From(_cryptoPaymentsTable).
		Where(squirrel.Eq{"status": string(status)}).
		Where(squirrel.Eq{"deletion_mark": false}).
		OrderBy("created_at ASC")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var payments []*crypto_payment.CryptoPayment
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &payments, sql, args...); err != nil {
		return nil, fmt.Errorf("list by status: %w", err)
	}

	return payments, nil
}

// GetByIDForUpdate retrieves a payment with an exclusive row lock (SELECT FOR UPDATE).
// Serializes concurrent confirmation updates: if two goroutines call this for the
// same payment, the second blocks until the first's transaction commits/rolls back.
// Safe from deadlocks: single-row lock on PK, deterministic order.
func (r *CryptoPaymentRepo) GetByIDForUpdate(ctx context.Context, docID id.ID) (*crypto_payment.CryptoPayment, error) {
	q := r.Builder().Select(r.selectCols...).
		From(_cryptoPaymentsTable).
		Where(squirrel.Eq{"id": docID}).
		Suffix("FOR UPDATE")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var p crypto_payment.CryptoPayment
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &p, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound(_cryptoPaymentsTable, docID.String())
		}
		return nil, fmt.Errorf("get by id for update: %w", err)
	}

	return &p, nil
}
