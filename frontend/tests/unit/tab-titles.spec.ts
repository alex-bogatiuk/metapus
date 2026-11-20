import { test, expect } from "@playwright/test"
import { Star } from "lucide-react"
import { registerNavigation } from "../../lib/navigation"
import { navigationTitleForUrl, resolveTitleFromUrl } from "../../lib/tab-utils"
import { useTabsStore } from "../../stores/useTabsStore"

registerNavigation({ systemLinks: [{ title: "Пробная сеть", url: "/admin/probe-networks", icon: Star }] })

test("a page the navigation names takes its label before metadata loads, not its section's", () => {
  expect(navigationTitleForUrl("/catalogs/integration-pools")).toBe("Пулы аккаунтов")
  expect(resolveTitleFromUrl("/catalogs/integration-pools")).toBe("Пулы аккаунтов")
  expect(resolveTitleFromUrl("/admin/probe-networks")).toBe("Пробная сеть")
  expect(resolveTitleFromUrl("/finance/exchange-rates")).toBe("Курсы валют")
  expect(resolveTitleFromUrl("/admin")).toBe("Управление")
})

test("an entity item matches only its list route, and an unknown page keeps the section guess", () => {
  expect(navigationTitleForUrl("/catalogs/integration-pools/0190a1b2-0000-7000-8000-000000000000")).toBeUndefined()
  expect(resolveTitleFromUrl("/catalogs/integration-pools/0190a1b2-0000-7000-8000-000000000000")).toBe("Пулы аккаунтов…")
  expect(navigationTitleForUrl("/settings/integration-pools")).toBeUndefined()
  expect(resolveTitleFromUrl("/catalogs/unknown-things")).toBe("Справочники…")
})

test("guessed titles are resolved again, a title a page set is kept", () => {
  useTabsStore.setState({ tabs: [
    { id: "/", title: "Главное", url: "/" },
    { id: "/a", title: "Старое", url: "/a", provisional: true },
    { id: "/b", title: "Справочники…", url: "/b" },
    { id: "/c", title: "0190a1b2-0000-7000-8000-000000000000", url: "/c" },
    { id: "/d", title: "Пул A (Пул аккаунтов)", url: "/d" },
  ] })
  useTabsStore.getState().updateTabTitle("/d", "Пул B (Пул аккаунтов)")
  useTabsStore.getState().refreshProvisionalTitles((pathname) => `новое ${pathname}`)
  expect(useTabsStore.getState().tabs.map((t) => t.title)).toEqual(["Главное", "новое /a", "новое /b", "новое /c", "Пул B (Пул аккаунтов)"])

  useTabsStore.getState().updateTabTitle("/a", "Пул C (Пул аккаунтов)")
  expect(useTabsStore.getState().tabs.find((t) => t.id === "/a")?.provisional).toBeUndefined()
})
