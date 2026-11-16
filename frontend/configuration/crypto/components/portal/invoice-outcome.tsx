"use client";
import { useState, type ReactNode } from "react";
import Link from "next/link";
import {
  AlertCircle,
  ArrowLeft,
  CheckCircle2,
  ChevronDown,
  Clock3,
  Copy,
  ExternalLink,
  RefreshCw,
  Undo2,
  Wallet,
  Webhook,
} from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty";
import { Progress } from "@/components/ui/progress";
import { Separator } from "@/components/ui/separator";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { buildTransactionExplorerUrl } from "@/configuration/crypto/lib/blockchain";
import { formatDateTime } from "@/lib/format";
import {
  EVENT_LABELS,
  invoiceOutcome,
  paymentOutcome,
  rawAmount,
  webhookOutcome,
} from "@/configuration/crypto/lib/portal-invoice-outcome";
import { cn } from "@/lib/utils";
import type {
  PortalInvoiceDetailResponse,
  PortalInvoicePayment,
} from "@/configuration/crypto/types/portal-api";

type Invoice = PortalInvoiceDetailResponse;
type Outcome = ReturnType<typeof invoiceOutcome>;
const date = (value?: string) =>
  value && Number.isFinite(Date.parse(value)) ? formatDateTime(value) : "—";
