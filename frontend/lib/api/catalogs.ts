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
    counterparties: createCatalogApi<CounterpartyResponse, CreateCounterpartyRequest, UpdateCounterpartyRequest>("/catalog/counterparties"),

    organizations: createCatalogApi<OrganizationResponse, CreateOrganizationRequest, UpdateOrganizationRequest>("/catalog/organizations"),

    currencies: createCatalogApi<CurrencyResponse, CreateCurrencyRequest, UpdateCurrencyRequest>("/catalog/currencies"),

    contracts: createCatalogApi<ContractResponse, CreateContractRequest, UpdateContractRequest>("/catalog/contracts"),
}
