import "./contracts" // registers the configuration's route contracts before any request
import type * as contracts from "@/configuration/crypto/types/generated/http-contracts"
import { createCatalogApi } from "@/lib/api/core"

// Catalogs of the crypto-processing configuration.
export const cryptoCatalogsApi = {
    merchants: createCatalogApi<contracts.MerchantResponse, contracts.CreateMerchantRequest, contracts.UpdateMerchantRequest>("/catalog/merchants"),

    blockchainNetworks: createCatalogApi<contracts.BlockchainNetworkResponse, contracts.CreateBlockchainNetworkRequest, contracts.UpdateBlockchainNetworkRequest>("/catalog/blockchain-networks"),

    tokens: createCatalogApi<
        contracts.CryptoTokenResponse,
        contracts.CreateTokenRequest,
        contracts.UpdateTokenRequest
    >("/catalog/tokens"),

    wallets: createCatalogApi<
        contracts.WalletResponse,
        contracts.CreateWalletRequest,
        contracts.UpdateWalletRequest
    >("/catalog/wallets"),

    cryptoProviderAccounts: createCatalogApi<
        import("@/configuration/crypto/types/crypto-network").CryptoProviderAccount,
        import("@/configuration/crypto/types/generated/http-contracts").CreateCryptoProviderAccountRequest,
        import("@/configuration/crypto/types/generated/http-contracts").UpdateCryptoProviderAccountRequest
    >("/catalog/crypto-provider-accounts"),
}
