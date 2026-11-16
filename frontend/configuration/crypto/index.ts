/**
 * The crypto-processing configuration's frontend. registerCryptoConfiguration
 * adds its parts to the platform registries; app/ calls it once at start-up.
 */
import { registerCryptoEntities } from "./lib/entity-overlays"
import { registerCryptoWidgets } from "./lib/widgets"

export function registerCryptoConfiguration(): void {
    registerCryptoEntities()
    registerCryptoWidgets()
}
