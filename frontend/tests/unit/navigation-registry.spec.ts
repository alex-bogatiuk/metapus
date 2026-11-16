import { test, expect } from "@playwright/test"
import { Star } from "lucide-react"
import { getNavigation, registerNavigation } from "../../lib/navigation"
import { getStaticNavItems } from "../../lib/command-nav-items"
import { getProfilePresets, registerProfilePreset } from "../../lib/security-registry"

test("a contribution extends groups, inserts groups and links at their anchors", () => {
  registerNavigation({
    sections: [
      {
        section: "Справочники",
        groups: [
          { label: "Бизнес и контрагенты", prepend: true, items: [{ entityKey: "probe_first", fallback: "Первый" }] },
          { label: "Новая группа", after: "Бизнес и контрагенты", items: [{ entityKey: "probe_group", fallback: "Группа" }] },
          { label: "Интеграции", items: [{ entityKey: "probe_last", fallback: "Последний" }] },
        ],
      },
      { section: "Новый раздел", icon: Star, groups: [{ label: "Г", items: [{ entityKey: "probe_section", fallback: "Р" }] }] },
    ],
    systemLinks: [{ title: "Проба", url: "/probe", icon: Star, before: "/help" }],
    commands: [{ id: "nav:probe", label: "Проба", url: "/probe", keywords: "проба", section: "navigate" }],
  })
  const { sections, systemLinks } = getNavigation()
  const catalogs = sections.find(s => s.title === "Справочники")!
  // Other test files in this worker may have registered the application configuration too.
  const labels = catalogs.groups.map(g => g.label)
  expect(labels.slice(0, 2)).toEqual(["Бизнес и контрагенты", "Новая группа"])
  expect(labels.indexOf("Интеграции")).toBeLessThan(labels.indexOf("Финансы и валюты"))
  expect(catalogs.groups[0].items[0].entityKey).toBe("probe_first")
  expect(catalogs.groups.find(g => g.label === "Интеграции")?.items.at(-1)?.entityKey).toBe("probe_last")
  expect(sections.at(-1)?.title).toBe("Новый раздел")
  expect(systemLinks.map(l => l.url).slice(-2)).toEqual(["/probe", "/help"])
  const command = getStaticNavItems().find(i => i.id === "nav:probe")
  expect(command?.icon).toBeDefined()
})

test("a preset goes after its anchor and an extension adds to an existing preset", () => {
  registerProfilePreset({ code: "probe", name: "Проба", description: "", icon: Star, dimensions: {}, fieldPolicies: [], after: "viewer" })
  registerProfilePreset({ code: "viewer", fieldPolicies: [{ entityName: "Probe", action: "read", allowedFields: ["*", "-secret"] }] })
  const presets = getProfilePresets()
  const codes = presets.map(p => p.code)
  expect(codes.indexOf("probe")).toBe(codes.indexOf("viewer") + 1)
  expect(presets.find(p => p.code === "viewer")?.fieldPolicies.at(-1)?.entityName).toBe("Probe")
})