function timezone(value: string) {
  const timestamp = new Date(value);
  return Number.isFinite(timestamp.getTime())
    ? new Intl.DateTimeFormat("ru-RU", { timeZoneName: "shortOffset" })
        .formatToParts(timestamp)
        .find((part) => part.type === "timeZoneName")
        ?.value.replace("GMT", "UTC")
    : "";
}
function Detail({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)] gap-3 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words [overflow-wrap:anywhere]">
        {children}
      </dd>
    </div>
  );
}
function CopyButton({
  text,
  label,
  primary = false,
}: {
  text: string;
  label: string;
  primary?: boolean;
}) {
  const [pending, setPending] = useState(false);
  async function copy() {
    setPending(true);
    try {
      if (!navigator.clipboard) throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(text);
      toast.success("Скопировано");
    } catch {
      toast.error(
        "Не удалось скопировать. Выделите текст и скопируйте вручную.",
      );
    } finally {
      setPending(false);
    }
  }
  return (
    <Button
      variant={primary ? "default" : "outline"}
      size="sm"
      onClick={() => void copy()}
      disabled={pending || !text}
    >
      <Copy data-icon="inline-start" />
      {label}
    </Button>
  );
}
function Disclosure({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <Collapsible className="group flex min-w-0 flex-col gap-3">
      <CollapsibleTrigger asChild>
        <Button variant="ghost" size="sm" className="justify-start self-start">
          <ChevronDown
            data-icon="inline-start"
            className="group-data-[state=open]:rotate-180"
          />
          {title}
        </Button>
      </CollapsibleTrigger>
      <CollapsibleContent className="min-w-0">{children}</CollapsibleContent>
    </Collapsible>
  );
}
function NoData({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <Empty>
      <EmptyHeader>
        <EmptyTitle>{title}</EmptyTitle>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}
function InvoiceSummary({
  invoice,
  outcome,
}: {
  invoice: Invoice;
  outcome: Outcome;
}) {
  const ledger = invoice.financialSummary.ledger,
    difference = outcome.difference;
  const pendingNet = rawAmount(invoice.financialSummary.pendingNetAmount);
  const allReceiptsCredited =
    pendingNet === 0n &&
    outcome.known &&
    (rawAmount(invoice.receivedAmount) ?? 0n) > 0n &&
    !invoice.payments.some(
      (payment) =>
        payment.canonicality === "canonical" &&
        ["detected", "confirming", "confirmed", "settled"].includes(
          payment.status,
        ) &&
        !payment.posted,
    );
  return (
    <Card data-testid="invoice-summary" className="min-w-0">
      <CardHeader>
        <CardTitle>Итог оплаты</CardTitle>
        <CardDescription>
          {outcome.topUp
            ? "Пополнение баланса по поступившему переводу"
            : "Получение оплаты и зачисление на баланс"}
        </CardDescription>
      </CardHeader>
      <CardContent className="grid min-w-0 gap-6 md:grid-cols-2 md:gap-8">
        <div className="flex min-w-0 flex-col gap-4">
          <p className="text-sm text-muted-foreground">Получено по инвойсу</p>
          <div
            data-testid="invoice-received"
            className="min-w-0 break-words text-3xl font-semibold tracking-tight tabular-nums [overflow-wrap:anywhere] sm:text-4xl"
          >
            {outcome.money(invoice.receivedAmount)}
            <span className="mt-2 block text-base font-normal text-muted-foreground">
              из {outcome.money(invoice.amount)}
            </span>
          </div>
          {outcome.valid && (
            <Progress
              value={outcome.progress}
              className="h-2"
              aria-label="Полученная сумма относительно суммы инвойса"
              aria-valuenow={outcome.progress}
            />
          )}
          <p data-testid="invoice-difference" className="text-sm tabular-nums">
            {difference == null
              ? "Сумма инвойса требует проверки"
              : difference > 0n
                ? `Не хватает ${outcome.money(difference.toString())}`
                : difference < 0n
                  ? `Переплата ${outcome.money((-difference).toString())}`
                  : "Полная сумма получена"}
          </p>
        </div>
        <div className="flex min-w-0 flex-col gap-4 rounded-lg bg-muted/40 p-5">
          <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <Wallet className="size-4" aria-hidden="true" />
            Зачислено на баланс
            {outcome.reversed && <span>· с учётом сторно</span>}
          </div>
          <p
            data-testid="invoice-credited"
            className="min-w-0 break-words text-3xl font-semibold tracking-tight tabular-nums [overflow-wrap:anywhere] sm:text-4xl"
          >
            {outcome.known ? outcome.money(ledger.creditedNetAmount) : "—"}
          </p>
          <dl className="flex min-w-0 flex-col gap-2 tabular-nums">
            <Detail label="Подтверждено сетью">
              {outcome.money(invoice.financialSummary.confirmedAmount)}
            </Detail>
            <Detail label="Учтённая комиссия">
              {outcome.known ? outcome.money(ledger.processingFeeAmount) : "—"}
            </Detail>
            <Detail label="Ещё не зачислено">
              {allReceiptsCredited
                ? "Все поступления зачислены"
                : outcome.money(invoice.financialSummary.pendingNetAmount)}
            </Detail>
            {outcome.reversed && (
              <Detail label="Сторнировано за всё время">
                {outcome.money(ledger.reversedNetAmount)}
              </Detail>
            )}
          </dl>
          {(pendingNet ?? 0n) > 0n && (
            <p className="text-xs text-muted-foreground">
              Сумма по обнаруженным платежам, которые ещё не зачислены на
              баланс, за вычетом их комиссии.
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
function Payment({
  payment,
  invoice,
  outcome,
}: {
  payment: PortalInvoicePayment;
  invoice: Invoice;
  outcome: Outcome;
}) {
  const label = paymentOutcome(payment),
    ledger = payment.ledger;
  const explorer = buildTransactionExplorerUrl(
    payment.txHash,
    invoice.explorerUrl,
  );
  const excluded =
    payment.canonicality !== "canonical" ||
    ["reorged", "replaced", "failed"].includes(payment.status);
  return (
    <article
      data-testid="invoice-payment"
      className="flex min-w-0 flex-col gap-3"
    >
      <div className="flex flex-wrap items-start justify-between gap-2">
        <p className="min-w-0 break-words text-lg font-medium tabular-nums [overflow-wrap:anywhere]">
          {outcome.money(payment.amount)}
        </p>
        <Badge
          variant={
            label === "Зачислен"
              ? "success"
              : label === "Требует сверки"
                ? "warning"
                : "secondary"
          }
        >
          {label === "Зачислен" ? (
            <CheckCircle2 className="mr-1 size-3" aria-hidden="true" />
          ) : (
            <Clock3 className="mr-1 size-3" aria-hidden="true" />
          )}
          {label}
        </Badge>
      </div>
      <p className="text-xs text-muted-foreground">
        {invoice.network} · Обнаружен {date(payment.createdAt)}
      </p>
      <dl className="flex min-w-0 flex-col gap-2 tabular-nums">
        <Detail label="Зачислено с учётом сторно">
          {ledger.status === "known"
            ? outcome.money(ledger.creditedNetAmount)
            : "—"}
        </Detail>
        <Detail label="Учтённая комиссия">
          {ledger.status === "known"
            ? outcome.money(ledger.processingFeeAmount)
            : "—"}
        </Detail>
        {!payment.posted && !excluded && (
          <Detail label="Расчётно к зачислению">
            {outcome.money(payment.netAmount)}
          </Detail>
        )}
        {(rawAmount(payment.refundedAmount) ?? 0n) > 0n && (
          <Detail label="Возвращено плательщику">
            {outcome.money(payment.refundedAmount)}
          </Detail>
        )}
        {(rawAmount(payment.refundPendingAmount) ?? 0n) > 0n && (
          <Detail label="Возврат в обработке">
            {outcome.money(payment.refundPendingAmount)}
          </Detail>
        )}
      </dl>
      {(rawAmount(payment.refundableAmount) ?? 0n) > 0n && (
        <Button variant="outline" size="sm" className="self-start" asChild>
          <Link href={`/portal/withdrawals/new?refund=${encodeURIComponent(payment.id)}`}>
            <Undo2 data-icon="inline-start" />
            Вернуть плательщику
          </Link>
        </Button>
      )}
      {excluded && (
        <p className="text-xs text-muted-foreground">
          Перевод не входит в полученную и подтверждённую сумму. Финансовые
          проводки учтены отдельно.
        </p>
      )}
      <Disclosure title="Детали транзакции">
        <dl className="flex min-w-0 flex-col gap-3">
          <Detail label="Хеш транзакции">
            <code className="select-all font-mono text-xs">
              {payment.txHash || "—"}
            </code>
          </Detail>
          <Detail label="Отправитель">
            <code className="select-all font-mono text-xs">
              {payment.fromAddress || "Не указан"}
            </code>
          </Detail>
          <Detail label="Подтверждения сети">
            {payment.confirmations} ·{" "}
            {payment.requiredConfs > 0
              ? `нужно ${payment.requiredConfs}`
              : "требование не задано"}
          </Detail>
          <Detail label="Подтверждён">{date(payment.confirmedAt)}</Detail>
          {(rawAmount(ledger.reversedNetAmount) ?? 0n) > 0n && (
            <Detail label="Сторнировано">
              {outcome.money(ledger.reversedNetAmount)}
            </Detail>
          )}
          <Detail label="Техническое состояние">
            <code className="font-mono text-xs">
              {payment.status} · {payment.canonicality}
            </code>
          </Detail>
        </dl>
        <div className="mt-4 flex flex-wrap gap-2">
          <CopyButton text={payment.txHash} label="Копировать хеш" />
          {explorer && (
            <Button variant="outline" size="sm" asChild>
              <a href={explorer} target="_blank" rel="noopener noreferrer">
                <ExternalLink data-icon="inline-start" />
                Транзакция в сети
              </a>
            </Button>
          )}
        </div>
      </Disclosure>
    </article>
  );
}
function InvoicePayments({
  invoice,
  outcome,
}: {
  invoice: Invoice;
  outcome: Outcome;
}) {
  const [visible, setVisible] = useState(5);
  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle>
          Платежи{" "}
          <span className="text-muted-foreground">
            · {invoice.payments.length}
          </span>
        </CardTitle>
        <CardDescription>
          Все обнаруженные переводы и их финансовый результат
        </CardDescription>
      </CardHeader>
      <CardContent className="flex min-w-0 flex-col gap-5">
        {invoice.payments.length === 0 ? (
          <NoData
            title="Платежей пока нет"
            description="Обнаруженные переводы появятся здесь после обновления данных."
          />
        ) : (
          invoice.payments.slice(0, visible).map((payment, index) => (
            <div key={payment.id} className="flex min-w-0 flex-col gap-5">
              {index > 0 && <Separator />}
              <Payment payment={payment} invoice={invoice} outcome={outcome} />
            </div>
          ))
        )}
        {visible < invoice.payments.length && (
          <Button
            variant="outline"
            onClick={() => setVisible((count) => count + 5)}
          >
            Показать ещё · {invoice.payments.length - visible}
          </Button>
        )}
      </CardContent>
    </Card>
  );
}
function InvoiceHistory({ invoice }: { invoice: Invoice }) {
  const [expanded, setExpanded] = useState(false);
  const events = [
    {
      id: `created-${invoice.id}`,
      paymentId: "",
      eventType: "invoice_created",
      createdAt: invoice.createdAt,
      fromStatus: "",
      toStatus: "created",
      metadata: {},
    },
    ...invoice.timeline,
  ].sort(
    (a, b) =>
      Date.parse(a.createdAt) - Date.parse(b.createdAt) ||
      a.id.localeCompare(b.id),
  );
  const visible = expanded ? events : events.slice(-10);
  return (
    <div className="flex min-w-0 flex-col gap-4">
      {!expanded && events.length > 10 && (
        <Button variant="outline" size="sm" onClick={() => setExpanded(true)}>
          Показать всю историю · {events.length}
        </Button>
      )}
      <ol className="flex min-w-0 flex-col gap-5">
        {visible.map((event) => (
          <li key={event.id} className="flex min-w-0 gap-3">
            <div className="flex size-7 shrink-0 items-center justify-center rounded-full bg-muted">
              <Clock3
                className="size-3.5 text-muted-foreground"
                aria-hidden="true"
              />
            </div>
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <p className="text-sm font-medium">
                {EVENT_LABELS[event.eventType] ??
                  "Состояние платежа изменилось"}
              </p>
              <time
                className="text-xs text-muted-foreground"
                dateTime={event.createdAt}
              >
                {date(event.createdAt)}
              </time>
              {event.paymentId && (
                <p className="truncate text-xs text-muted-foreground">
                  Платёж {event.paymentId}
                </p>
              )}
              <Disclosure title="Технические данные события">
                <dl className="flex min-w-0 flex-col gap-2">
                  <Detail label="Событие">
                    <code className="font-mono text-xs">{event.eventType}</code>
                  </Detail>
                  <Detail label="Переход">
                    <code className="font-mono text-xs">
                      {event.fromStatus || "—"} → {event.toStatus || "—"}
                    </code>
                  </Detail>
                  {event.metadata.confirmations != null && (
                    <Detail label="Подтверждения">
                      {event.metadata.confirmations}
                      {event.metadata.requiredConfs != null &&
                        ` · нужно ${event.metadata.requiredConfs}`}
                    </Detail>
                  )}
                  {event.metadata.txHash && (
                    <Detail label="Хеш">
                      <code className="select-all font-mono text-xs">
                        {event.metadata.txHash}
                      </code>
                    </Detail>
                  )}
                </dl>
              </Disclosure>
            </div>
          </li>
        ))}
      </ol>
    </div>
  );
}
function InvoiceWebhooks({ invoice }: { invoice: Invoice }) {
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <p className="text-sm text-muted-foreground">
        Доставка уведомлений о событиях инвойса. Результат доставки не меняет
        финансовые суммы.
      </p>
      {invoice.webhookDeliveryTotal > 50 && (
        <p className="text-sm">
          Последние 50 из {invoice.webhookDeliveryTotal} попыток
        </p>
      )}
      {invoice.webhookDeliveries.length === 0 ? (
        <NoData
          title="Попыток доставки пока нет"
          description="Отправленные уведомления появятся здесь после обновления данных."
        />
      ) : (
        invoice.webhookDeliveries.map((delivery) => {
          const label = webhookOutcome(
            delivery.statusCode,
            delivery.errorMessage,
          );
          return (
            <div
              key={delivery.id}
              className="flex min-w-0 flex-col gap-2 rounded-lg border p-4"
            >
              <div className="flex flex-wrap items-start justify-between gap-2">
                <code className="min-w-0 break-all text-xs">
                  {delivery.eventType}
                </code>
                <Badge
                  variant={
                    label === "Доставлен"
                      ? "success"
                      : label === "Ошибка доставки"
                        ? "warning"
                        : "outline"
                  }
                >
                  {label}
                </Badge>
              </div>
              <p className="text-xs text-muted-foreground tabular-nums">
                {date(delivery.createdAt)} · Попытка #{delivery.attempt}
              </p>
              <dl className="flex flex-col gap-2 tabular-nums">
                <Detail label="HTTP-код">
                  {delivery.statusCode ?? "Нет ответа"}
                </Detail>
                <Detail label="Длительность">
                  {delivery.responseTimeMs == null
                    ? "—"
                    : `${delivery.responseTimeMs} мс`}
                </Detail>
              </dl>
              {delivery.errorMessage && (
                <Disclosure title="Причина ошибки">
                  <p className="break-words text-sm [overflow-wrap:anywhere]">
                    {delivery.errorMessage}
                  </p>
                </Disclosure>
              )}
            </div>
          );
        })
      )}
      <Button variant="outline" size="sm" className="self-start" asChild>
        <Link href="/portal/developers/webhooks">
          <Webhook data-icon="inline-start" />
          Все вебхуки
        </Link>
      </Button>
    </div>
  );
}
function InvoiceDiagnostics({ invoice }: { invoice: Invoice }) {
  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle>История и уведомления</CardTitle>
        <CardDescription>
          Хронология платежей и диагностика доставки
        </CardDescription>
      </CardHeader>
      <CardContent className="min-w-0">
        <Tabs defaultValue="history">
          <TabsList variant="line" aria-label="Диагностика инвойса">
            <TabsTrigger variant="line" value="history">
              История
            </TabsTrigger>
            {invoice.canViewWebhooks && (
              <TabsTrigger variant="line" value="webhooks">
                Вебхуки · {invoice.webhookDeliveryTotal}
              </TabsTrigger>
            )}
          </TabsList>
          <TabsContent value="history" className="pt-4">
            <InvoiceHistory invoice={invoice} />
          </TabsContent>
          {invoice.canViewWebhooks && (
            <TabsContent value="webhooks" className="pt-4">
              <InvoiceWebhooks invoice={invoice} />
            </TabsContent>
          )}
        </Tabs>
      </CardContent>
    </Card>
  );
}
function InvoiceReferences({
  invoice,
  outcome,
}: {
  invoice: Invoice;
  outcome: Outcome;
}) {
  return (
    <Card className="order-3 min-w-0 lg:order-1">
      <CardHeader>
        <CardTitle>Реквизиты инвойса</CardTitle>
      </CardHeader>
      <CardContent>
        <dl className="flex min-w-0 flex-col gap-3">
          <Detail label="Номер заказа">{invoice.orderId || "Не указан"}</Detail>
          <Detail label="Внешний ID">
            {invoice.externalId || "Не указан"}
          </Detail>
          <Detail label="Customer ID">
            {invoice.customerId || "Не указан"}
          </Detail>
          <Detail label="Токен и сеть">
            {invoice.symbol}
            <br />
            <span className="text-muted-foreground">{invoice.network}</span>
          </Detail>
          <Detail label="Создан">{date(invoice.createdAt)}</Detail>
          <Detail label="Срок оплаты">
            {outcome.topUp ? "Не применяется" : date(invoice.expiresAt)}
          </Detail>
          <Detail label="Приём переводов">
            <Badge variant="outline">
              {outcome.topUp
                ? "Системное пополнение"
                : invoice.acceptsPayments
                  ? "Открыт"
                  : "Закрыт"}
            </Badge>
          </Detail>
          {invoice.description && (
            <Detail label="Описание">{invoice.description}</Detail>
          )}
        </dl>
      </CardContent>
    </Card>
  );
}
function InvoiceClientLink({
  invoice,
  outcome,
}: {
  invoice: Invoice;
  outcome: Outcome;
}) {
  const safeURL = (() => {
    try {
      const url = new URL(invoice.payUrl);
      return url.protocol === "https:" && !url.username && !url.password
        ? url.href
        : null;
    } catch {
      return null;
    }
  })();
  return (
    <Card className="order-1 min-w-0 lg:order-2">
      <CardHeader>
        <CardTitle>
          {outcome.canPay ? "Оплата клиентом" : "Страница для клиента"}
        </CardTitle>
        <CardDescription>
          {outcome.canPay
            ? "Сумма, сеть и реквизиты оплаты по одной ссылке."
            : outcome.topUp
              ? "Страница с результатом пополнения баланса."
              : outcome.overdue
                ? "Срок оплаты прошёл. Проверьте обновлённый статус перед отправкой ссылки."
                : "Клиент увидит состояние и результат инвойса."}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex min-w-0 flex-col gap-4">
        {safeURL ? (
          <div className="flex flex-wrap gap-2">
            {outcome.canPay && (
              <CopyButton
                text={invoice.payUrl}
                label="Копировать ссылку"
                primary
              />
            )}
            <Button size="sm" variant="outline" asChild>
              <a href={safeURL} target="_blank" rel="noopener noreferrer">
                <ExternalLink data-icon="inline-start" />
                {outcome.canPay
                  ? "Открыть страницу оплаты"
                  : "Открыть страницу инвойса"}
              </a>
            </Button>
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">
            Ссылка для клиента недоступна.
          </p>
        )}
        {safeURL && (
          <Disclosure title="Ссылка для клиента">
            <div className="flex min-w-0 flex-col gap-3">
              <code className="select-all break-all rounded-md bg-muted p-3 text-xs">
                {invoice.payUrl}
              </code>
              {!outcome.canPay && (
                <CopyButton text={invoice.payUrl} label="Копировать ссылку" />
              )}
            </div>
          </Disclosure>
        )}
      </CardContent>
    </Card>
  );
}
export function InvoiceOutcomePage({
  invoice,
  refreshing,
  refresh,
  refreshError,
}: {
  invoice: Invoice;
  refreshing: boolean;
  refresh: () => void;
  refreshError: boolean;
}) {
  const outcome = invoiceOutcome(invoice);
  return (
    <div className="mx-auto flex w-full min-w-0 max-w-7xl flex-col gap-5 p-4 sm:p-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex min-w-0 flex-col gap-3">
          <Link
            className="flex items-center gap-2 self-start text-sm text-muted-foreground hover:text-foreground"
            href="/portal/invoices"
          >
            <ArrowLeft className="size-4" aria-hidden="true" />
            Инвойсы
          </Link>
          <div className="flex min-w-0 flex-wrap items-center gap-3">
            <h1 className="min-w-0 break-words text-2xl font-semibold tracking-tight [overflow-wrap:anywhere]">
              Инвойс {invoice.number}
            </h1>
            <Badge
              variant={
                outcome.destructive
                  ? "warning"
                  : invoice.status === "completed"
                    ? "success"
                    : "secondary"
              }
            >
              {outcome.label}
            </Badge>
            {outcome.overpaid && <Badge variant="warning">Переплата</Badge>}
            {outcome.topUp && (
              <Badge variant="outline">Пополнение баланса</Badge>
            )}
          </div>
          <p className="text-sm text-muted-foreground">
            {invoice.symbol} · {invoice.network}
          </p>
        </div>
        <div className="flex flex-col items-start gap-2 sm:items-end">
          <Button
            variant="outline"
            size="sm"
            disabled={refreshing}
            onClick={refresh}
          >
            <RefreshCw
              data-icon="inline-start"
              className={cn(refreshing && "animate-spin")}
            />
            {refreshing ? "Обновляем…" : "Обновить"}
          </Button>
          <p
            data-testid="invoice-observed"
            className="text-xs text-muted-foreground"
          >
            Данные на {date(invoice.observedAt)} ·{" "}
            {timezone(invoice.observedAt)}
          </p>
        </div>
      </div>
      {refreshError && (
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>Не удалось обновить данные</AlertTitle>
          <AlertDescription>
            Показаны данные на {date(invoice.observedAt)}. Нажмите «Обновить»,
            чтобы попробовать ещё раз.
          </AlertDescription>
        </Alert>
      )}
      <InvoiceSummary invoice={invoice} outcome={outcome} />
      <Alert variant={outcome.destructive ? "destructive" : "default"}>
        <AlertCircle />
        <AlertTitle>{outcome.title}</AlertTitle>
        <AlertDescription>{outcome.description}</AlertDescription>
      </Alert>
      <div className="grid min-w-0 grid-cols-1 items-start gap-5 lg:grid-cols-[minmax(0,2fr)_minmax(18rem,1fr)]">
        <div className="order-2 flex min-w-0 flex-col gap-5 lg:order-1">
          <InvoicePayments invoice={invoice} outcome={outcome} />
          <InvoiceDiagnostics invoice={invoice} />
        </div>
        <aside className="contents lg:order-2 lg:flex lg:min-w-0 lg:flex-col lg:gap-5">
          <InvoiceReferences invoice={invoice} outcome={outcome} />
          <InvoiceClientLink invoice={invoice} outcome={outcome} />
        </aside>
      </div>
    </div>
  );
}
