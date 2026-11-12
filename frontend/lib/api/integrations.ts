import { apiFetch, createCatalogApi } from "./core"
import * as wire from "@/lib/generated/http-contract-schemas"
import type * as Types from "@/types/generated/http-contracts"

export const integrationAccounts = createCatalogApi<Types.IntegrationAccountResponse, Types.CreateIntegrationAccountRequest, Types.UpdateIntegrationAccountRequest>(
  "/catalog/integration-accounts", wire.IntegrationAccountResponseSchema, wire.CreateIntegrationAccountRequestSchema, wire.UpdateIntegrationAccountRequestSchema,
)
export const integrationPools = createCatalogApi<Types.IntegrationPoolResponse, Types.CreateIntegrationPoolRequest, Types.UpdateIntegrationPoolRequest>(
  "/catalog/integration-pools", wire.IntegrationPoolResponseSchema, wire.CreateIntegrationPoolRequestSchema, wire.UpdateIntegrationPoolRequestSchema,
)
export const integrationRuntime = {
  providers: () => apiFetch<Types.IntegrationProvidersResponse>("/system/integrations/providers"),
  status: (id: string) => apiFetch<Types.IntegrationPoolStatusResponse>(`/system/integrations/pools/${id}/status`),
  command: (id: string, data: Types.IntegrationPoolCommandRequest) => apiFetch<Types.IntegrationPoolStatusResponse>(`/system/integrations/pools/${id}/command`, { method: "POST", body: JSON.stringify(data) }),
  test: (id: string) => apiFetch<Types.IntegrationConnectionTestResponse>(`/system/integrations/pools/${id}/test`, { method: "POST" }),
  rpcKeyNames: (poolId: string, draft: { networkId?: string; role?: string; url?: string }) => {
    const query = new URLSearchParams(Object.entries(draft).filter((entry): entry is [string, string] => !!entry[1])).toString()
    return apiFetch<Types.IntegrationRPCKeyOptionsResponse>(`/system/integrations/pools/${poolId}/rpc-key-names${query ? `?${query}` : ""}`)
  },
}
