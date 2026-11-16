import type {
  PortalInvoiceDetailResponse,
  PortalInvoicePayment,
} from "@/configuration/crypto/types/portal-api";
import { formatMinorUnits } from "@/lib/format";

export const INVOICE_LABELS: Record<string, string> = {
  created: "Ожидает оплаты",
  partially_paid: "Частичная оплата",
  paid: "Ожидает подтверждения сети",
  confirmed: "Ожидает зачисления",
  completed: "Оплачен",
  underpaid: "Недоплата",
  reversed: "Подтверждение утрачено",
  expired: "Истёк",
  cancelled: "Отменён",
};
export function rawAmount(value: string | null | undefined): bigint | null {
  return value != null && /^\d+$/.test(value) ? BigInt(value) : null;
}
export function invoiceMoney(
  value: string | null | undefined,
  decimals: number,
  symbol: string,
): string {
  if (
    rawAmount(value) == null ||
    !Number.isInteger(decimals) ||
    decimals < 0 ||
    decimals > 78
  )
    return "—";
  return `${formatMinorUnits(value!, decimals).replace(".", ",")} ${symbol}`;
}
export function invoiceOutcome(invoice: PortalInvoiceDetailResponse) {
  const amount = rawAmount(invoice.amount),
    received = rawAmount(invoice.receivedAmount);
  const ledger = invoice.financialSummary.ledger;
  const known =
    ledger.status === "known" &&
    [
      ledger.creditedNetAmount,
      ledger.processingFeeAmount,
      ledger.reversedNetAmount,
    ].every((value) => rawAmount(value) != null);
  const valid =
    amount != null &&
    amount > 0n &&
    received != null &&
    Number.isInteger(invoice.decimalPlaces) &&
    invoice.decimalPlaces >= 0 &&
    invoice.decimalPlaces <= 78;
  const difference = valid ? amount! - received! : null;
  const overpaid = difference != null && difference < 0n;
  const topUp = invoice.invoiceKind === "top_up";
  const deadline = Date.parse(invoice.expiresAt),
    observed = Date.parse(invoice.observedAt);
  const overdue =
    !topUp &&
    invoice.acceptsPayments &&
    Number.isFinite(deadline) &&
    deadline <= observed;
  const canPay =
    invoice.acceptsPayments &&
    !overdue &&
    !topUp &&
    valid &&
    known &&
    !!INVOICE_LABELS[invoice.status];
  const label =
    !known || !valid
      ? "Требует сверки"
      : (INVOICE_LABELS[invoice.status] ?? "Требует проверки");
  const progress = valid
    ? Number(
        (received! * 10000n) / amount! > 10000n
          ? 10000n
          : (received! * 10000n) / amount!,
      ) / 100
    : 0;
  const money = (value: string | null | undefined) =>
    invoiceMoney(value, invoice.decimalPlaces, invoice.symbol);
  let title = label,
    description =
      "Состояние инвойса не распознано. Финансовые суммы и технические данные доступны ниже.";
  if (!known || !valid)
    description =
      "Не удалось установить полный финансовый результат. Проверьте историю платежей и обновите данные. Неизвестные суммы отмечены знаком «—».";
  else if (overdue) {
    title = "Срок оплаты прошёл";
    description =
      "Статус инвойса ещё не обновлён. Обновите данные, чтобы проверить результат оплаты.";
  } else if (topUp)
    description =
      "Инвойс пополнения сформирован автоматически по поступившему переводу. Результат подтверждения и зачисления показан отдельно.";
  else {
    const closure =
      invoice.paymentAcceptanceReason === "single_payment_selected"
        ? "Инвойс принимает один перевод; перевод уже выбран. Приём новых переводов закрыт."
        : invoice.paymentAcceptanceReason === "expired"
          ? "Срок оплаты завершён. Приём новых переводов закрыт."
          : "Приём новых переводов закрыт.";
    switch (invoice.status) {
      case "created":
        description = canPay
          ? "Передайте клиенту ссылку: на странице оплаты указаны сумма, сеть и реквизиты."
          : closure;
        break;
      case "partially_paid":
        description = canPay
          ? `Не хватает ${money(difference?.toString())}. Инвойс продолжает принимать оплату.`
          : `Получена часть суммы. Ожидаем подтверждения перевода. ${closure}`;
        break;
      case "paid":
        description =
          "Полная сумма обнаружена. Ожидаем подтверждения сети; зачисление на баланс проверяется по проводкам.";
        break;
      case "confirmed":
        description =
          "Оплата подтверждена сетью. Ожидаем завершения зачисления на баланс.";
        break;
      case "completed":
        description = overpaid
          ? `Получено на ${money((-difference!).toString())} больше суммы инвойса. Переплата не означает выполненный возврат.`
          : "Оплата завершена. Зачисление и комиссия показаны по фактическим проводкам.";
        break;
      case "underpaid":
        description = `Не хватает ${money(difference != null && difference > 0n ? difference.toString() : "0")}. ${closure}`;
        break;
      case "expired":
        description =
          "Срок оплаты завершён. Если переводы поступали, их финансовый результат показан ниже.";
        break;
      case "cancelled":
        description =
          "Инвойс отменён. Полученные средства и их зачисление остаются видимыми в итогах и платежах.";
        break;
      case "reversed":
        description =
          "Подтверждение платежа утрачено. Финансовый результат показан с учётом записанных сторно.";
        break;
    }
  }
  return {
    label,
    title,
    description,
    known,
    valid,
    difference,
    overpaid,
    topUp,
    overdue,
    canPay,
    progress,
    money,
    reversed: (rawAmount(ledger.reversedNetAmount) ?? 0n) > 0n,
    destructive:
      !known || !valid || ["reversed", "cancelled"].includes(invoice.status),
  };
}
export function paymentOutcome(payment: PortalInvoicePayment) {
  const ledger = payment.ledger;
  if (ledger.status !== "known") return "Требует сверки";
  const credited = rawAmount(ledger.creditedNetAmount),
    reversed = rawAmount(ledger.reversedNetAmount);
  if (credited == null || reversed == null) return "Требует сверки";
  if (
    payment.canonicality !== "canonical" ||
    ["reorged", "replaced", "failed"].includes(payment.status)
  )
    return "Исключён из оплаты";
  if (payment.posted) return credited > 0n ? "Зачислен" : "Учтён";
  if (reversed > 0n && credited === 0n) return "Ожидает повторного зачисления";
  if (["confirmed", "settled"].includes(payment.status))
    return "Ожидает зачисления";
  return "Ожидает подтверждения";
}
export function webhookOutcome(
  statusCode: number | null,
  error: string | null,
): string {
  if (statusCode != null && statusCode >= 200 && statusCode < 300)
    return "Доставлен";
  if (statusCode != null || error) return "Ошибка доставки";
  return "Результат неизвестен";
}
export const EVENT_LABELS: Record<string, string> = {
  invoice_created: "Инвойс создан",
  detected: "Перевод обнаружен",
  payment_detected: "Перевод обнаружен",
  first_confirmation: "Получено первое подтверждение",
  confirmed: "Платёж подтверждён сетью",
  merchant_ledger_credited: "Платёж зачислен на баланс",
  chain_reorg: "Подтверждение утрачено",
  posted_chain_reorg: "Подтверждение утрачено после зачисления",
  reorged: "Перевод исключён из цепочки",
  replaced: "Перевод заменён",
  failed: "Обработка платежа завершилась ошибкой",
};
