/**
 * The configuration's HTTP route contracts: its catalogs' and documents'
 * generated routes.
 */
import { httpRoutes } from "@/configuration/trade/lib/generated/http-contract-routes"
import { registerHTTPRoutes } from "@/lib/http-contracts"

export function registerTradeContracts(): void {
  registerHTTPRoutes(httpRoutes)
}
