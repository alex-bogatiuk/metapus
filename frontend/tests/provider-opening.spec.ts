import { expect, test, type Page } from "@playwright/test"
import { commonAPIResponse, erpUser, ids, installSession, json } from "./helpers"
import type { ActionAvailability, EntityActionDef } from "../types/metadata"

const prefix = "crypto-provider-balance-operation"
const path = `/api/v1/document/${prefix}/${ids.adjustment}`
const reason = "Проверены основание и баланс провайдера"
const actions: EntityActionDef[] = ["approve", "reject"].map((key) => ({
    key, label: key === "approve" ? "Одобрить" : "Отклонить", method: "POST", path: `/:id/${key}`, scope: "item",
    permission: `document:crypto_provider_balance_operation:${key}`, idempotent: true, destructive: key === "reject",
    allowedStates: ["pending_approval"], ui: { formToolbar: true }, input: { kind: "reasonedDecision", summaryFields: ["number", "externalRef", "amount"], reasonMaxBytes: 2000 },
}))
const metadata = {
    key: "crypto_provider_balance_operation", name: "CryptoProviderBalanceOperation", type: "document", routePrefix: prefix,
    presentation: { singular: "Баланс провайдера", plural: "Балансы провайдера" },
    capabilities: { list: true, read: true, create: false, update: false, delete: false, setDeletionMark: false, post: false, unpost: false, repost: false, copy: false, batchAction: false, batchActionByFilter: false, exportList: true, printForms: false, relatedDocuments: false, movements: true },
    actions,
    fields: [{ name: "number", label: "Номер", type: "string" }, { name: "amount", label: "Сумма", type: "string" },
        { name: "externalRef", label: "Основание", type: "string" }, { name: "approvalReason", label: "Причина одобрения", type: "string" },
        { name: "rejectionReason", label: "Причина отклонения", type: "string" }],
}
type Scenario = "success" | "mfa" | "lost" | "stale" | "cancel" | "expired"
async function setup(page: Page, mode: "admin_self" | "independent" | "deny" | "missing", scenario: Scenario = "success") {
    const user = { ...erpUser, isAdmin: mode === "admin_self" }
    await installSession(page, user)
    const availability: ActionAvailability = mode === "deny" ? { allowed: false, reasonCode: "PROVIDER_OPENING_SELF_DECISION_FORBIDDEN" } : { allowed: true, decisionMode: mode === "independent" ? mode : "admin_self" }
    const doc = { id: ids.adjustment, number: "PBO-TEST", date: "2026-10-03T12:00:00Z", deletionMark: false, createdAt: "2026-10-03T12:00:00Z", updatedAt: "2026-10-03T12:00:00Z",
        postedVersion: 0, providerAccountId: ids.network, tokenId: ids.token, operationType: "opening", recordType: "receipt",
        amount: "989250000", externalRef: "opening-observation:verified", status: "pending_approval", version: 1, posted: false,
        decisionMode: "", decisionPolicyVersion: 0, approvalReason: "", rejectionReason: "", actionAvailability: mode === "missing" ? {} : { approve: availability, reject: availability } }
    const requests: { key: string; body: { version: number; reason: string }; action: string }[] = []
    await page.route("**/api/v1/**", async (route) => {
        const request = route.request()
        const url = new URL(request.url())
        if (url.pathname === "/api/v1/meta/entities") return json(route, [metadata])
        if (url.pathname.startsWith("/api/v1/meta/")) return json(route, metadata)
        const common = commonAPIResponse(url.pathname, user)
        if (common !== undefined) return json(route, common)
        if (url.pathname === path) return json(route, doc)
        if (url.pathname === "/api/v1/auth/refresh") return json(route, { accessToken: "test-refreshed", tokenType: "Bearer", expiresAt: "2099-01-01T00:00:00Z", cacheScope: { tenantId: "e2e-tenant", principalId: user.id, sessionId: "10000000-0000-4000-8000-000000000010", userVersion: "1", policyVersion: "1" } })
        if (url.pathname === "/api/v1/auth/step-up") return json(route, { expiresAt: "2099-01-01T00:00:00Z" })
        if (url.pathname === `${path}/approve` || url.pathname === `${path}/reject`) {
            const action = url.pathname.endsWith("/approve") ? "approve" : "reject"
            requests.push({ key: request.headers()["idempotency-key"] ?? "", body: request.postDataJSON(), action })
            if ((["mfa", "cancel"].includes(scenario) && requests.length === 1) || scenario === "expired") {
                return json(route, { code: "STEP_UP_REQUIRED", message: "Нужно свежее подтверждение", details: { actionCode: `crypto.provider_balance.${action}:test-object-fingerprint` } }, 403)
            }
            if (scenario === "stale" && requests.length === 1) {
                doc.version = 2
                return json(route, { code: "CONCURRENT_MODIFICATION", message: "Документ изменён. Обновите карточку." }, 409)
            }
            doc.status = action === "approve" ? "posted" : "rejected"
            doc.posted = action === "approve"
            doc.version = 2
            doc.decisionMode = mode
            doc.decisionPolicyVersion = 1
            if (action === "approve") doc.approvalReason = requests.at(-1)!.body.reason
            else doc.rejectionReason = requests.at(-1)!.body.reason
            if (scenario === "lost" && requests.length === 1) return route.abort("connectionreset")
            return json(route, doc)
        }
        return json(route, { items: [], total: 0 })
    })
    await page.goto(`/documents/${prefix}/${ids.adjustment}`)
    return requests
}
async function openDecision(page: Page, action = "Одобрить") {
    await page.getByRole("button", { name: action, exact: true }).click()
    const dialog = page.getByRole("dialog").filter({ has: page.getByRole("heading", { name: `${action} документ` }) })
    await dialog.getByLabel("Причина решения").fill(reason)
    return dialog
}
async function confirmMFA(page: Page) {
    const dialog = page.getByRole("dialog").filter({ hasText: "Подтверждение действия" })
    await dialog.getByLabel("Пароль").fill("test-fixture-only-password")
    await dialog.getByLabel("Код TOTP").fill("123456")
    await dialog.getByRole("button", { name: "Подтвердить", exact: true }).click()
}

