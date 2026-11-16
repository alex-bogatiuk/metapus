/**
 * The configuration's HTTP route contracts. Importing this module registers
 * them, so every API module of the configuration imports it first.
 */
import { httpRoutes } from "@/configuration/crypto/lib/generated/http-contract-routes"
import { registerHTTPRoutes } from "@/lib/http-contracts"

registerHTTPRoutes(httpRoutes)

export function registerCryptoContracts(): void {
    registerHTTPRoutes(httpRoutes)
}
