/**
 * The auto parts store configuration's frontend («Автозапчасти»). Its
 * catalogs and documents use the platform's generic lists and forms;
 * registerAutopartsConfiguration adds their contracts and navigation.
 */
import { registerAutopartsContracts } from "./lib/api/contracts"
import { registerAutopartsNavigation } from "./lib/navigation"

export function registerAutopartsConfiguration(): void {
  registerAutopartsContracts()
  registerAutopartsNavigation()
}
