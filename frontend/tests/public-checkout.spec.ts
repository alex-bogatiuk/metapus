import { readFileSync } from "node:fs"
import { resolve } from "node:path"

import { expect, test } from "@playwright/test"

const checkoutDocument = readFileSync(
  resolve(process.cwd(), "../internal/configuration/crypto/infrastructure/http/cryptohttp/static/payment.html"),
  "utf8",
)

const publicRef = "ci_0123456789abcdef0123456789abcdef"

const invoice = {
  checkoutRef: publicRef,
  status: "created",
  acceptsPayments: true,
  walletAddress: "0x1111111111111111111111111111111111111111",
  expectedAmount: "12.500000",
  receivedAmount: "0.000000",
  overpaidAmount: "0.000000",
  tokenSymbol: "USDT",
  tokenName: "Tether USD",
  networkName: "Sepolia",
  explorerUrl: "https://sepolia.etherscan.io",
  decimalPlaces: 6,
  expiresAt: "2026-08-30T13:00:00Z",
  merchantName: "Acme",
  orderId: "ORDER-42",
  description: "Production checkout contract",
  confirmations: 0,
  requiredConfs: 12,
  qrCode: "",
}

test.beforeEach(async ({ page }) => {
  await page.clock.install({ time: new Date("2026-08-30T12:00:00Z") })
  await page.route("**/pay/i/*", async (route) => {
    await route.fulfill({ status: 200, contentType: "text/html; charset=utf-8", body: checkoutDocument })
  })
})

test("opaque checkout reference survives refresh and reaches terminal state", async ({ page }) => {
  const detailRefs: string[] = []
  const statusRefs: string[] = []

  await page.route("**/api/v1/checkout/invoices/**", async (route) => {
    const pathname = new URL(route.request().url()).pathname
    if (pathname.endsWith("/status")) {
      statusRefs.push(pathname.split("/").at(-2) ?? "")
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          status: "completed",
          acceptsPayments: false,
          receivedAmount: "12.500000",
          overpaidAmount: "0.000000",
          confirmations: 12,
        }),
      })
    }

    detailRefs.push(pathname.split("/").at(-1) ?? "")
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(invoice) })
  })

  await page.goto(`/pay/i/${publicRef}`)
  await expect(page.locator("#payment-card")).toBeVisible()
  await expect(page.locator("#merchant-name")).toHaveText("Acme")
  await expect(page.locator("#status-badge")).toHaveText("WAITING")
  await expect(page.locator("#amount-value")).toHaveText("12.5")

  await page.reload()
  await expect(page.locator("#wallet-address")).toHaveText(invoice.walletAddress)
  expect(detailRefs).toEqual([publicRef, publicRef])

  await page.clock.fastForward(5_000)
  await expect(page.locator("#status-badge")).toHaveText("COMPLETED")
  await expect(page.locator("#overlay-confirmed")).toBeVisible()
  expect(statusRefs).toEqual([publicRef])
})

const ethInvoice = {
  ...invoice,
  expectedAmount: "0.005000000000000000",
  receivedAmount: "0.000000000000000000",
  overpaidAmount: "0.000000000000000000",
  tokenSymbol: "ETH",
  tokenName: "Ether",
  decimalPlaces: 18,
}

test("provisional payment waits for confirmation even when the counter reaches the policy", async ({ page }) => {
	await page.route("**/api/v1/checkout/invoices/**", route => route.fulfill({ json: { ...invoice, status: "paid", acceptsPayments: false, confirmations: 12, receivedAmount: "12.500000" } }))
	await page.goto(`/pay/i/${publicRef}`)
	await expect(page.locator("#status-badge")).toHaveText("RECEIVED")
	await expect(page.locator("#payment-confirmation-hint")).toHaveText("Payment detected, awaiting confirmation")
	await expect(page.locator("#overlay-confirmed")).toBeHidden()
	await page.clock.fastForward(10_000)
	await expect(page.locator("#overlay-confirmed")).toBeHidden()
})

