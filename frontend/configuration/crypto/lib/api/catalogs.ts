import "./contracts" // registers the configuration's route contracts before any request
import type * as contracts from "@/configuration/crypto/types/generated/http-contracts"
import * as wireSchemas from "@/configuration/crypto/lib/generated/http-contract-schemas"
import { createCatalogApi } from "@/lib/api/core"

// Catalogs of the crypto-processing configuration.
export const cryptoCatalogsApi = {
    merchants: createCatalogApi<contracts.MerchantResponse, contracts.CreateMerchantRequest, contracts.UpdateMerchantRequest>(
        "/catalog/merchants",
        wireSchemas.MerchantResponseSchema,
        wireSchemas.CreateMerchantRequestSchema,
        wireSchemas.UpdateMerchantRequestSchema
    ),

    blockchainNetworks: createCatalogApi<contracts.BlockchainNetworkResponse, contracts.CreateBlockchainNetworkRequest, contracts.UpdateBlockchainNetworkRequest>(
        "/catalog/blockchain-networks",
        wireSchemas.BlockchainNetworkResponseSchema,
        wireSchemas.CreateBlockchainNetworkRequestSchema,
        wireSchemas.UpdateBlockchainNetworkRequestSchema
    ),

    tokens: createCatalogApi<
        contracts.CryptoTokenResponse,
        contracts.CreateTokenRequest,
        contracts.UpdateTokenRequest
    >(
        "/catalog/tokens",
        wireSchemas.CryptoTokenResponseSchema,
        wireSchemas.CreateTokenRequestSchema,
        wireSchemas.UpdateTokenRequestSchema
    ),

    wallets: createCatalogApi<
        contracts.WalletResponse,
        contracts.CreateWalletRequest,
        contracts.UpdateWalletRequest
    >(
        "/catalog/wallets",
        wireSchemas.WalletResponseSchema,
        wireSchemas.CreateWalletRequestSchema,
        wireSchemas.UpdateWalletRequestSchema
    ),

    cryptoProviderAccounts: createCatalogApi<
        import("@/configuration/crypto/types/crypto-network").CryptoProviderAccount,
        import("@/configuration/crypto/types/generated/http-contracts").CreateCryptoProviderAccountRequest,
        import("@/configuration/crypto/types/generated/http-contracts").UpdateCryptoProviderAccountRequest
    >(
        "/catalog/crypto-provider-accounts",
        wireSchemas.CryptoProviderAccountResponseSchema,
        wireSchemas.CreateCryptoProviderAccountRequestSchema,
        wireSchemas.UpdateCryptoProviderAccountRequestSchema
    ),
}
