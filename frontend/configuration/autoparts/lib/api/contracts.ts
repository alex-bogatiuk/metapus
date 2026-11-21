/**
 * The configuration's HTTP route contracts: its catalogs' and the goods
 * receipt's generated routes.
 */
import { httpRoutes } from "@/configuration/autoparts/lib/generated/http-contract-routes"
import { registerHTTPRoutes } from "@/lib/http-contracts"

export function registerAutopartsContracts(): void {
  registerHTTPRoutes(httpRoutes)
}
