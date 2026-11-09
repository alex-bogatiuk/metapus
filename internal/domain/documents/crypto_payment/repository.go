package crypto_payment

import (
	"context"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain"
)

// InvoicePaymentSummary is the canonical projection used to advance invoice
// lifecycle. All values are exact token minor units.
type InvoicePaymentSummary struct {
	ReceivedAmount  types.CryptoAmount
	ConfirmedAmount types.CryptoAmount
	CreditedAmount  types.CryptoAmount
	ChainObservedAt *time.Time
}

// Repository defines operations for crypto payment documents (header-only, no lines).
type Repository interface {
	// CRUD operations
	Create(ctx context.Context, doc *CryptoPayment) error
	GetByID(ctx context.Context, docID id.ID) (*CryptoPayment, error)
	GetByNumber(ctx context.Context, number string) (*CryptoPayment, error)
	Update(ctx context.Context, doc *CryptoPayment) error
	Delete(ctx context.Context, docID id.ID) error

	// List operations
	List(ctx context.Context, filter domain.ListFilter) (domain.CursorListResult[*CryptoPayment], error)
	ListIDs(ctx context.Context, filter domain.ListFilter, maxIDs int) ([]id.ID, error)

	// Crypto-specific
	FindByTxHash(ctx context.Context, txHash string) (*CryptoPayment, error)
	FindByChainEvent(ctx context.Context, networkID id.ID, txHash, eventKind string, eventIndex int64) (*CryptoPayment, error)
	FindBySenderNonce(ctx context.Context, networkID id.ID, sourceAddress string, sourceNonce int64) (*CryptoPayment, error)
	SummarizeByInvoice(ctx context.Context, invoiceID id.ID) (InvoicePaymentSummary, error)
	ClaimForConfirmation(ctx context.Context, statuses []PaymentStatus, workerID string, limit int, leaseTTL time.Duration) ([]*CryptoPayment, error)
	ReleaseConfirmationClaim(ctx context.Context, paymentID id.ID, workerID string) error
	ClaimForCanonicalityAudit(ctx context.Context, workerID string, limit int, leaseTTL time.Duration) ([]*CryptoPayment, error)
	ReleaseCanonicalityAuditClaim(ctx context.Context, paymentID id.ID, workerID string, checkErr error) error

	// GetByIDForUpdate retrieves a payment with an exclusive row lock (SELECT FOR UPDATE).
	// Used to serialize concurrent confirmation updates from consumer + confirmation loop.
	// Must be called inside a transaction — the lock is released on COMMIT/ROLLBACK.
	GetByIDForUpdate(ctx context.Context, docID id.ID) (*CryptoPayment, error)
}
