import { z } from "zod"

// The Content-Security-Policy (proxy.ts) forbids eval, which Zod's JIT object
// parsers rely on. Parse without it, before any schema is built, so Zod does
// not probe for eval and trip a CSP violation on every page load.
z.config({ jitless: true })
