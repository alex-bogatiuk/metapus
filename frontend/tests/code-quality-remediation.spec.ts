import { test, expect, type Page } from "@playwright/test"
import { commonAPIResponse, erpUser, ids, installSession, json } from "./helpers"
import type { CounterpartyResponse } from "../types/catalog"
import { AutomationRuleResponseSchema, CounterpartyResponseSchema } from "../lib/generated/http-contract-schemas"
import { jsonObjectSchema, type JsonObject } from "../lib/json-value"

const admin = { ...erpUser, isAdmin: true }
const timestamp = "2026-10-03T12:00:00Z"
const channel = { id: ids.address, name: "Тестовый канал", accountId: ids.network, destination: {}, isActive: true, deletionMark: false, version: 1, createdAt: timestamp, updatedAt: timestamp, ruleCount: 0 }
const rule = {
  id: ids.wallet, name: "Исходное правило", triggerType: "entity_event", eventType: "posted", targetEntities: [],
  reactionType: "notify", notifSeverity: "info", messageFormat: "text", actionTemplate: "Message", priority: 0, maxRetries: 0, cooldownSeconds: 0,
  isActive: true, executionCount: 0, errorCount: 0, deletionMark: false, version: 7, createdAt: timestamp, updatedAt: timestamp,
  subscribers: [{ id: ids.merchant, ruleId: ids.wallet, subscriberType: "channel", channelId: channel.id, deliveryMethod: "push", idx: 1 }],
}
const counterparty = { fullName: null, inn: null, id: ids.token, name: "Исходный контрагент", code: "CP-001", type: "supplier", legalForm: "company", isFolder: false, deletionMark: false, version: 7 } satisfies CounterpartyResponse

function responseRecord(isRule: boolean, body: JsonObject, version: number) {
  const fields = Object.fromEntries(Object.entries(body).filter(([key, value]) => value !== null || (!isRule && ["fullName", "inn"].includes(key))))
  if (isRule) {
    const subscribers = Array.isArray(body.subscribers) ? body.subscribers.map(value => ({
      ...Object.fromEntries(Object.entries(jsonObjectSchema.parse(value)).filter(([, field]) => field !== null)),
      id: ids.merchant, ruleId: rule.id,
    })) : rule.subscribers
    const reportConfig = body.reportConfig ? { ...jsonObjectSchema.parse(body.reportConfig), executorUserId: admin.id } : undefined
    return AutomationRuleResponseSchema.parse({ ...rule, ...fields, subscribers, reportConfig, version })
  }
  return CounterpartyResponseSchema.parse({ ...counterparty, ...fields, version })
}

async function setup(page: Page, failResources = false) {
  await installSession(page, admin)
  const writes: JsonObject[] = []
  let resourcesFail = failResources
  await page.route("**/api/v1/**", async route => {
    const path = new URL(route.request().url()).pathname
    const common = commonAPIResponse(path, admin)
    if (common !== undefined) return json(route, common)
    if (path === "/api/v1/system/automation-channels") {
      if (resourcesFail) return json(route, { code: "INTERNAL_ERROR", message: "test failure" }, 503)
      return json(route, [channel])
    }
    if (path === "/api/v1/system/automation-meta/datasets") return json(route, [{ key: "document-journal", name: "Журнал документов" }])
    if (path === "/api/v1/meta/Counterparty/filters") return json(route, [
      { key: "type", label: "Тип", fieldType: "enum", enumValues: [{ value: "supplier", label: "Поставщик" }, { value: "customer", label: "Покупатель" }] },
      { key: "legalForm", label: "Правовая форма", fieldType: "enum", enumValues: [{ value: "company", label: "Организация" }, { value: "individual", label: "Физическое лицо" }] },
    ])
    if (path.includes("/automation-rules") || path.includes("/catalog/counterparties")) {
      const isRule = path.includes("/automation-rules")
      const original = isRule ? rule : counterparty
      if (["PUT", "POST"].includes(route.request().method())) {
        const body = jsonObjectSchema.parse(route.request().postDataJSON())
        writes.push(body)
        if (route.request().method() === "POST" && isRule) return json(route, { id: original.id })
        return json(route, responseRecord(isRule, body, 8))
      }
      return json(route, responseRecord(isRule, writes.at(-1) ?? {}, writes.length ? 8 : 7))
    }
    return json(route, { items: [], totalCount: 0, hasMore: false, hasPrev: false, unreadCount: 0 })
  })
  return { writes, recoverResources: () => { resourcesFail = false } }
}

