/**
 * The frontend of the base configuration «Торговля и сервис» (trade). Its
 * catalogs and documents use the platform's generic lists and forms;
 * registerTradeConfiguration adds their contracts and navigation.
 */
import { registerTradeContracts } from "./lib/api/contracts"
import { registerTradeNavigation } from "./lib/navigation"

export function registerTradeConfiguration(): void {
  registerTradeContracts()
  registerTradeNavigation()
}
