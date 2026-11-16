import { existsSync, readdirSync, readFileSync, statSync } from "node:fs"
import { dirname, join, normalize, relative } from "node:path"
import { expect, test } from "@playwright/test"

// The platform UI (components, lib, hooks, stores, types) is reusable by any
// configuration; the crypto-processing configuration lives in configuration/.
// app/ is the composition root and may import both.
const root = join(__dirname, "..", "..")
const platformDirs = ["components", "lib", "hooks", "stores", "types"]

// knownConfigurationLeaks lists tolerated platform → configuration imports.
// It is empty: register configuration parts through a platform extension
// point (entity overlays, widgets, the configuration's own API client)
// instead of importing them here.
const knownConfigurationLeaks: Record<string, string[]> = {}

const importPattern = /(?:from|import)\s*\(?\s*["']([^"']+)["']/g

function sourceFiles(dir: string): string[] {
  return readdirSync(join(root, dir)).flatMap(name => {
    const path = join(dir, name)
    // Generated wire files are included: the platform's describe platform
    // contracts only, each configuration's live in configuration/<name>/.
    if (statSync(join(root, path)).isDirectory()) return sourceFiles(path)
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

// The platform UI names no configuration concept either: navigation, labels,
// icons and access rules of a configuration are registered from its own code.
// components/ui wraps Radix primitives, whose Portal is unrelated.
const configurationVocabulary = /crypto|merchant|portal|blockchain|wallet|withdrawal|treasury/i
const webCrypto = /\bcrypto\??\.randomUUID\b/g

test("platform UI does not use the configuration's vocabulary", () => {
  const hits = platformDirs.flatMap(sourceFiles).filter(file => !file.startsWith(join("components", "ui"))).flatMap(file =>
    readFileSync(join(root, file), "utf8").split("\n").flatMap((line, index) =>
      configurationVocabulary.test(line.replace(webCrypto, "")) ? [`${file}:${index + 1}: ${line.trim().slice(0, 120)}`] : []))
  expect(hits, "move configuration-specific code and wording into configuration/ and register it").toEqual([])
})

// app/ is the composition root. It reaches the configuration only through
// app/configuration.ts and one-line route wrappers that re-export a page or
// layout the configuration provides; the platform's own routes stay neutral.
const routeWrapper = /^export \{ default \} from "@\/configuration\/[^"]+"\r?\n?$/
const moduleSpecifier = /(?:from|import)\s*\(?\s*["'][^"']+["']/g

test("app/ reaches the configuration only through route wrappers", () => {
  const offenders = sourceFiles("app").filter(file => file !== join("app", "configuration.ts")).flatMap(file => {
    const source = readFileSync(join(root, file), "utf8")
    const importsConfiguration = [...source.matchAll(importPattern)].some(([, spec]) => {
      const target = resolveImport(file, spec)
      return target !== undefined && relative("configuration", target).split(/[\\/]/)[0] !== ".."
    })
    if (importsConfiguration && !routeWrapper.test(source)) return [`${file}: imports the configuration outside a route wrapper`]
    return source.split("\n").flatMap((line, index) =>
      configurationVocabulary.test(line.replace(moduleSpecifier, "").replace(webCrypto, "")) ? [`${file}:${index + 1}: ${line.trim().slice(0, 120)}`] : [])
  })
  expect(offenders, "register the configuration's parts or move its page into configuration/ behind a route wrapper").toEqual([])
})
