/**
 * The crypto-processing configuration's frontend. registerCryptoConfiguration
 * adds its parts to the platform registries; app/ calls it once at start-up.
 */
import { registerPortalWorkspace } from "./lib/access"
import { registerCryptoContracts } from "./lib/api/contracts"
import { registerCryptoAdmin } from "./lib/admin"
import { registerCryptoEntities } from "./lib/entity-overlays"
import { registerCryptoLabels } from "./lib/labels"
import { registerCryptoNavigation } from "./lib/navigation"
import { registerCryptoSecurity } from "./lib/security"
import { registerCryptoSettings } from "./lib/settings"
import { registerCryptoWidgets } from "./lib/widgets"

export function registerCryptoConfiguration(): void {
    registerCryptoContracts()
    registerCryptoEntities()
    registerCryptoNavigation()
    registerPortalWorkspace()
    registerCryptoSecurity()
    registerCryptoLabels()
    registerCryptoSettings()
    registerCryptoAdmin()
    registerCryptoWidgets()
}
