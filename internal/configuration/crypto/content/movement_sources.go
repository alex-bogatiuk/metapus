package content

import (
	"metapus/internal/configuration/crypto/domain/documents/crypto_custody_receipt"
	"metapus/internal/configuration/crypto/domain/documents/crypto_merchant_credit"
	"metapus/internal/configuration/crypto/domain/documents/crypto_network_operation"
	"metapus/internal/configuration/crypto/domain/documents/crypto_payment"
	"metapus/internal/configuration/crypto/domain/documents/crypto_platform_funding"
	"metapus/internal/configuration/crypto/domain/documents/crypto_provider_balance_operation"
	"metapus/internal/configuration/crypto/domain/documents/crypto_resource_rental"
	"metapus/internal/configuration/crypto/domain/documents/crypto_sweep"
	"metapus/internal/configuration/crypto/domain/documents/crypto_withdrawal"
	"metapus/internal/configuration/crypto/domain/documents/treasury_withdrawal"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/pkg/sdk/registry"
)

// movementSources maps each posting document to the source fields behind its
// register movement columns (see platform.MovementSourceProvider). A column
// missing here is hidden from readers with a field policy on the document.
var movementSources = map[string]map[string][]string{
	crypto_payment.Definition.Key: {
		"merchant": {"merchant_id"}, "wallet": {"wallet_id"}, "token": {"token_id"},
		"amount": {"amount", "fee_fixed", "fee_percent_bp", "fee_min", "fee_max"}, "feeType": {"fee_fixed", "fee_percent_bp"},
	},
	crypto_withdrawal.Definition.Key: {
		"merchant": {"merchant_id"}, "wallet": {"source_wallet_id"}, "token": {"token_id", "network_fee_token_id"},
		"amount": {"amount", "network_fee"}, "feeType": {"network_fee"},
	},
	crypto_sweep.Definition.Key: {
		"wallet": {"hot_wallet_id", "lines.wallet_id"}, "token": {"token_id"},
		"amount": {"total_amount", "total_fee", "lines.amount", "lines.network_fee"},
	},
	crypto_provider_balance_operation.Definition.Key: {
		"providerAccount": {"provider_account_id"}, "token": {"token_id"}, "wallet": {"source_wallet_id"},
		"operationType": {"operation_type"}, "externalRef": {"external_ref"}, "amount": {"amount"},
	},
	crypto_resource_rental.Definition.Key: {
		"providerAccount": {"provider_account_id"}, "network": {"network_id"}, "wallet": {"receive_wallet_id"},
		"token": {"cost_token_id"}, "costToken": {"cost_token_id"}, "provider": {"provider"},
		// The purpose is the constant resource_rental; it discloses no field.
		"costType": {"resource_type"}, "purpose": {}, "externalOrderId": {"external_serial"},
		"amount": {"gross_cost_amount", "refund_amount", "net_cost_amount", "allocated_cost_amount", "unused_cost_amount"},
	},
	crypto_network_operation.Definition.Key: {
		"network": {"network_id"}, "wallet": {"source_wallet_id", "target_wallet_id"},
		"token": {"cost_token_id"}, "costToken": {"cost_token_id"}, "costType": {"operation_type"},
		// Purpose (activation) and provider ("chain") are constants.
		"purpose": {}, "provider": {}, "amount": {"cost_amount"},
	},
	treasury_withdrawal.Definition.Key: {
		"merchant": {"feeLines.merchantId"}, "feeType": {"feeLines.feeType"},
		"wallet": {"source_wallet_id"}, "token": {"token_id"}, "amount": {"amount", "network_fee", "feeLines.amount"},
	},
	withdrawal_request.Definition.Key: {
		"merchant": {"merchant_id"}, "token": {"token_id"}, "amount": {"amount"},
	},
	// A receipt adds custody and the unattributed remainder of its own transfer.
	crypto_custody_receipt.Definition.Key: {
		"wallet": {"wallet_id"}, "receipt": {"id"}, "token": {"token_id"}, "amount": {"amount"},
	},
	crypto_merchant_credit.Definition.Key: {
		"receipt": {"receipt_id"}, "merchant": {"merchant_id"}, "token": {"token_id"},
		"amount": {"amount", "net_amount", "processing_fee"}, "feeType": {"processing_fee"},
	},
	crypto_platform_funding.Definition.Key: {
		"receipt": {"receipt_id"}, "token": {"token_id"}, "amount": {"amount"},
	},
}

var (
	_ registry.MovementSourceProvider = (*CryptoPaymentRegistration)(nil)
	_ registry.MovementSourceProvider = (*CryptoWithdrawalRegistration)(nil)
	_ registry.MovementSourceProvider = (*CryptoSweepRegistration)(nil)
	_ registry.MovementSourceProvider = (*CryptoProviderBalanceOperationRegistration)(nil)
	_ registry.MovementSourceProvider = (*CryptoResourceRentalRegistration)(nil)
	_ registry.MovementSourceProvider = (*CryptoNetworkOperationRegistration)(nil)
	_ registry.MovementSourceProvider = (*TreasuryWithdrawalRegistration)(nil)
	_ registry.MovementSourceProvider = (*WithdrawalRequestRegistration)(nil)
	_ registry.MovementSourceProvider = (*CryptoCustodyReceiptRegistration)(nil)
	_ registry.MovementSourceProvider = (*CryptoMerchantCreditRegistration)(nil)
	_ registry.MovementSourceProvider = (*CryptoPlatformFundingRegistration)(nil)
)

func (*CryptoPaymentRegistration) MovementSources() map[string][]string {
	return movementSources[crypto_payment.Definition.Key]
}
func (*CryptoWithdrawalRegistration) MovementSources() map[string][]string {
	return movementSources[crypto_withdrawal.Definition.Key]
}
func (*CryptoSweepRegistration) MovementSources() map[string][]string {
	return movementSources[crypto_sweep.Definition.Key]
}
func (*CryptoProviderBalanceOperationRegistration) MovementSources() map[string][]string {
	return movementSources[crypto_provider_balance_operation.Definition.Key]
}
func (*CryptoResourceRentalRegistration) MovementSources() map[string][]string {
	return movementSources[crypto_resource_rental.Definition.Key]
}
func (*CryptoNetworkOperationRegistration) MovementSources() map[string][]string {
	return movementSources[crypto_network_operation.Definition.Key]
}
func (*TreasuryWithdrawalRegistration) MovementSources() map[string][]string {
	return movementSources[treasury_withdrawal.Definition.Key]
}
func (*WithdrawalRequestRegistration) MovementSources() map[string][]string {
	return movementSources[withdrawal_request.Definition.Key]
}
func (*CryptoCustodyReceiptRegistration) MovementSources() map[string][]string {
	return movementSources[crypto_custody_receipt.Definition.Key]
}
func (*CryptoMerchantCreditRegistration) MovementSources() map[string][]string {
	return movementSources[crypto_merchant_credit.Definition.Key]
}
func (*CryptoPlatformFundingRegistration) MovementSources() map[string][]string {
	return movementSources[crypto_platform_funding.Definition.Key]
}
