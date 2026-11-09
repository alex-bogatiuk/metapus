import { expect, test, type Page } from "@playwright/test"
import { commonAPIResponse, ids, installPortalSession, json, portalOwner, portalRoles, unvaluedPortalBalance } from "./helpers"

const assets = [
  {
    tokenId: ids.token,
    networkId: ids.network,
    addressId: ids.address,
    symbol: "PYUSD",
    network: "Ethereum Sepolia",
    address: "0x1234567890aBcDEF1234567890abCDef12345678",
  },
  {
    tokenId: "20000000-0000-4000-8000-000000000003",
    networkId: "20000000-0000-4000-8000-000000000004",
    addressId: "20000000-0000-4000-8000-000000000006",
    symbol: "USDT",
    network: "TRON Nile",
    address: "TQXx1eKQtVFjoDvyiGc3cRmJkSWE9KeXni",
  },
] as const

async function setup(page: Page) {
  await installPortalSession(page)
  const mutations: string[] = []
  await page.route("**/api/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname
    return json(route, commonAPIResponse(path, portalOwner) ?? { items: [] })
  })
  await page.route("**/portal/v1/**", (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    // No test in this suite may create a withdrawal or touch a real backend.
    if (request.method() !== "GET") {
      mutations.push(path)
      return json(route, { code: "UNEXPECTED_MUTATION" }, 500)
    }
    if (path === "/portal/v1/merchants") {
      return json(route, { items: [{ id: ids.merchant, name: "Acme", code: "ACME", role: portalRoles.owner, networkAccess: "allow" }] })
    }
    if (path === "/portal/v1/dashboard/currencies") {
      return json(route, { items: assets.map((asset) => ({ ...asset, count: 1, totalMinor: "100000000", invoiceSharePct: "50", decimalPlaces: 6 })) })
    }
    if (path === "/portal/v1/dashboard/balance") {
      return json(route, unvaluedPortalBalance([...assets]))
    }
    if (path === "/portal/v1/balances/risk") {
      return json(route, { snapshotId: "risk-snapshot", asOf: new Date().toISOString(), freshUntil: new Date(Date.now() + 60000).toISOString(), items: assets.map((asset) => ({
        merchantId: ids.merchant, networkId: asset.networkId, tokenId: asset.tokenId,
        symbol: asset.symbol, network: asset.network, decimalPlaces: 6,
        routingEnabled: true, scopeVersion: 1, allocationComplete: true, reason: "",
        hasHighExposure: true, ledgerBalance: "400000000", pendingChain: "0", pendingRestoration: "0",
        total: "400000000", free: "200000000", reserved: "0", technicalHold: "200000000",
        buckets: ["low", "medium", "high", "unknown"].map((riskClass) => ({
          riskClass, total: "100000000", free: ["low", "medium"].includes(riskClass) ? "100000000" : "0",
          reserved: "0", technicalHold: ["high", "unknown"].includes(riskClass) ? "100000000" : "0",
        })),
      })) })
    }
    if (path === "/portal/v1/withdrawal-addresses") {
      return json(route, { items: assets.map((asset) => ({
        id: asset.addressId, networkId: asset.networkId, network: asset.network,
        address: asset.address, label: "External", createdAt: "2026-09-03T12:00:00Z",
      })) })
    }
    return json(route, { items: [] })
  })
  await page.goto("/portal/withdrawals/new")
  return mutations
}

async function selectRecipient(page: Page, asset: typeof assets[number]) {
  await page.getByLabel("Токен", { exact: true }).click()
  await page.getByRole("option", { name: new RegExp(asset.symbol) }).click()
  await page.getByLabel("Адрес назначения", { exact: true }).click()
  await page.getByRole("option", { name: new RegExp(asset.address.slice(0, 8)) }).click()
}

