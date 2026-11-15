import { test, expect, type Page, type Route } from "@playwright/test";
import {
  commonAPIResponse,
  ids,
  installPortalSession,
  json,
  portalOwner,
} from "./helpers";
import {
  invoiceFixture,
  invoiceId,
  knownLedger,
} from "./invoice-detail-fixture";
import type { PortalInvoiceDetailResponse } from "../types/portal-api";
declare global {
  interface Window {
    copiedText: string;
  }
}
const secondMerchant = "10000000-0000-4000-8000-000000000021";
async function setup(
  page: Page,
  response: () => PortalInvoiceDetailResponse = () => invoiceFixture(),
  intercept?: (route: Route, url: URL) => Promise<boolean>,
  role = 1,
) {
  await page.clock.install({ time: new Date("2026-10-02T09:06:00Z") });
  await installPortalSession(page);
  await page.addInitScript(() =>
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (text: string) => {
          window.copiedText = text;
        },
      },
    }),
  );
  await page.route("**/api/v1/**", (route) =>
    json(
      route,
      commonAPIResponse(new URL(route.request().url()).pathname, {
        ...portalOwner,
        merchantIds: [ids.merchant, secondMerchant],
      }) ?? { items: [] },
    ),
  );
  await page.route("**/portal/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (await intercept?.(route, url)) return;
    if (url.pathname.endsWith("/merchants"))
      return json(route, {
        items: [
          {
            id: ids.merchant,
            name: "Acme",
            code: "ACME",
            role,
            networkAccess: "allow",
          },
          {
            id: secondMerchant,
            name: "Beta",
            code: "BETA",
            role,
            networkAccess: "allow",
          },
        ],
      });
    if (url.pathname.endsWith(`/invoices/${invoiceId}`))
      return json(route, response());
    return json(route, { items: [] });
  });
}
test("outcome shows real credit, closed intake and functional diagnostics", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 1100 });
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await setup(page);
  await page.goto(`/portal/invoices/${invoiceId}`);
  await expect(page.getByTestId("invoice-received")).toContainText(
    "1 PYUSDиз 2 PYUSD",
  );
  await expect(page.getByTestId("invoice-credited")).toHaveText("1 PYUSD");
  await expect(
    page.getByText("Ещё не зачислено", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Все поступления зачислены", { exact: true }),
  ).toBeVisible();
  await expect(page.getByTestId("invoice-difference")).toHaveText(
    "Не хватает 1 PYUSD",
  );
  await expect(
    page.getByText("Инвойс принимает один перевод", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Копировать ссылку", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: "Открыть страницу инвойса" }),
  ).toHaveAttribute("href", "https://example.test/pay/i/ci_test");
  await expect(page.getByRole("tab", { name: "История" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await page.getByRole("button", { name: "Детали транзакции" }).click();
  await expect(page.getByText("13 · нужно 12", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Копировать хеш" }).click();
  expect(await page.evaluate(() => window.copiedText)).toBe(
    invoiceFixture().payments[0].txHash,
  );
  await expect(
    page.getByRole("link", { name: "Транзакция в сети" }),
  ).toHaveAttribute("href", /sepolia.etherscan.io\/tx\/0x6/);
  // A credited payment with nothing refunded yet offers a refund of itself.
  await expect(
    page.getByRole("link", { name: "Вернуть плательщику" }),
  ).toHaveAttribute("href", `/portal/withdrawals/new?refund=${ids.wallet}`);
  await page.getByRole("tab", { name: /Вебхуки/ }).click();
  await expect(page.getByTestId("invoice-payment")).toBeVisible();
  await expect(page.getByText("0 мс", { exact: true })).toBeVisible();
  await expect(
    page.getByText("Ошибка доставки", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Причина ошибки" }).click();
  await expect(
    page.getByText("connection refused", { exact: true }),
  ).toBeVisible();
  await page.getByRole("tab", { name: "История" }).click();
  await page.screenshot({
    path: "test-results/invoice-outcome-desktop.png",
    fullPage: true,
  });
  expect(errors).toEqual([]);
});
test("only explicit refresh requests data and failures retain the snapshot", async ({
  page,
}) => {
  let calls = 0,
    fail = false;
  await setup(
    page,
    () => invoiceFixture(),
    async (route, url) => {
      if (!url.pathname.endsWith(`/invoices/${invoiceId}`)) return false;
      calls++;
      await json(
        route,
        fail
          ? { code: "INTERNAL_ERROR", message: "test" }
          : invoiceFixture({
              observedAt:
                calls > 1 ? "2026-10-02T09:07:00Z" : "2026-10-02T09:06:00Z",
            }),
        fail ? 500 : 200,
      );
      return true;
    },
  );
  await page.goto(`/portal/invoices/${invoiceId}`);
  await expect(page.getByTestId("invoice-summary")).toBeVisible();
  expect(calls).toBe(1);
  await page.getByRole("tab", { name: /Вебхуки/ }).click();
  await page.evaluate(() => {
    window.dispatchEvent(new Event("focus"));
    window.dispatchEvent(new Event("online"));
  });
  await page.clock.runFor(120000);
  expect(calls).toBe(1);
  const snapshot = await page.getByTestId("invoice-observed").innerText();
  fail = true;
  await page.getByRole("button", { name: "Обновить", exact: true }).click();
  await expect(
    page.getByText("Не удалось обновить данные", { exact: true }),
  ).toBeVisible();
  expect(calls).toBe(2);
  await expect(page.getByTestId("invoice-credited")).toHaveText("1 PYUSD");
  await expect(page.getByTestId("invoice-observed")).toHaveText(snapshot);
  await page.clock.runFor(60000);
  expect(calls).toBe(2);
  fail = false;
  await page.getByRole("button", { name: "Обновить", exact: true }).click();
  await expect(
    page.getByText("Не удалось обновить данные", { exact: true }),
  ).toHaveCount(0);
  await expect(page.getByTestId("invoice-observed")).not.toHaveText(snapshot);
  expect(calls).toBe(3);
});
test("open payment link copies successfully and reports denied clipboard access", async ({
  page,
}) => {
  await setup(page, () =>
    invoiceFixture({
      status: "partially_paid",
      acceptsPayments: true,
      paymentAcceptanceReason: "open",
    }),
  );
  await page.goto(`/portal/invoices/${invoiceId}`);
  await page
    .getByRole("button", { name: "Копировать ссылку", exact: true })
    .click();
  await expect(page.getByText("Скопировано", { exact: true })).toBeVisible();
  expect(await page.evaluate(() => window.copiedText)).toBe(
    invoiceFixture().payUrl,
  );
  await page.evaluate(() =>
    Object.defineProperty(navigator, "clipboard", {
      value: {
        writeText: async () => {
          throw new Error("denied");
        },
      },
    }),
  );
  await page
    .getByRole("button", { name: "Копировать ссылку", exact: true })
    .click();
  await expect(
    page.getByText(
      "Не удалось скопировать. Выделите текст и скопируйте вручную.",
    ),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Ссылка для клиента", exact: true })
    .click();
  await expect(
    page.getByText(invoiceFixture().payUrl, { exact: true }),
  ).toBeVisible();
});
test("initial failure can be retried without background retries", async ({
  page,
}) => {
  let calls = 0;
  await setup(
    page,
    () => invoiceFixture(),
    async (route, url) => {
      if (!url.pathname.endsWith(`/invoices/${invoiceId}`)) return false;
      await json(
        route,
        ++calls === 1 ? { message: "failed" } : invoiceFixture(),
        calls === 1 ? 500 : 200,
      );
      return true;
    },
  );
  await page.goto(`/portal/invoices/${invoiceId}`);
  await expect(page.getByText("Не удалось загрузить инвойс")).toBeVisible();
  await page.clock.runFor(10000);
  expect(calls).toBe(1);
  await page.getByRole("button", { name: "Повторить", exact: true }).click();
  await expect(page.getByTestId("invoice-summary")).toBeVisible();
  expect(calls).toBe(2);
});
test("viewer cannot see webhook diagnostics", async ({ page }) => {
  await setup(
    page,
    () =>
      invoiceFixture({
        canViewWebhooks: false,
        webhookDeliveries: [],
        webhookDeliveryTotal: 0,
      }),
    undefined,
    3,
  );
  await page.goto(`/portal/invoices/${invoiceId}`);
  await expect(page.getByTestId("invoice-summary")).toBeVisible();
  await expect(page.getByRole("tab", { name: /Вебхуки/ })).toHaveCount(0);
  await expect(
    page.getByText("invoice.underpaid", { exact: true }),
  ).toHaveCount(0);
});
test("merchant switch discards a late response and resets open disclosures", async ({
  page,
}) => {
  let release: (() => void) | undefined;
  const wait = new Promise<void>((resolve) => {
    release = resolve;
  });
  await setup(
    page,
    () => invoiceFixture(),
    async (route, url) => {
      if (!url.pathname.endsWith(`/invoices/${invoiceId}`)) return false;
      if (url.searchParams.get("merchant_id") === secondMerchant) {
        await wait;
        await json(
          route,
          invoiceFixture({ number: "CI-BETA", receivedAmount: "9000000" }),
        );
      } else await json(route, invoiceFixture());
      return true;
    },
  );
  await page.goto(`/portal/invoices/${invoiceId}`);
  await expect(page.getByTestId("invoice-summary")).toBeVisible();
  await page.getByRole("button", { name: "Детали транзакции" }).click();
  await page.getByRole("button", { name: /Acme/ }).click();
  await page.getByRole("menuitem", { name: /Beta/ }).click();
  await expect(page.getByTestId("invoice-summary")).toHaveCount(0);
  await page.getByRole("button", { name: /Beta/ }).click();
  await page.getByRole("menuitem", { name: /Acme/ }).click();
  await expect(page.getByTestId("invoice-summary")).toBeVisible();
  release?.();
  await page.clock.runFor(100);
  await expect(
    page.getByRole("heading", { name: "Инвойс CI-BETA" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Копировать хеш" }),
  ).toHaveCount(0);
});
for (const width of [320, 390, 1280])
  test(`layout at ${width}px handles long values and preserves mobile order`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1100 });
    const long = "9".repeat(78);
    await setup(page, () =>
      invoiceFixture({
        amount: long,
        receivedAmount: long,
        orderId: "order".repeat(30),
        financialSummary: {
          confirmedAmount: long,
          pendingNetAmount: "0",
          ledger: knownLedger(long),
        },
      }),
    );
    await page.goto(`/portal/invoices/${invoiceId}`);
    await expect(page.getByTestId("invoice-summary")).toBeVisible();
    await page.getByRole("button", { name: "Детали транзакции" }).click();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    if (width < 1024) {
      const link = await page
        .getByRole("heading", { name: "Страница для клиента" })
        .boundingBox();
      const payments = await page
        .getByRole("heading", { name: /^Платежи/ })
        .boundingBox();
      const refs = await page
        .getByRole("heading", { name: "Реквизиты инвойса" })
        .boundingBox();
      expect(link!.y).toBeLessThan(payments!.y);
      expect(payments!.y).toBeLessThan(refs!.y);
    }
    await page.screenshot({
      path: `test-results/invoice-outcome-${width}.png`,
      fullPage: true,
    });
    await page.evaluate(() => document.documentElement.classList.add("dark"));
    await page.screenshot({
      path: `test-results/invoice-outcome-${width}-dark.png`,
      fullPage: true,
    });
  });
test("large histories and payment lists expand without hiding the financial result", async ({
  page,
}) => {
  const invoice = invoiceFixture();
  invoice.payments = Array.from({ length: 11 }, (_, index) => ({
    ...invoice.payments[0],
    id: `payment-${index}`,
  }));
  invoice.timeline = Array.from({ length: 15 }, (_, index) => ({
    ...invoice.timeline[0],
    id: `event-${index}`,
    createdAt: new Date(
      Date.parse(invoice.createdAt) + (index + 1) * 1000,
    ).toISOString(),
  }));
  invoice.webhookDeliveryTotal = 55;
  await setup(page, () => invoice);
  await page.goto(`/portal/invoices/${invoiceId}`);
  await expect(page.getByTestId("invoice-payment")).toHaveCount(5);
  await page.getByRole("button", { name: /Показать ещё/ }).click();
  await expect(page.getByTestId("invoice-payment")).toHaveCount(10);
  await page.getByRole("button", { name: /Показать ещё/ }).click();
  await expect(page.getByTestId("invoice-payment")).toHaveCount(11);
  await page.getByRole("button", { name: /Показать всю историю/ }).click();
  await expect(page.getByText("Инвойс создан", { exact: true })).toBeVisible();
  await page.getByRole("tab", { name: /Вебхуки/ }).click();
  await expect(page.getByText("Последние 50 из 55 попыток")).toBeVisible();
});

test("confirmation, ledger uncertainty, reversals and top-ups stay distinct", async ({
  page,
}) => {
  let current = invoiceFixture();
  await setup(page, () => current);
  await page.goto(`/portal/invoices/${invoiceId}`);
  await expect(page.getByTestId("invoice-credited")).toHaveText("1 PYUSD");

  current = invoiceFixture({
    status: "confirmed",
    receivedAmount: "2000000",
    paymentAcceptanceReason: "awaiting_credit",
    financialSummary: {
      confirmedAmount: "2000000",
      pendingNetAmount: "1900000",
      ledger: knownLedger("0"),
    },
  });
  current.payments = [
    {
      ...current.payments[0],
      amount: "2000000",
      processingFee: "100000",
      netAmount: "1900000",
      posted: false,
      status: "confirmed",
      requiredConfs: 0,
      ledger: knownLedger("0"),
    },
  ];
  await page.getByRole("button", { name: "Обновить", exact: true }).click();
  await expect(page.getByTestId("invoice-credited")).toHaveText("0 PYUSD");
  await expect(
    page.getByTestId("invoice-summary").getByText("1,9 PYUSD", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Все поступления зачислены", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page
      .getByTestId("invoice-payment")
      .getByText("Ожидает зачисления", { exact: true }),
  ).toBeVisible();

  current = invoiceFixture({
    status: "reversed",
    receivedAmount: "0",
    paymentAcceptanceReason: "reversed",
    financialSummary: {
      confirmedAmount: "0",
      pendingNetAmount: "0",
      ledger: knownLedger("0", "0", "1000000"),
    },
  });
  current.payments = [
    {
      ...current.payments[0],
      status: "reorged",
      canonicality: "orphaned",
      posted: false,
      ledger: knownLedger("0", "0", "1000000"),
    },
  ];
  await page.getByRole("button", { name: "Обновить", exact: true }).click();
  await expect(page.getByTestId("invoice-credited")).toHaveText("0 PYUSD");
  await expect(
    page
      .getByTestId("invoice-payment")
      .getByText("Исключён из оплаты", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Сторнировано за всё время", { exact: true }),
  ).toBeVisible();

  current = invoiceFixture({
    financialSummary: {
      confirmedAmount: "1000000",
      pendingNetAmount: "0",
      ledger: {
        status: "inconsistent",
        creditedNetAmount: null,
        processingFeeAmount: null,
        reversedNetAmount: null,
      },
    },
  });
  await page.getByRole("button", { name: "Обновить", exact: true }).click();
  await expect(page.getByTestId("invoice-credited")).toHaveText("—");
  await expect(
    page.getByText("Все поступления зачислены", { exact: true }),
  ).toHaveCount(0);
  await expect(page.getByText("Требует сверки", { exact: true })).toHaveCount(
    2,
  );

  current = invoiceFixture({
    status: "completed",
    invoiceKind: "top_up",
    amount: "1000000",
    expiresAt: "0001-01-01T00:00:00Z",
    paymentAcceptanceReason: "completed",
  });
  await page.getByRole("button", { name: "Обновить", exact: true }).click();
  await expect(
    page.getByText("Пополнение баланса", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Не применяется", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Копировать ссылку", exact: true }),
  ).toHaveCount(0);
});

test("overpayment and overdue open intake do not offer misleading actions", async ({
  page,
}) => {
  let current = invoiceFixture({
    status: "completed",
    receivedAmount: "3000000",
    paymentAcceptanceReason: "completed",
    financialSummary: {
      confirmedAmount: "3000000",
      pendingNetAmount: "0",
      ledger: knownLedger("2900000", "100000"),
    },
  });
  await setup(page, () => current);
  await page.goto(`/portal/invoices/${invoiceId}`);
  await expect(page.getByTestId("invoice-difference")).toHaveText(
    "Переплата 1 PYUSD",
  );
  await expect(page.getByRole("progressbar")).toHaveAttribute(
    "aria-valuenow",
    "100",
  );
  await expect(page.getByTestId("invoice-credited")).toHaveText("2,9 PYUSD");

  current = invoiceFixture({
    status: "partially_paid",
    acceptsPayments: true,
    paymentAcceptanceReason: "open",
    expiresAt: "2026-10-02T09:00:00Z",
  });
  await page.getByRole("button", { name: "Обновить", exact: true }).click();
  await expect(
    page.getByText("Срок оплаты прошёл", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Статус инвойса ещё не обновлён.", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Копировать ссылку", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: "Открыть страницу инвойса" }),
  ).toBeVisible();

  current = invoiceFixture({
    status: "unrecognized_status",
    acceptsPayments: true,
    paymentAcceptanceReason: "closed",
  });
  await page.getByRole("button", { name: "Обновить", exact: true }).click();
  await expect(page.getByText("Требует проверки", { exact: true })).toHaveCount(
    2,
  );
  await expect(
    page.getByRole("button", { name: "Копировать ссылку", exact: true }),
  ).toHaveCount(0);
});

test("an in-flight refresh keeps the snapshot and prevents a second request", async ({
  page,
}) => {
  let calls = 0;
  let release: (() => void) | undefined;
  const wait = new Promise<void>((resolve) => {
    release = resolve;
  });
  await setup(
    page,
    () => invoiceFixture(),
    async (route, url) => {
      if (!url.pathname.endsWith(`/invoices/${invoiceId}`)) return false;
      if (++calls > 1) await wait;
      await json(route, invoiceFixture());
      return true;
    },
  );
  await page.goto(`/portal/invoices/${invoiceId}`);
  await expect(page.getByTestId("invoice-summary")).toBeVisible();
  await page.getByRole("button", { name: "Обновить", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Обновляем…", exact: true }),
  ).toBeDisabled();
  await expect(page.getByTestId("invoice-summary")).toBeVisible();
  expect(calls).toBe(2);
  release?.();
  await expect(
    page.getByRole("button", { name: "Обновить", exact: true }),
  ).toBeEnabled();
  expect(calls).toBe(2);
});

test("diagnostic tabs and transaction details work from the keyboard", async ({
  page,
}) => {
  await setup(page);
  await page.goto(`/portal/invoices/${invoiceId}`);
  const history = page.getByRole("tab", { name: "История", exact: true });
  await history.press("ArrowRight");
  await expect(page.getByRole("tab", { name: /Вебхуки/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await page.getByRole("tab", { name: /Вебхуки/ }).press("ArrowLeft");
  await expect(history).toHaveAttribute("aria-selected", "true");
  const details = page.getByRole("button", {
    name: "Детали транзакции",
    exact: true,
  });
  await details.press("Enter");
  await expect(details).toHaveAttribute("aria-expanded", "true");
  await page
    .getByRole("button", { name: "Копировать хеш", exact: true })
    .press("Enter");
  await expect(page.getByText("Скопировано", { exact: true })).toBeVisible();
});