test("native ETH checkout trims insignificant zeros", async ({ page }) => {
  await page.route("**/api/v1/checkout/invoices/**", (route) => route.fulfill({ json: ethInvoice }))

  await page.goto(`/pay/i/${publicRef}`)
  await expect(page.locator("#amount-value")).toHaveText("0.005")
  await expect(page.locator("#amount-symbol")).toHaveText("ETH")
  await expect(page.locator("#partial-info")).toBeHidden()
})

for (const decimalPlaces of [0, 6, 8, 18]) {
  for (const delivery of ["initial load", "polling"]) {
    test(`exact payment with ${decimalPlaces} decimals succeeds on ${delivery}`, async ({ page }) => {
      const zero = decimalPlaces === 0 ? "0" : `0.${"0".repeat(decimalPlaces)}`
      const amount = decimalPlaces === 18
        ? "0.005000000000000000"
        : decimalPlaces === 0 ? "100" : `100.${"0".repeat(decimalPlaces)}`
      const displayedAmount = decimalPlaces === 18 ? "0.005" : "100"
      const completedInvoice = {
        ...ethInvoice,
        decimalPlaces,
        status: "completed",
        acceptsPayments: false,
        expectedAmount: amount,
        receivedAmount: amount,
        overpaidAmount: zero,
        confirmations: 12,
      }
      await page.route("**/api/v1/checkout/invoices/**", (route) => {
        const isPoll = new URL(route.request().url()).pathname.endsWith("/status")
        return route.fulfill({
          json: delivery === "initial load" || isPoll
            ? completedInvoice
            : { ...completedInvoice, status: "created", acceptsPayments: true, receivedAmount: zero, confirmations: 0 },
        })
      })

      await page.goto(`/pay/i/${publicRef}`)
      await expect(page.locator("#payment-card")).toBeVisible()
      if (delivery === "polling") {
        await expect(page.locator("#status-badge")).toHaveText("WAITING")
        await page.clock.fastForward(5_000)
      }

      await expect(page.locator("#overlay-confirmed")).toBeVisible()
      await expect(page.locator("#overlay-overpaid")).toBeHidden()
      await expect(page.locator("#amount-value")).toHaveText(displayedAmount)
      await expect(page.locator("#amount-label")).toHaveText("Сумма счёта")
      await expect(page.locator("#received-value")).toHaveText(`${displayedAmount} ETH`)
      await expect(page.locator("#remaining-value")).toHaveText("0 ETH")
      await expect(page.locator("#remaining-row")).toBeHidden()
      await expect(page.locator("#payment-state-title")).toBeHidden()
    })
  }
}

for (const payment of [
  { received: "0.005100000000000000", excess: "0.000100000000000000", display: "0.0001" },
  { received: "0.005000000000000001", excess: "0.000000000000000001", display: "0.000000000000000001" },
]) {
  test(`ETH overpayment of ${payment.display} is retained after polling`, async ({ page }) => {
    await page.route("**/api/v1/checkout/invoices/**", (route) => {
      const isPoll = new URL(route.request().url()).pathname.endsWith("/status")
      return route.fulfill({
        json: isPoll
          ? { status: "completed", acceptsPayments: false, receivedAmount: payment.received, overpaidAmount: payment.excess, confirmations: 12 }
          : ethInvoice,
      })
    })

    await page.goto(`/pay/i/${publicRef}`)
    await expect(page.locator("#status-badge")).toHaveText("WAITING")
    await page.clock.fastForward(5_000)

    await expect(page.locator("#overlay-overpaid")).toBeVisible()
    await expect(page.locator("#overlay-confirmed")).toBeHidden()
    await expect(page.locator("#overpaid-desc")).toHaveText(
      `Сумма перевода превышает сумму инвойса. Переплата: ${payment.display} ETH. Продавец уведомлён.`,
    )
    await expect(page.locator("#remaining-value")).toHaveText(`+${payment.display} ETH`)
  })
}