for (const width of [320, 768, 1024, 1440]) {
  for (const asset of assets) {
    test(`${asset.network}: complete recipient is readable and copied at ${width}px`, async ({ page, context }) => {
      await page.setViewportSize({ width, height: 1000 })
      await context.grantPermissions(["clipboard-read", "clipboard-write"])
      const mutations = await setup(page)
      const summary = page.getByRole("region", { name: "Сводка" })
      const copy = summary.getByRole("button", { name: "Скопировать адрес" })
      await expect(copy).toBeDisabled()
      await selectRecipient(page, asset)

      const recipient = summary.getByTestId("withdrawal-recipient-address")
      await expect(recipient).toHaveText(asset.address)
      await expect(recipient).toBeVisible()
      await expect(summary.getByText(`${asset.symbol} (${asset.network})`, { exact: true })).toBeVisible()
      const layout = await recipient.evaluate((element) => {
        const style = getComputedStyle(element)
        const range = document.createRange()
        range.selectNodeContents(element)
        const bounds = element.getBoundingClientRect()
        const textRects = [...range.getClientRects()]
        return {
          whiteSpace: style.whiteSpace,
          textOverflow: style.textOverflow,
          wordBreak: style.wordBreak,
          scrollWidth: element.scrollWidth,
          clientWidth: element.clientWidth,
          allTextInside: textRects.length > 0 && textRects.every((rect) =>
            rect.left >= bounds.left - 1 && rect.right <= bounds.right + 1 &&
            rect.top >= bounds.top - 1 && rect.bottom <= bounds.bottom + 1),
        }
      })
      expect(layout.whiteSpace).toBe("normal")
      expect(layout.textOverflow).toBe("clip")
      expect(layout.wordBreak).toBe("break-all")
      expect(layout.scrollWidth).toBeLessThanOrEqual(layout.clientWidth)
      expect(layout.allTextInside).toBe(true)
      expect(await summary.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

      await copy.click()
      await expect(page.getByText("Адрес скопирован", { exact: true })).toBeVisible()
      expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(asset.address)
      expect(mutations).toEqual([])
      await page.screenshot({ path: test.info().outputPath("recipient-summary.png"), fullPage: true })
    })
  }
}

test("changing network clears the previous recipient and disables copying", async ({ page }) => {
  const mutations = await setup(page)
  await selectRecipient(page, assets[0])
  await page.getByLabel("Токен", { exact: true }).click()
  await page.getByRole("option", { name: /USDT/ }).click()
  const summary = page.getByRole("region", { name: "Сводка" })
  await expect(summary.getByTestId("withdrawal-recipient-address")).toHaveText("Не выбран")
  await expect(summary.getByRole("button", { name: "Скопировать адрес" })).toBeDisabled()
  await expect(summary.getByRole("button", { name: "Отправить заявку" })).toBeDisabled()
  await page.getByLabel("Адрес назначения", { exact: true }).click()
  await page.getByRole("option", { name: new RegExp(assets[1].address.slice(0, 8)) }).click()
  await expect(summary.getByTestId("withdrawal-recipient-address")).toHaveText(assets[1].address)
  expect(mutations).toEqual([])
})

for (const clipboardMode of ["denied", "unavailable"]) {
  test(`clipboard ${clipboardMode}: full address remains available for manual copying`, async ({ page }) => {
    const mutations = await setup(page)
    await selectRecipient(page, assets[0])
    await page.evaluate((mode) => {
      Object.defineProperty(navigator, "clipboard", {
        configurable: true,
        value: mode === "unavailable" ? undefined : {
          writeText: () => Promise.reject(new DOMException("Denied", "NotAllowedError")),
        },
      })
    }, clipboardMode)
    const summary = page.getByRole("region", { name: "Сводка" })
    await summary.getByRole("button", { name: "Скопировать адрес" }).click()
    await expect(page.getByText("Не удалось скопировать адрес. Выделите его и скопируйте вручную.", { exact: true })).toBeVisible()
    await expect(page.getByText("Адрес скопирован", { exact: true })).toHaveCount(0)
    await expect(summary.getByTestId("withdrawal-recipient-address")).toHaveText(assets[0].address)
    expect(mutations).toEqual([])
  })
}


test("high and unknown exports require acceptance for the selected class", async ({ page }) => {
  const mutations = await setup(page)
  await selectRecipient(page, assets[0])
  await page.getByLabel("Сумма", { exact: true }).fill("12.5")
  const submit = page.getByRole("button", { name: "Отправить заявку" })
  await expect(submit).toBeEnabled()
  const risk = page.getByRole("group", { name: "Класс риска средств" })
  await risk.getByRole("radio", { name: "Высокий", exact: true }).click()
  await expect(submit).toBeDisabled()
  await page.getByRole("checkbox", { name: "Подтверждаю вывод средств с высоким риском" }).check()
  await expect(submit).toBeEnabled()
  await risk.getByRole("radio", { name: "Неизвестный", exact: true }).click()
  const acceptance = page.getByRole("checkbox", { name: "Подтверждаю вывод средств с неизвестным риском" })
  await expect(acceptance).not.toBeChecked()
  await expect(submit).toBeDisabled()
  await acceptance.check()
  await expect(submit).toBeEnabled()
  await risk.getByRole("radio", { name: "Низкий", exact: true }).click()
  await expect(acceptance).toHaveCount(0)
  await expect(submit).toBeEnabled()
  expect(mutations).toEqual([])
})


test("accepted high export sends its exact class in the request", async ({ page }) => {
  await setup(page)
  await page.route("**/portal/v1/withdrawal-requests?merchant_id=*", (route) => {
    if (route.request().method() !== "POST") return route.fallback()
    return json(route, { object: "withdrawal_request", version: 1, id: ids.address, number: "WR-AML-1", status: "pending_signature" })
  })
  await selectRecipient(page, assets[0])
  await page.getByLabel("Сумма", { exact: true }).fill("12.5")
  await page.getByRole("group", { name: "Класс риска средств" }).getByRole("radio", { name: "Высокий", exact: true }).click()
  await page.getByRole("checkbox", { name: "Подтверждаю вывод средств с высоким риском" }).check()
  const request = page.waitForRequest((candidate) => candidate.method() === "POST" && candidate.url().includes("/portal/v1/withdrawal-requests?"))
  await page.getByRole("button", { name: "Отправить заявку" }).click()
  expect((await request).postDataJSON()).toEqual({
    tokenId: assets[0].tokenId, addressId: assets[0].addressId,
    amount: "12.5", riskClass: "high", riskAccepted: true,
  })
  await expect(page).toHaveURL(/\/portal\/withdrawals\/requests$/)
})
