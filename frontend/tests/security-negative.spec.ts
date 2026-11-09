import { expect, test } from "@playwright/test"

import { commonAPIResponse, ids, installPortalSession, json, portalOwner, portalRoles } from "./helpers"

test("invoice detail keeps merchant scope and exposes no BOLA data", async ({ page }) => {
  await installPortalSession(page)
  const foreignInvoiceId = "20000000-0000-4000-8000-000000000001"
  let requestedMerchant = ""

  await page.route("**/api/v1/**", async (route) => {
    const pathname = new URL(route.request().url()).pathname
    return json(route, commonAPIResponse(pathname, portalOwner) ?? { items: [] })
  })
  await page.route("**/portal/v1/**", async (route) => {
    const url = new URL(route.request().url())
    if (url.pathname === "/portal/v1/merchants") {
      return json(route, { items: [{
        id: ids.merchant,
        name: "Acme",
        code: "ACME",
        role: portalRoles.owner,
        networkAccess: "allow",
      }] })
    }
    if (url.pathname === `/portal/v1/invoices/${foreignInvoiceId}`) {
      requestedMerchant = url.searchParams.get("merchant_id") ?? ""
      return json(route, { code: "NOT_FOUND", message: "resource not found" }, 404)
    }
    return json(route, { items: [] })
  })

  await page.goto(`/portal/invoices/${foreignInvoiceId}`)
  const invoiceError = page.getByRole("alert").filter({ hasText: "Не удалось загрузить инвойс" })
  await expect(invoiceError).toBeVisible()
  await expect(invoiceError).toContainText("Проверьте доступ к выбранному мерчанту и попробуйте ещё раз.")
  await expect(page.getByText("0xforeign-wallet-secret", { exact: true })).toHaveCount(0)
  expect(requestedMerchant).toBe(ids.merchant)
})