for (const mode of ["new", "edit"] as const) {
  test(`shared counterparty ${mode} form saves fields and preserves version on edit`, async ({ page }) => {
    const { writes } = await setup(page)
    await page.goto(`/catalogs/counterparties/${mode === "new" ? "new" : ids.token}`, { waitUntil: "domcontentloaded" })
    await page.getByLabel("Наименование *", { exact: true }).fill("Проверенный контрагент")
    await page.getByLabel("ИНН", { exact: true }).fill("1234567890")
    await page.getByLabel("Тип *", { exact: true }).click()
    await page.getByRole("option", { name: "Покупатель", exact: true }).click()
    await page.getByRole("button", { name: "Записать", exact: true }).click()
    await expect.poll(() => writes.length).toBe(1)
    expect(writes[0]).toMatchObject({ name: "Проверенный контрагент", inn: "1234567890", type: "customer", legalForm: "company" })
    if (mode === "edit") expect(writes[0].version).toBe(7)
    else expect(writes[0]).not.toHaveProperty("version")
    await expect(page.getByRole("button", { name: /^Вкладка:/ }).filter({ hasText: "●" })).toHaveCount(0)
  })

  test(`shared automation ${mode} form saves report and subscriber settings`, async ({ page }) => {
    const { writes } = await setup(page)
    await page.goto(`/admin/automation-rules/${mode === "new" ? "new" : ids.wallet}`, { waitUntil: "domcontentloaded" })
    await page.getByLabel("Наименование *", { exact: true }).fill("Проверенное правило")
    await page.getByLabel("Тип действия *", { exact: true }).click()
    await page.getByRole("option", { name: /Сформировать отчёт/ }).click()
    await page.getByLabel("Набор данных *", { exact: true }).click()
    await page.getByRole("option", { name: "Журнал документов", exact: true }).click()
    await page.getByLabel("Часовой пояс", { exact: true }).fill("Europe/Moscow")
    if (mode === "new") await page.getByRole("button", { name: "Добавить", exact: true }).click()
    await page.getByRole("button", { name: "Записать", exact: true }).click()
    await expect.poll(() => writes.length).toBe(1)
    expect(writes[0]).toMatchObject({ name: "Проверенное правило", reactionType: "generate_report", reportConfig: { datasetKey: "document-journal", timezone: "Europe/Moscow", includeEmptyReport: false } })
    expect(writes[0].subscribers).toEqual([expect.objectContaining({ subscriberType: "channel", channelId: channel.id })])
    if (mode === "edit") expect(writes[0].version).toBe(7)
    await expect(page.getByRole("button", { name: /^Вкладка:/ }).filter({ hasText: "●" })).toHaveCount(0)
  })
}

test("shared automation form displays a resource error and recovers after retry", async ({ page }) => {
  const server = await setup(page, true)
  await page.goto("/admin/automation-rules/new", { waitUntil: "domcontentloaded" })
  await expect(page.getByText("Не удалось загрузить каналы и отчёты")).toBeVisible()
  server.recoverResources()
  await page.getByRole("button", { name: "Повторить", exact: true }).click()
  await expect(page.getByText("Не удалось загрузить каналы и отчёты")).toHaveCount(0)
  await expect(page.getByText("Загрузка каналов и отчётов…", { exact: true })).toHaveCount(0)
  await page.getByRole("button", { name: "Добавить", exact: true }).click()
  await expect(page.getByRole("combobox").filter({ hasText: "Тестовый канал" })).toBeVisible()
})
