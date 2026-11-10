package document_repo

import (
	"metapus/internal/domain/documents/crypto_merchant_credit"
	"metapus/internal/infrastructure/storage/postgres"
)

type CryptoMerchantCreditRepo struct {
	*BaseDocumentRepo[*crypto_merchant_credit.CryptoMerchantCredit]
}

func NewCryptoMerchantCreditRepo() *CryptoMerchantCreditRepo {
	return &CryptoMerchantCreditRepo{BaseDocumentRepo: NewDocumentRepoForDefinition(
		crypto_merchant_credit.Definition,
		postgres.ExtractDBColumns[crypto_merchant_credit.CryptoMerchantCredit](),
		func() *crypto_merchant_credit.CryptoMerchantCredit {
			return &crypto_merchant_credit.CryptoMerchantCredit{}
		},
	)}
}
