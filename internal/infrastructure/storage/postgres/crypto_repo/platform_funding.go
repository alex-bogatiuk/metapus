package crypto_repo

import (
	"context"
	"fmt"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_platform_funding"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
)

type PlatformFundingRepo struct {
	*document_repo.CryptoPlatformFundingRepo
}

func NewPlatformFundingRepo() *PlatformFundingRepo {
	return &PlatformFundingRepo{CryptoPlatformFundingRepo: document_repo.NewCryptoPlatformFundingRepo()}
}

func (r *PlatformFundingRepo) LockReceiptRemainder(ctx context.Context, receiptID id.ID) (cryptoapp.ReceiptRemainder, error) {
	return lockReceiptRemainder(ctx, receiptID)
}

// ReceiptMerchantTie reads the tie with the same SQL function the platform
// funding guard checks, so the warning and the guard never disagree.
func (r *PlatformFundingRepo) ReceiptMerchantTie(ctx context.Context, receiptID id.ID) (*crypto_platform_funding.MerchantTie, error) {
	var merchantID *id.ID
	var link, name *string
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT t.merchant_id,t.link,m.name
		FROM fn_crypto_receipt_merchant_link($1) t LEFT JOIN cat_merchants m ON m.id=t.merchant_id`, receiptID).
		Scan(&merchantID, &link, &name)
	if err != nil {
		return nil, fmt.Errorf("read receipt merchant tie: %w", err)
	}
	if merchantID == nil || link == nil {
		return nil, nil
	}
	tie := &crypto_platform_funding.MerchantTie{MerchantID: *merchantID, Link: crypto_platform_funding.MerchantLink(*link)}
	if name != nil {
		tie.MerchantName = *name
	}
	return tie, nil
}

var _ cryptoapp.PlatformFundingRepository = (*PlatformFundingRepo)(nil)