for (const payment of [
  { expected: "0.005000000000000000", received: "0.002000000000000000", remaining: "0.003", receivedDisplay: "0.002" },
  { expected: "1.000000000000000001", received: "1.000000000000000000", remaining: "0.000000000000000001", receivedDisplay: "1" },
  { expected: "1000000000000000000.000000000000000001", received: "1.000000000000000000", remaining: "999999999999999999.000000000000000001", receivedDisplay: "1" },
]) {
  test(`partial ETH payment preserves the exact remainder ${payment.remaining}`, async ({ page }) => {
    await page.route("**/api/v1/checkout/invoices/**", (route) => route.fulfill({
      json: { ...ethInvoice, status: "partially_paid", expectedAmount: payment.expected, receivedAmount: payment.received },
    }))

    await page.goto(`/pay/i/${publicRef}`)
    await expect(page.locator("#partial-info")).toBeVisible()
    await expect(page.locator("#remaining-value")).toHaveText(`${payment.remaining} ETH`)
    await expect(page.locator("#received-value")).toHaveText(`${payment.receivedDisplay} ETH`)
    await expect(page.locator("#remaining-label")).toHaveText("Осталось оплатить")
    await expect(page.locator("#amount-label")).toHaveText("Сумма счёта")
    await expect(page.locator("#address-section")).toBeVisible()
    await expect(page.locator("#payment-state-notice")).toBeHidden()
  })
}

for (const payment of [
  { symbol: "PYUSD", decimals: 6, expected: "2.000000", received: "1.000000", expectedDisplay: "2", receivedDisplay: "1", deficit: "1" },
  { symbol: "ETH", decimals: 18, expected: "1.000000000000000001", received: "1.000000000000000000", expectedDisplay: "1.000000000000000001", receivedDisplay: "1", deficit: "0.000000000000000001" },
]) {
  for (const delivery of ["initial load", "polling"]) {
    test(`closed underpayment of ${payment.deficit} ${payment.symbol} is explained on ${delivery}`, async ({ page }) => {
      const closedInvoice = {
        ...invoice,
        status: "underpaid",
        acceptsPayments: false,
        walletAddress: "",
        expectedAmount: payment.expected,
        receivedAmount: payment.received,
        tokenSymbol: payment.symbol,
        decimalPlaces: payment.decimals,
      }
      await page.route("**/api/v1/checkout/invoices/**", route => {
        const isPoll = new URL(route.request().url()).pathname.endsWith("/status")
        return route.fulfill({ json: delivery === "initial load" || isPoll
          ? closedInvoice
          : { ...closedInvoice, status: "created", acceptsPayments: true, receivedAmount: "0", walletAddress: invoice.walletAddress },
        })
      })

      await page.goto(`/pay/i/${publicRef}`)
      if (delivery === "polling") {
        await expect(page.locator("#status-badge")).toHaveText("WAITING")
        await page.clock.fastForward(5_000)
      }

      await expect(page.locator("#remaining-value")).toHaveText(`${payment.deficit} ${payment.symbol}`)
      await expect(page.locator("#status-badge")).toHaveText("Недоплата")
      await expect(page.locator("#payment-state-title")).toHaveText("Счёт закрыт с недоплатой")
      await expect(page.locator("#amount-label")).toHaveText("Сумма счёта")
      await expect(page.locator("#amount-value")).toHaveText(payment.expectedDisplay)
      await expect(page.locator("#received-label")).toHaveText("Получено")
      await expect(page.locator("#received-value")).toHaveText(`${payment.receivedDisplay} ${payment.symbol}`)
      await expect(page.locator("#remaining-label")).toHaveText("Недоплата")
      await expect(page.locator("#payment-state-description")).toHaveText("Доплата по этому счёту недоступна. Не отправляйте средства на ранее показанный адрес.")
      await expect(page.locator("#payment-state-next-step")).toHaveText("Свяжитесь с продавцом по поводу оплаты заказа.")
      for (const section of ["qr-section", "address-section", "timer-section", "overlay-confirmed", "overlay-overpaid"]) {
        await expect(page.locator(`#${section}`)).toBeHidden()
      }
      await expect(page.getByText("Оплачено полностью", { exact: true })).toHaveCount(0)
    })
  }
}

