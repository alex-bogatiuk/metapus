import { execFileSync } from "node:child_process"
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { expect, test } from "@playwright/test"
// The generator is a plain CommonJS Node script.
const { configurations, configure, current, plan, NONE, root } = require("../../scripts/configure.cjs")

const routeWrapper = /^export \{ default \} from "@\/configuration\/[^"]+"\n$/

// The committed app/ holds the platform's files only: the route wrappers and
// app/configuration.ts are assembled per configuration.
function committedAppFiles(): string[] {
  return execFileSync("git", ["ls-files", "app"], { cwd: root, encoding: "utf8" }).split("\n").filter(Boolean)
}

test("every configuration assembles app/ without shadowing a platform file", () => {
  const committed = new Set(committedAppFiles())
  for (const name of [NONE, ...configurations()]) {
    const files: Map<string, string> = plan(name)
    expect([...files.keys()], name).toContain(join("app", "configuration.ts"))
    for (const [path, content] of files) {
      expect(committed.has(path.split("\\").join("/")), `${name}: ${path} is committed`).toBe(false)
      if (!path.endsWith("configuration.ts")) {
        expect(content, path).toMatch(routeWrapper)
        const target = content.match(/from "@\/([^"]+)"/)![1]
        expect(existsSync(join(root, target + ".tsx")), `${path} -> ${target}`).toBe(true)
      }
    }
  }
})

test("the committed app/ holds no generated file", () => {
  const generated = committedAppFiles().filter(file =>
    file === "app/configuration.ts" || file === "app/.gitignore" || routeWrapper.test(readFileSync(join(root, file), "utf8")))
  expect(generated, "run `npm run configure` instead of committing generated files").toEqual([])
})

test("assembling another configuration replaces the previous one's files", () => {
  const base = mkdtempSync(join(tmpdir(), "metapus-configure-"))
  const page = join("configuration", "probe", "pages", "(main)", "probe", "[id]", "page.tsx")
  mkdirSync(join(base, "app", "(main)"), { recursive: true })
  writeFileSync(join(base, "app", "(main)", "layout.tsx"), "platform layout\n")
  mkdirSync(join(base, page, ".."), { recursive: true })
  writeFileSync(join(base, page), "export default function Page() { return null }\n")
  writeFileSync(join(base, "configuration", "probe", "manifest.json"), JSON.stringify({ register: "registerProbe" }))

  configure("probe", base)
  const wrapper = join(base, "app", "(main)", "probe", "[id]", "page.tsx")
  expect(readFileSync(wrapper, "utf8")).toBe('export { default } from "@/configuration/probe/pages/(main)/probe/[id]/page"\n')
  expect(readFileSync(join(base, "app", "configuration.ts"), "utf8")).toContain("registerProbe()")
  expect(current(base).name).toBe("probe")

  configure(NONE, base)
  expect(existsSync(wrapper)).toBe(false)
  expect(existsSync(join(base, "app", "(main)", "probe")), "empty route directories are pruned").toBe(false)
  expect(readFileSync(join(base, "app", "(main)", "layout.tsx"), "utf8")).toBe("platform layout\n")
  expect(current(base)).toEqual({ name: NONE, files: [join("app", "configuration.ts")] })

  // A configuration page may not take a platform route.
  const shadow = join("configuration", "probe", "pages", "(main)", "layout.tsx")
  cpSync(join(base, page), join(base, shadow))
  expect(() => configure("probe", base)).toThrow(/platform file/)
  expect(() => plan("Probe", base)).toThrow(/lowercase identifier/)
  expect(() => plan("missing", base)).toThrow(/manifest.json is missing/)
})

test("an add-on is assembled on its base, which registers first", () => {
  const base = mkdtempSync(join(tmpdir(), "metapus-configure-"))
  const pages = (name: string) => join("configuration", name, "pages", "(main)", "orders")
  for (const name of ["store", "shop"]) mkdirSync(join(base, pages(name)), { recursive: true })
  mkdirSync(join(base, "configuration", "store", "pages", "(main)", "stock"), { recursive: true })
  writeFileSync(join(base, pages("store"), "page.tsx"), "export default function Page() { return null }\n")
  writeFileSync(join(base, "configuration", "store", "pages", "(main)", "stock", "page.tsx"), "export default function Page() { return null }\n")
  writeFileSync(join(base, pages("shop"), "page.tsx"), "export default function Page() { return null }\n")
  writeFileSync(join(base, "configuration", "store", "manifest.json"), JSON.stringify({ register: "registerStore" }))
  writeFileSync(join(base, "configuration", "shop", "manifest.json"), JSON.stringify({ register: "registerShop", base: "store" }))

  const files: Map<string, string> = plan("shop", base)
  const registration = files.get(join("app", "configuration.ts"))!
  expect(registration.indexOf("registerStore()")).toBeGreaterThan(-1)
  expect(registration.indexOf("registerStore()"), "the base registers first").toBeLessThan(registration.indexOf("registerShop()"))
  expect(files.get(join("app", "(main)", "stock", "page.tsx")), "the base's pages come along").toContain("configuration/store/")
  expect(files.get(join("app", "(main)", "orders", "page.tsx")), "an add-on page replaces the base's").toContain("configuration/shop/")

  writeFileSync(join(base, "configuration", "store", "manifest.json"), JSON.stringify({ register: "registerStore", base: "shop" }))
  expect(() => plan("shop", base), "an add-on is not built on an add-on").toThrow(/is an add-on itself/)
  writeFileSync(join(base, "configuration", "shop", "manifest.json"), JSON.stringify({ register: "registerShop", base: "shop" }))
  expect(() => plan("shop", base)).toThrow(/must name another configuration/)
})
