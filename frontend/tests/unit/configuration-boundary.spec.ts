import { existsSync, readdirSync, readFileSync, statSync } from "node:fs"
import { dirname, join, normalize, relative } from "node:path"
import { expect, test } from "@playwright/test"

// The platform UI (components, lib, hooks, stores, types) is reusable by any
// configuration; the crypto-processing configuration lives in configuration/.
// app/ is the composition root and may import both.
const root = join(__dirname, "..", "..")
const platformDirs = ["components", "lib", "hooks", "stores", "types"]

// knownConfigurationLeaks lists tolerated platform → configuration imports.
// It only shrinks: register configuration parts through a platform extension
// point instead of importing them here.
const knownConfigurationLeaks: Record<string, string[]> = {
  "components/dashboard/widgets/attention-counter-renderer.tsx": ["configuration/crypto/hooks/useAdminNetworks.ts"],
  "components/dashboard/widgets/attention-queue-renderer.tsx": ["configuration/crypto/lib/portal-overview.ts"],
  "components/dashboard/widgets/financial-summary-renderer.tsx": ["configuration/crypto/lib/portal-overview.ts"],
  "components/dashboard/widgets/platform-fees-renderer.tsx": ["configuration/crypto/lib/portal-overview.ts"],
  "components/dashboard/widgets/processing-health-renderer.tsx": ["configuration/crypto/hooks/useAdminNetworks.ts", "configuration/crypto/types/crypto-network.ts"],
  "lib/admin-dashboard.ts": ["configuration/crypto/lib/crypto-network-readiness.ts", "configuration/crypto/types/crypto-network.ts"],
  "lib/api/catalogs.ts": ["configuration/crypto/types/crypto-network.ts"],
  "lib/api.ts": ["configuration/crypto/lib/api/crypto.ts", "configuration/crypto/lib/api/portal.ts"],
  "lib/entity-registry-defaults.ts": [
    "configuration/crypto/components/catalogs/crypto-provider-account-form.tsx",
    "configuration/crypto/components/crypto/aml-risk.tsx",
    "configuration/crypto/components/documents/crypto-custody-receipt-card.tsx",
    "configuration/crypto/components/documents/crypto-sweep-card.tsx",
  ],
  "lib/widget-registry.ts": [
    "configuration/crypto/components/dashboard/widgets/custody-reconciliation-renderer.tsx",
    "configuration/crypto/components/dashboard/widgets/receipts-volume-renderer.tsx",
  ],
}

const importPattern = /(?:from|import)\s*\(?\s*["']([^"']+)["']/g

function sourceFiles(dir: string): string[] {
  return readdirSync(join(root, dir)).flatMap(name => {
    const path = join(dir, name)
    if (statSync(join(root, path)).isDirectory()) return name === "generated" ? [] : sourceFiles(path)
    return /\.tsx?$/.test(name) ? [path] : []
  })
}

function resolveImport(importer: string, spec: string): string | undefined {
  const base = spec.startsWith("@/") ? spec.slice(2) : spec.startsWith(".") ? normalize(join(dirname(importer), spec)) : undefined
  if (base === undefined) return undefined
  return ["", ".ts", ".tsx", "/index.ts", "/index.tsx"].map(ext => base + ext).find(path => existsSync(join(root, path)) && statSync(join(root, path)).isFile())
}

test("platform UI does not import the configuration", () => {
  const actual: Record<string, Set<string>> = {}
  for (const file of platformDirs.flatMap(sourceFiles)) {
    for (const [, spec] of readFileSync(join(root, file), "utf8").matchAll(importPattern)) {
      const target = resolveImport(file, spec)
      if (target && relative("configuration", target).split(/[\\/]/)[0] !== "..") (actual[file] ??= new Set()).add(target)
    }
  }
  const unexpected = Object.entries(actual).flatMap(([file, targets]) => [...targets].filter(t => !knownConfigurationLeaks[file]?.includes(t)).map(t => `${file} -> ${t}`))
  const gone = Object.entries(knownConfigurationLeaks).flatMap(([file, targets]) => targets.filter(t => !actual[file]?.has(t)).map(t => `${file} -> ${t}`))
  expect(unexpected, "platform files import configuration; register through an extension point instead").toEqual([])
  expect(gone, "these leaks are gone; remove them from knownConfigurationLeaks").toEqual([])
})
