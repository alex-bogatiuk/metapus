// Registers the crypto configuration's parts with the platform registries, as
// app/configuration.ts does in the crypto build. Tests of the assembled
// crypto application import it first, whichever configuration app/ holds.
import { registerCryptoConfiguration } from "../configuration/crypto"

registerCryptoConfiguration()