for (const action of ["Одобрить", "Отклонить"]) {
    test(`administrator can ${action} own opening with explicit warning and MFA`, async ({ page }) => {
        const requests = await setup(page, "admin_self", "mfa")
        const dialog = await openDecision(page, action)
        await expect(dialog.getByText("Решение по собственному документу", { exact: true })).toBeVisible()
        await dialog.getByRole("button", { name: "Подтвердить решение" }).click()
        await confirmMFA(page)
        await expect(page.getByText("Решение принято автором-администратором", { exact: true })).toBeVisible()
        expect(requests).toHaveLength(2)
        expect(requests[0].key).toMatch(/^[0-9a-f-]{36}$/i)
        expect(requests[1].key).toBe(requests[0].key)
        expect(requests[1].body).toEqual({ version: 1, reason })
    })
}
test("ordinary author cannot decide, and missing availability fails closed", async ({ page }) => {
    await setup(page, "deny")
    await expect(page.getByRole("button", { name: "Одобрить", exact: true })).toBeDisabled()
    await expect(page.getByRole("button", { name: "Отклонить", exact: true })).toBeDisabled()
    await expect(page.getByText(/Одобрить: Решение по вашему документу должен принять другой оператор/)).toBeVisible()
})
test("missing server availability disables actions", async ({ page }) => {
    await setup(page, "missing")
    await expect(page.getByRole("button", { name: "Одобрить", exact: true })).toBeDisabled()
})
test("independent checker and duplicate submit produce one command", async ({ page }) => {
    const requests = await setup(page, "independent")
    const dialog = await openDecision(page)
    await expect(dialog.getByText("Решение по собственному документу", { exact: true })).toHaveCount(0)
    await dialog.locator("form").evaluate((form) => { form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })); form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })) })
    await expect(page.getByText("Независимое решение", { exact: true })).toBeVisible()
    expect(requests).toHaveLength(1)
})
test("lost response freezes reason and replays the same key", async ({ page }) => {
    const requests = await setup(page, "admin_self", "lost")
    const dialog = await openDecision(page)
    await dialog.getByRole("button", { name: "Подтвердить решение" }).click()
    await expect(dialog.getByRole("button", { name: "Повторить тот же запрос" })).toBeEnabled()
    await expect(dialog.getByLabel("Причина решения")).toBeDisabled()
    await expect(dialog.getByRole("button", { name: "Отмена", exact: true })).toBeDisabled()
    await dialog.getByRole("button", { name: "Повторить тот же запрос" }).click()
    await expect(page.getByText("Решение принято автором-администратором", { exact: true })).toBeVisible()
    expect(requests).toHaveLength(2)
    expect(requests[0]).toEqual(requests[1])
})
test("version conflict refetches and requires a new confirmed intent", async ({ page }) => {
    const requests = await setup(page, "admin_self", "stale")
    let dialog = await openDecision(page)
    await dialog.getByRole("button", { name: "Подтвердить решение" }).click()
    await expect(page.getByRole("button", { name: "Одобрить", exact: true })).toBeEnabled()
    dialog = await openDecision(page)
    await dialog.getByRole("button", { name: "Подтвердить решение" }).click()
    await expect(page.getByText("Решение принято автором-администратором", { exact: true })).toBeVisible()
    expect(requests).toHaveLength(2)
    expect(requests[1].body.version).toBe(2)
    expect(requests[1].key).not.toBe(requests[0].key)
})
test("MFA cancellation creates no decision and leaves an editable draft", async ({ page }) => {
    const requests = await setup(page, "admin_self", "cancel")
    const dialog = await openDecision(page)
    await dialog.getByRole("button", { name: "Подтвердить решение" }).click()
    const mfa = page.getByRole("dialog").filter({ hasText: "Подтверждение действия" })
    await mfa.getByRole("button", { name: "Отмена", exact: true }).click()
    await expect(dialog.getByLabel("Причина решения")).toBeEnabled()
    expect(requests).toHaveLength(1)
    await dialog.getByRole("button", { name: "Отмена", exact: true }).click()
    await expect(page.getByText("Решение принято автором-администратором", { exact: true })).toHaveCount(0)
})
test("expired object-bound MFA does not produce a decision or infinite retry", async ({ page }) => {
    const requests = await setup(page, "admin_self", "expired")
    const dialog = await openDecision(page)
    await dialog.getByRole("button", { name: "Подтвердить решение" }).click()
    await confirmMFA(page)
    await expect(dialog.getByLabel("Причина решения")).toBeEnabled()
    expect(requests).toHaveLength(2)
    expect(requests[1].key).toBe(requests[0].key)
})
