package crypto_document_repo

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_merchant_credit"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/document_repo"
)

type CryptoMerchantCreditRepo struct {
	*document_repo.BaseDocumentRepo[*crypto_merchant_credit.CryptoMerchantCredit]
}

func NewCryptoMerchantCreditRepo() *CryptoMerchantCreditRepo {
	return &CryptoMerchantCreditRepo{BaseDocumentRepo: document_repo.NewDocumentRepoForDefinition(
		crypto_merchant_credit.Definition,
		postgres.ExtractDBColumns[crypto_merchant_credit.CryptoMerchantCredit](),
		func() *crypto_merchant_credit.CryptoMerchantCredit {
			return &crypto_merchant_credit.CryptoMerchantCredit{}
		},
	)}
}
