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
}
