import { apiFetch, createCatalogApi } from "./core"
import type * as Types from "@/types/generated/http-contracts"

export const integrationAccounts = createCatalogApi<Types.IntegrationAccountResponse, Types.CreateIntegrationAccountRequest, Types.UpdateIntegrationAccountRequest>("/catalog/integration-accounts")
export const integrationPools = createCatalogApi<Types.IntegrationPoolResponse, Types.CreateIntegrationPoolRequest, Types.UpdateIntegrationPoolRequest>("/catalog/integration-pools")
export const integrationRuntime = {
  providers: () => apiFetch<Types.IntegrationProvidersResponse>("/system/integrations/providers"),
  status: (id: string) => apiFetch<Types.IntegrationPoolStatusResponse>(`/system/integrations/pools/${id}/status`),
  command: (id: string, data: Types.IntegrationPoolCommandRequest) => apiFetch<Types.IntegrationPoolStatusResponse>(`/system/integrations/pools/${id}/command`, { method: "POST", body: JSON.stringify(data) }),
  test: (id: string) => apiFetch<Types.IntegrationConnectionTestResponse>(`/system/integrations/pools/${id}/test`, { method: "POST" }),
}