test("a single partial payment waits for confirmation without offering a top-up", async ({ page }) => {
  await page.route("**/api/v1/checkout/invoices/**", route => route.fulfill({ json: {
    ...invoice, status: "partially_paid", acceptsPayments: false,
    expectedAmount: "2.000000", receivedAmount: "1.000000", tokenSymbol: "PYUSD",
  } }))

  await page.goto(`/pay/i/${publicRef}`)
  await expect(page.locator("#payment-state-title")).toHaveText("Ожидаем подтверждения перевода")
  await expect(page.locator("#remaining-label")).toHaveText("Недоплата")
  await expect(page.locator("#remaining-value")).toHaveText("1 PYUSD")
  await expect(page.locator("#payment-state-description")).toHaveText("Доплата сейчас недоступна. Не отправляйте средства на ранее показанный адрес.")
  await expect(page.locator("#payment-state-next-step")).toBeHidden()
  await expect(page.locator("#address-section")).toBeHidden()
  await expect(page.locator("#qr-section")).toBeHidden()
  await expect(page.locator("#timer-section")).toBeHidden()
  await expect(page.locator("#overlay-confirmed")).toBeHidden()
})

test("payment availability changes update the amount label and next step during polling", async ({ page }) => {
  let acceptsPayments = true
  await page.route("**/api/v1/checkout/invoices/**", route => {
    const isPoll = new URL(route.request().url()).pathname.endsWith("/status")
    if (isPoll) acceptsPayments = !acceptsPayments
    return route.fulfill({ json: {
      ...invoice, status: "partially_paid", acceptsPayments,
      expectedAmount: "2.000000", receivedAmount: "1.000000", tokenSymbol: "PYUSD",
    } })
  })

  await page.goto(`/pay/i/${publicRef}`)
  await expect(page.locator("#remaining-label")).toHaveText("Осталось оплатить")
  await expect(page.locator("#address-section")).toBeVisible()
  await expect(page.locator("#payment-state-notice")).toBeHidden()

  await page.clock.fastForward(5_000)
  await expect(page.locator("#remaining-label")).toHaveText("Недоплата")
  await expect(page.locator("#address-section")).toBeHidden()
  await expect(page.locator("#payment-state-title")).toHaveText("Ожидаем подтверждения перевода")

  await page.clock.fastForward(5_000)
  await expect(page.locator("#remaining-label")).toHaveText("Осталось оплатить")
  await expect(page.locator("#address-section")).toBeVisible()
  await expect(page.locator("#payment-state-notice")).toBeHidden()
  await expect(page.locator("#payment-state-title")).toBeHidden()
  await expect(page.locator("#remaining-value")).toHaveText("1 PYUSD")
})

test("an enumerated checkout reference receives the same not-found surface", async ({ page }) => {
  const guessedRef = "ci_ffffffffffffffffffffffffffffffff"

  await page.route("**/api/v1/checkout/invoices/**", async (route) => {
    await route.fulfill({
      status: 404,
      contentType: "application/json",
      body: JSON.stringify({ code: "NOT_FOUND", message: "resource not found" }),
    })
  })

  await page.goto(`/pay/i/${guessedRef}`)
  await expect(page.getByText("Payment not found", { exact: true })).toBeVisible()
  await expect(page.getByText(guessedRef, { exact: true })).toBeVisible()
  await expect(page.locator("#payment-card")).toBeHidden()
  await expect(page.locator("#wallet-address")).not.toHaveText(invoice.walletAddress)
})
