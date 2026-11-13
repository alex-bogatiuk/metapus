package content

import (
	"metapus/internal/domain"
	"metapus/internal/domain/documents/crypto_custody_receipt"
	"metapus/internal/domain/documents/crypto_merchant_credit"
	"metapus/internal/domain/documents/crypto_platform_funding"
	"metapus/internal/infrastructure/http/contracts/spec"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/metadata"
)

// Receipts and their manual attributions are created and decided only by the
// unidentified receipts workflow. They are listed and opened read-only, with
// their register movements.
var readOnlyCryptoDocument = metadata.DocumentCapabilities{List: true, Read: true, ExportList: true, Movements: true}

// ---------------------------------------------------------------------------
// CryptoCustodyReceipt
// ---------------------------------------------------------------------------

type CryptoCustodyReceiptRegistration struct {
	v1.EntityRegistration[crypto_custody_receipt.CryptoCustodyReceipt]
}

func (r *CryptoCustodyReceiptRegistration) EntityLabel() string {
	return "Поступление на кошелёк"
}
func (r *CryptoCustodyReceiptRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{Singular: "Поступление на кошелёк", Plural: "Поступления на кошельки", Genitive: "поступления на кошелёк"}
}
func (r *CryptoCustodyReceiptRegistration) RLSDimensions() map[string]string {
	return map[string]string{}
}
func (r *CryptoCustodyReceiptRegistration) DocumentCapabilities() metadata.DocumentCapabilities {
	return readOnlyCryptoDocument
}
func (r *CryptoCustodyReceiptRegistration) HTTPContracts() spec.Bundle {
	return spec.EntityWithDetail[struct{}, struct{}, crypto_custody_receipt.CryptoCustodyReceipt, crypto_custody_receipt.CryptoCustodyReceipt](
		r.EntityDefinition(), r.DocumentCapabilities())
}
func (r *CryptoCustodyReceiptRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	service := domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*crypto_custody_receipt.CryptoCustodyReceipt]{
		Definition: &crypto_custody_receipt.Definition, Repo: document_repo.NewCryptoCustodyReceiptRepo(),
		PostingEngine: deps.PostingEngine, Numerator: deps.Numerator, PolicyEngine: deps.PolicyEngine, NumeratorPrefix: "CCR",
	})
	return handlers.NewReadOnlyDocumentHandler(deps.BaseHandler, service, crypto_custody_receipt.Definition.Key,
		deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}

// ---------------------------------------------------------------------------
// CryptoMerchantCredit
// ---------------------------------------------------------------------------

type CryptoMerchantCreditRegistration struct {
	v1.EntityRegistration[crypto_merchant_credit.CryptoMerchantCredit]
}

func (r *CryptoMerchantCreditRegistration) EntityLabel() string {
	return "Ручное зачисление мерчанту"
}
func (r *CryptoMerchantCreditRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{Singular: "Ручное зачисление мерчанту", Plural: "Ручные зачисления мерчантам", Genitive: "ручного зачисления"}
}
func (r *CryptoMerchantCreditRegistration) RLSDimensions() map[string]string {
	return map[string]string{"merchant": "merchant_id"}
}
func (r *CryptoMerchantCreditRegistration) DocumentCapabilities() metadata.DocumentCapabilities {
	return readOnlyCryptoDocument
}
func (r *CryptoMerchantCreditRegistration) HTTPContracts() spec.Bundle {
	return spec.EntityWithDetail[struct{}, struct{}, crypto_merchant_credit.CryptoMerchantCredit, crypto_merchant_credit.CryptoMerchantCredit](
		r.EntityDefinition(), r.DocumentCapabilities())
}
func (r *CryptoMerchantCreditRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	service := domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*crypto_merchant_credit.CryptoMerchantCredit]{
		Definition: &crypto_merchant_credit.Definition, Repo: document_repo.NewCryptoMerchantCreditRepo(),
		PostingEngine: deps.PostingEngine, Numerator: deps.Numerator, PolicyEngine: deps.PolicyEngine, NumeratorPrefix: "CMC",
	})
	return handlers.NewReadOnlyDocumentHandler(deps.BaseHandler, service, crypto_merchant_credit.Definition.Key,
		deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}

// ---------------------------------------------------------------------------
// CryptoPlatformFunding
// ---------------------------------------------------------------------------

type CryptoPlatformFundingRegistration struct {
	v1.EntityRegistration[crypto_platform_funding.CryptoPlatformFunding]
}

func (r *CryptoPlatformFundingRegistration) EntityLabel() string {
	return "Пополнение платформы"
}
func (r *CryptoPlatformFundingRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{Singular: "Пополнение платформы", Plural: "Пополнения платформы", Genitive: "пополнения платформы"}
}
func (r *CryptoPlatformFundingRegistration) RLSDimensions() map[string]string {
	return map[string]string{}
}
func (r *CryptoPlatformFundingRegistration) DocumentCapabilities() metadata.DocumentCapabilities {
	return readOnlyCryptoDocument
}
func (r *CryptoPlatformFundingRegistration) HTTPContracts() spec.Bundle {
	return spec.EntityWithDetail[struct{}, struct{}, crypto_platform_funding.CryptoPlatformFunding, crypto_platform_funding.CryptoPlatformFunding](
		r.EntityDefinition(), r.DocumentCapabilities())
}
func (r *CryptoPlatformFundingRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	service := domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*crypto_platform_funding.CryptoPlatformFunding]{
		Definition: &crypto_platform_funding.Definition, Repo: document_repo.NewCryptoPlatformFundingRepo(),
		PostingEngine: deps.PostingEngine, Numerator: deps.Numerator, PolicyEngine: deps.PolicyEngine, NumeratorPrefix: "CPF",
	})
	return handlers.NewReadOnlyDocumentHandler(deps.BaseHandler, service, crypto_platform_funding.Definition.Key,
		deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}
