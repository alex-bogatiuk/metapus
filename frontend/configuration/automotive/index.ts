/**
 * The frontend of the add-on «Автобизнес» on «Торговля и сервис»: the base
 * configuration registers first (manifest "base"), this after it. The add-on's
 * fields of the base's catalogs (an item's brand and normalized article) are
 * shown by the base's generic forms and lists from the metadata; its route
 * table merges their request and response contracts into the base's routes.
 */
import { httpRoutes } from "@/configuration/automotive/lib/generated/http-contract-routes"
import { registerHTTPRoutes } from "@/lib/http-contracts"

export function registerAutomotiveConfiguration(): void {
  registerHTTPRoutes(httpRoutes)
}
