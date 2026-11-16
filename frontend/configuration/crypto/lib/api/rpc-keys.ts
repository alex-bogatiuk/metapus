import "./contracts" // registers the configuration's route contracts before any request
import { apiFetch } from "@/lib/api/core"
import type { IntegrationRPCKeyOptionsResponse } from "@/configuration/crypto/types/generated/http-contracts"

/** Account key names an RPC connection of a pool can use, served by the configuration. */
export const rpcKeysApi = {
    names: (poolId: string, draft: { networkId?: string; role?: string; url?: string }) => {
        const query = new URLSearchParams(Object.entries(draft).filter((entry): entry is [string, string] => !!entry[1])).toString()
        return apiFetch<IntegrationRPCKeyOptionsResponse>(`/system/integrations/pools/${poolId}/rpc-key-names${query ? `?${query}` : ""}`)
    },
}
