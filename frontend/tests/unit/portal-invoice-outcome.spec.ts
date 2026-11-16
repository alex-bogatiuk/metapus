import { test, expect } from "@playwright/test";
import {
  invoiceOutcome,
  invoiceMoney,
  paymentOutcome,
  webhookOutcome,
} from "../../configuration/crypto/lib/portal-invoice-outcome";
import { PortalInvoiceDetailResponseSchema } from "../../configuration/crypto/lib/generated/http-contract-schemas";
import { invoiceFixture, knownLedger } from "../invoice-detail-fixture";

test("underpaid invoice separates balance credit, shortage and closed intake", () => {
  const model = invoiceOutcome(invoiceFixture());
  expect(model.label).toBe("Недоплата");
  expect(model.difference).toBe(1000000n);
  expect(model.progress).toBe(50);
  expect(model.canPay).toBe(false);
  expect(model.description).toContain("один перевод");
  expect(model.description).not.toContain("Срок оплаты завершён");
});
test("exact progress, overpayment and scale remain safe above Number precision", () => {
  const amount = "9".repeat(78);
  const model = invoiceOutcome(
    invoiceFixture({
      amount,
      receivedAmount: (BigInt(amount) + 1n).toString(),
    }),
  );
  expect(model.difference).toBe(-1n);
  expect(model.overpaid).toBe(true);
  expect(model.progress).toBe(100);
  expect(invoiceMoney("1", 18, "ETH")).toBe("0,000000000000000001 ETH");
  expect(invoiceMoney("9007199254740993", 0, "TOKEN")).toBe(
    "9007199254740993 TOKEN",
  );
  expect(invoiceMoney(null, 6, "USDT")).toBe("—");
  expect(invoiceOutcome(invoiceFixture({ amount: "0" })).label).toBe(
    "Требует сверки",
  );
});
test("network finality, booked money and reversals remain distinct", () => {
  const invoice = invoiceFixture({
    status: "confirmed",
    financialSummary: {
      confirmedAmount: "1000000",
      pendingNetAmount: "900000",
      ledger: knownLedger("0"),
    },
  });
  expect(invoiceOutcome(invoice).label).toBe("Ожидает зачисления");
  const reversed = invoiceOutcome(
    invoiceFixture({
      status: "reversed",
      financialSummary: {
        confirmedAmount: "0",
        pendingNetAmount: "0",
        ledger: knownLedger("0", "0", "1000000"),
      },
    }),
  );
  expect(reversed.known).toBe(true);
  expect(reversed.reversed).toBe(true);
  const inconsistent = invoiceOutcome(
    invoiceFixture({
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
    }),
  );
  expect(inconsistent.known).toBe(false);
  expect(inconsistent.label).toBe("Требует сверки");
});
for (const [status, label] of Object.entries({
  created: "Ожидает оплаты",
  partially_paid: "Частичная оплата",
  paid: "Ожидает подтверждения сети",
  confirmed: "Ожидает зачисления",
  completed: "Оплачен",
  underpaid: "Недоплата",
  reversed: "Подтверждение утрачено",
  expired: "Истёк",
  cancelled: "Отменён",
  future: "Требует проверки",
})) {
  test(`status ${status} retains distinct merchant wording`, () =>
    expect(invoiceOutcome(invoiceFixture({ status })).label).toBe(label));
}
test("open partial invoices permit payment, closed selections and overdue snapshots do not", () => {
  const open = invoiceFixture({
    status: "partially_paid",
    acceptsPayments: true,
    paymentAcceptanceReason: "open",
  });
  expect(invoiceOutcome(open).canPay).toBe(true);
  expect(
    invoiceOutcome({ ...open, observedAt: "2026-10-02T10:01:00Z" }).overdue,
  ).toBe(true);
  expect(
    invoiceOutcome({ ...open, observedAt: "2026-10-02T10:01:00Z" }).canPay,
  ).toBe(false);
  expect(
    invoiceOutcome({
      ...open,
      acceptsPayments: false,
      paymentAcceptanceReason: "single_payment_selected",
    }).description,
  ).toContain("закрыт");
  expect(invoiceOutcome({ ...open, invoiceKind: "top_up" }).canPay).toBe(false);
});
test("webhook diagnostics preserve no response, zero duration and successful retries", () => {
  expect(webhookOutcome(204, null)).toBe("Доставлен");
  expect(webhookOutcome(500, null)).toBe("Ошибка доставки");
  expect(webhookOutcome(null, "timeout")).toBe("Ошибка доставки");
  expect(webhookOutcome(null, null)).toBe("Результат неизвестен");
});
test("detail contract rejects numeric money and preserves null financial facts", () => {
  expect(
    PortalInvoiceDetailResponseSchema.safeParse(invoiceFixture()).success,
  ).toBe(true);
  const fixture = invoiceFixture();
  const bad = {
    ...fixture,
    financialSummary: {
      ...fixture.financialSummary,
      ledger: { ...fixture.financialSummary.ledger, creditedNetAmount: 42 },
    },
  };
  expect(PortalInvoiceDetailResponseSchema.safeParse(bad).success).toBe(false);
  const unknown = invoiceFixture({
    financialSummary: {
      confirmedAmount: "0",
      pendingNetAmount: "0",
      ledger: {
        status: "inconsistent",
        creditedNetAmount: null,
        processingFeeAmount: null,
        reversedNetAmount: null,
      },
    },
  });
  expect(
    PortalInvoiceDetailResponseSchema.parse(unknown).financialSummary.ledger
      .creditedNetAmount,
  ).toBeNull();
});
test("canonicality does not fabricate a reversal of financial credit", () => {
  const payment = invoiceFixture().payments[0];
  expect(paymentOutcome(payment)).toBe("Зачислен");
  expect(
    paymentOutcome({ ...payment, canonicality: "orphaned", posted: false }),
  ).toBe("Исключён из оплаты");
  expect(
    paymentOutcome({
      ...payment,
      status: "confirmed",
      posted: false,
      ledger: knownLedger("0"),
    }),
  ).toBe("Ожидает зачисления");
});
