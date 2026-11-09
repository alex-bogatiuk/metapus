import type * as contracts from "@/types/generated/http-contracts"
import * as wireSchemas from "@/lib/generated/http-contract-schemas"
import type {
    CounterpartyResponse,
    CreateCounterpartyRequest,
    UpdateCounterpartyRequest,
    OrganizationResponse,
    CreateOrganizationRequest,
    UpdateOrganizationRequest,
    CurrencyResponse,
    CreateCurrencyRequest,
    UpdateCurrencyRequest,
    ContractResponse,
    CreateContractRequest,
    UpdateContractRequest,
} from "@/types/catalog"
import { createCatalogApi } from "./core"

export const catalogsApi = {
    counterparties: createCatalogApi<CounterpartyResponse, CreateCounterpartyRequest, UpdateCounterpartyRequest>(
        "/catalog/counterparties",
        wireSchemas.CounterpartyResponseSchema,
        wireSchemas.CreateCounterpartyRequestSchema,
        wireSchemas.UpdateCounterpartyRequestSchema
    ),

    organizations: createCatalogApi<OrganizationResponse, CreateOrganizationRequest, UpdateOrganizationRequest>(
        "/catalog/organizations",
        wireSchemas.OrganizationResponseSchema,
        wireSchemas.CreateOrganizationRequestSchema,
        wireSchemas.UpdateOrganizationRequestSchema
    ),

    currencies: createCatalogApi<CurrencyResponse, CreateCurrencyRequest, UpdateCurrencyRequest>(
        "/catalog/currencies",
        wireSchemas.CurrencyResponseSchema,
        wireSchemas.CreateCurrencyRequestSchema,
        wireSchemas.UpdateCurrencyRequestSchema
    ),

    contracts: createCatalogApi<ContractResponse, CreateContractRequest, UpdateContractRequest>(
        "/catalog/contracts",
        wireSchemas.ContractResponseSchema,
        wireSchemas.CreateContractRequestSchema,
        wireSchemas.UpdateContractRequestSchema
    ),

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
        import("@/types/crypto-network").CryptoProviderAccount,
        import("@/types/generated/http-contracts").CreateCryptoProviderAccountRequest,
        import("@/types/generated/http-contracts").UpdateCryptoProviderAccountRequest
    >(
        "/catalog/crypto-provider-accounts",
        wireSchemas.CryptoProviderAccountResponseSchema,
        wireSchemas.CreateCryptoProviderAccountRequestSchema,
        wireSchemas.UpdateCryptoProviderAccountRequestSchema
    ),
}
