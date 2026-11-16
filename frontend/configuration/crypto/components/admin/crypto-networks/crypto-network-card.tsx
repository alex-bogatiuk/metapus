"use client"

import { useEffect, useMemo, useState } from "react"
import {
  AlertTriangle,
  ChevronRight,
  LoaderCircle,
  Save,
} from "lucide-react"
import { toast } from "sonner"

import { api, ApiError } from "@/lib/api"
import { cn } from "@/lib/utils"
import type {
  CryptoNetworkState,
  FeeFundingMode,
  ResourceStrategy,
} from "@/configuration/crypto/types/crypto-network"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"

import {
  type AmountField,
  type EditablePolicy,
  type NumericField,
  strategyDescriptions,
  strategyLabels,
  AmountFieldInput,
  FieldHelp,
  NumberField,
  formatTimestamp,
  getNativeTokenSymbol,
  networkHeaderDescription,
  networkReady,
  policiesEqual,
  toEditable,
  toPatch,
  validatePolicy,
} from "./types-and-helpers"
import { ProviderAccountsOrderSection } from "./provider-accounts-order-section"
import { RuntimeStatus } from "./runtime-status"
import { ShadowControlSection } from "./shadow-control-section"
import { NetworkWatchingSection } from "./network-watching-section"
import { IngestionPill, IngestionStreamsSection } from "./ingestion-status"

export interface CryptoNetworkCardProps {
  incoming: CryptoNetworkState
  expanded: boolean
  onExpandedChange: (expanded: boolean) => void
  onUpdated: () => Promise<import("@/configuration/crypto/types/crypto-network").CryptoNetworkListResponse | undefined>
  onDirtyChange: (networkId: string, dirty: boolean) => void
}

export function CryptoNetworkCard({
  incoming,
  expanded,
  onExpandedChange,
  onUpdated,
  onDirtyChange,
}: CryptoNetworkCardProps) {
  const [baseline, setBaseline] = useState(incoming)
  const [form, setForm] = useState<EditablePolicy>(() => toEditable(incoming.policy))
  const [saving, setSaving] = useState(false)
  const [rpcDirty, setRPCDirty] = useState(false)
  const hasExternalResources = incoming.capabilities.externalResourceMode !== "none"
  const isDirty = !policiesEqual(form, toEditable(baseline.policy))
  const errors = useMemo(() => validatePolicy(form, hasExternalResources), [form, hasExternalResources])
  const stale = incoming.policy.version !== baseline.policy.version
  const ready = networkReady(incoming, form)
  const nativeSymbol = incoming.network.nativeTokenSymbol || getNativeTokenSymbol(incoming.network.family)
  const nativeDecimals = incoming.network.nativeDecimals


  useEffect(() => {
    if (!isDirty && incoming.policy.version !== baseline.policy.version) {
      setBaseline(incoming)
      setForm(toEditable(incoming.policy))
    }
  }, [baseline.policy.version, incoming, isDirty])

  useEffect(() => {
    onDirtyChange(incoming.network.id, isDirty || rpcDirty)
    return () => onDirtyChange(incoming.network.id, false)
  }, [incoming.network.id, isDirty, rpcDirty, onDirtyChange])

  const updateNumber = (field: NumericField, raw: string) => {
    const value = Number(raw)
    setForm((current) => ({ ...current, [field]: Number.isFinite(value) ? Math.trunc(value) : 0 }))
  }
  const updateAmount = (field: AmountField, raw: string) => {
    setForm((current) => ({ ...current, [field]: raw.replace(/\D/g, "") }))
  }

  const save = async () => {
    if (Object.keys(errors).length > 0) {
      toast.error("Исправьте ошибки в настройках")
      return
    }
    setSaving(true)
    try {
      const updated = await api.cryptoNetworks.updatePolicy(
        incoming.network.id,
        toPatch(form, hasExternalResources)
      )
      setBaseline(updated)
      setForm(toEditable(updated.policy))
      await onUpdated()
      toast.success(`Настройки сети ${incoming.network.code} сохранены`)
    } catch (saveError) {
      setForm(toEditable(baseline.policy))
      try {
        await onUpdated()
      } catch {
        // The next SWR refresh reconciles after the primary save error.
      }
      if (saveError instanceof ApiError && saveError.status === 409) {
        toast.error("Настройки изменены другим администратором. Обновите данные.")
      } else if (saveError instanceof ApiError && saveError.parsedBody?.message === "operations processing requires a watched network") {
        toast.error("Сначала возобновите наблюдение за сетью.")
      } else {
        toast.error("Не удалось сохранить настройки. Проверьте данные и повторите попытку.")
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <Collapsible open={expanded} onOpenChange={onExpandedChange}>
      <Card>
        <CardHeader
          className="cursor-pointer"
          onClick={(event) => {
            if (event.target instanceof Element && event.target.closest("button, input, a, [role='switch']")) return
            onExpandedChange(!expanded)
          }}
        >
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="flex min-w-0 flex-1 items-start gap-2">
              <CollapsibleTrigger asChild>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="size-8 shrink-0"
                  aria-label={
                    expanded
                      ? `Свернуть сеть ${incoming.network.code}`
                      : `Развернуть сеть ${incoming.network.code}`
                  }
                  title={expanded ? "Свернуть" : "Развернуть"}
                >
                  <ChevronRight className={cn("transition-transform duration-200", expanded && "rotate-90")} />
                </Button>
              </CollapsibleTrigger>
              <div className="flex min-w-0 flex-col gap-1">
                <div className="flex flex-wrap items-center gap-2">
                  <CardTitle>{incoming.network.code}</CardTitle>
                  <Badge variant="outline">{incoming.network.family}</Badge>
                  {incoming.watching ? null : <Badge variant="secondary">Сеть не читается</Badge>}
                  <IngestionPill state={incoming} />
                  <Badge variant={form.enabled ? "success" : "secondary"}>
                    {form.enabled ? "Обработка включена" : "Обработка выключена"}
                  </Badge>
                  <Badge variant={ready ? "success" : "destructive"}>
                    {ready ? "Сеть готова" : "Сеть не готова"}
                  </Badge>
                  {incoming.runtime.operationalHold ? <Badge variant="destructive">Operational hold</Badge> : null}
                  {stale ? <Badge variant="warning">Настройки изменены в другом сеансе</Badge> : null}
                  {isDirty || rpcDirty ? <Badge variant="warning">Не сохранено</Badge> : null}
                </div>
                <CardDescription>
                  {networkHeaderDescription(
                    incoming,
                    form,
                    hasExternalResources,
                    baseline.policy.version,
                    expanded
                  )}
                </CardDescription>
              </div>
            </div>
            {expanded ? (
              <Field orientation="horizontal">
                <FieldContent>
                  <div className="flex items-center gap-1.5">
                    <FieldLabel htmlFor={`network-enabled-${incoming.network.id}`}>Обработка операций</FieldLabel>
                    <FieldHelp title="Обработка операций">
                      <p>
                        Разрешает воркеру выполнять новые транзакции в блокчейне (свипы, выплаты, JIT-пополнения).
                      </p>
                      <p className="mt-1">
                        Чтение сети не останавливает: входящие переводы и подтверждения отправленных транзакций обрабатываются и при выключенной обработке. Чтобы воркер не обращался к сети совсем, остановите наблюдение.
                      </p>
                      <p className="mt-1 font-medium text-destructive">
                        Включение блокируется, если за сетью не наблюдают, Signer недоступен, RPC-узлы не готовы или настройки содержат ошибки.
                      </p>
                    </FieldHelp>
                  </div>
                </FieldContent>
                <Switch
                  id={`network-enabled-${incoming.network.id}`}
                  checked={form.enabled}
                  disabled={!incoming.watching && !form.enabled}
                  onCheckedChange={(enabled) => setForm((current) => ({ ...current, enabled }))}
                />
              </Field>
            ) : null}
          </div>
        </CardHeader>

        <CollapsibleContent forceMount={rpcDirty ? true : undefined} className="data-[state=closed]:hidden">
          <CardContent className="flex flex-col gap-6">
            {incoming.runtime.operationalHold ? (
              <Alert variant="destructive">
                <AlertTriangle />
                <AlertTitle>Сеть остановлена защитным контуром</AlertTitle>
                <AlertDescription>{incoming.runtime.operationalHoldReason || "Требуется ручное расследование."}</AlertDescription>
              </Alert>
            ) : null}
            {!ready && form.enabled ? (
              <Alert variant="destructive">
                <AlertTriangle />
                <AlertTitle>Сеть нельзя включить</AlertTitle>
                <AlertDescription>
                  Проверьте signer, обязательные RPC-роли, fee wallet, лимиты и family-specific зависимости.
                </AlertDescription>
              </Alert>
            ) : null}

            <NetworkWatchingSection state={incoming} onUpdated={onUpdated} />
            <Separator />

            <IngestionStreamsSection state={incoming} />
            <Separator />

            <RuntimeStatus state={incoming} resourceStrategy={form.resourceStrategy} onRPCDirtyChange={setRPCDirty} />
            <Separator />

            {incoming.network.family === "tron" ? <>
              <ShadowControlSection networkId={incoming.network.id} networkCode={incoming.network.code} />
              <Separator />
            </> : null}

            <FieldSet>
              <FieldLegend>Комиссии и финансирование</FieldLegend>
              <FieldDescription>
                Суммы указаны в минимальных единицах нативного токена: 10⁻{incoming.network.nativeDecimals} ({nativeSymbol}).
                Replacement увеличивает один существующий резерв, а не создаёт новый расход.
              </FieldDescription>
              <FieldGroup className="grid gap-4 md:grid-cols-2">
                <Field data-invalid={Boolean(errors.feeFundingMode)}>
                  <div className="flex items-center justify-between gap-1.5">
                    <FieldLabel>Режим оплаты комиссии</FieldLabel>
                    <FieldHelp title="Режим оплаты комиссии">
                      <p className="mb-1.5">
                        Определяет источник нативного токена для оплаты газа:
                      </p>
                      <ul className="list-disc pl-4 space-y-1">
                        <li>
                          <strong className="text-foreground">JIT с fee wallet</strong>:
                          обязателен для токенов контрактов (USDT TRC-20, ERC-20). Перед свипом система автоматически переводит на депозитный адрес точный дефицит газа с центрального кошелька комиссии.
                        </li>
                        <li>
                          <strong className="text-foreground">Только исходный кошелёк</strong>:
                          комиссия списывается напрямую с адреса отправителя (для нативных TRX или ETH).
                        </li>
                      </ul>
                    </FieldHelp>
                  </div>
                  <Select
                    value={form.feeFundingMode}
                    onValueChange={(feeFundingMode: FeeFundingMode) =>
                      setForm((current) => ({ ...current, feeFundingMode }))
                    }
                  >
                    <SelectTrigger aria-invalid={Boolean(errors.feeFundingMode)}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value="source_only">Только исходный кошелёк</SelectItem>
                        <SelectItem value="jit">Автоматическое пополнение комиссии</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldError>{errors.feeFundingMode}</FieldError>
                  {form.feeFundingMode === "jit" ? (
                    <FieldDescription>
                      Источник пополнения определяется автоматически по владельцу кошелька.
                    </FieldDescription>
                  ) : null}
                </Field>

                <AmountFieldInput
                  field="perTransactionMaxFeeMinor"
                  label="Лимит комиссии на транзакцию"
                  value={form.perTransactionMaxFeeMinor}
                  error={errors.perTransactionMaxFeeMinor}
                  nativeDecimals={nativeDecimals}
                  nativeTokenSymbol={nativeSymbol}
                  help={{
                    title: "Лимит комиссии на транзакцию",
                    content: (
                      <>
                        <p>
                          Абсолютный потолок нативного токена на одну операцию (включая ускорение газа).
                        </p>
                        <p>
                          {incoming.network.family === "tron"
                            ? "Для TRON: стандартный перевод TRC-20 сжигает до 14–28 TRX (без аренды энергии). Рекомендуется: 30 000 000 (30 TRX)."
                            : incoming.network.family === "evm"
                            ? "Для Ethereum: рекомендуется 3 000 000 000 000 000 (0.003 ETH)."
                            : `Вводится в неделимых единицах (10⁻${nativeDecimals} ${nativeSymbol}).`}
                        </p>
                        <p className="font-medium text-amber-600 dark:text-amber-400">
                          Если котировка сети превысит этот лимит, операция будет отложена.
                        </p>
                      </>
                    ),
                  }}
                  onChange={updateAmount}
                />
                <AmountFieldInput
                  field="dailyNetworkFeeBudgetMinor"
                  label="Дневной лимит network fee"
                  value={form.dailyNetworkFeeBudgetMinor}
                  error={errors.dailyNetworkFeeBudgetMinor}
                  nativeDecimals={nativeDecimals}
                  nativeTokenSymbol={nativeSymbol}
                  help={{
                    title: "Дневной лимит network fee",
                    content: (
                      <>
                        <p>
                          Общий суточный бюджет сетевых комиссий за одни сутки (сброс в 00:00 UTC). Защищает баланс от опустошения при всплесках тарифов в блокчейне или программных циклах.
                        </p>
                        <p>
                          {incoming.network.family === "tron"
                            ? "Для TRON: рекомендуется от 300 000 000 (300 TRX тест) до 3 000 000 000 (3 000 TRX прод)."
                            : incoming.network.family === "evm"
                            ? "Для Ethereum: рекомендуется от 50 000 000 000 000 000 (0.05 ETH)."
                            : `Суточный лимит в неделимых единицах ${nativeSymbol}.`}
                        </p>
                        <p className="font-medium text-amber-600 dark:text-amber-400">
                          При исчерпании лимита новые операции в сети приостанавливаются.
                        </p>
                      </>
                    ),
                  }}
                  onChange={updateAmount}
                />
                {form.feeFundingMode === "jit" ? (
                  <>
                    <AmountFieldInput
                      field="dailyJitFundingBudgetMinor"
                      label="Дневной лимит JIT"
                      value={form.dailyJitFundingBudgetMinor}
                      error={errors.dailyJitFundingBudgetMinor}
                      nativeDecimals={nativeDecimals}
                      nativeTokenSymbol={nativeSymbol}
                      help={{
                        title: "Дневной лимит JIT",
                        content: (
                          <>
                            <p>
                              Суммарный объем нативного газа, который Fee Wallet может разослать на временные депозитные адреса за сутки (UTC).
                            </p>
                            <p>
                              <strong>Формула:</strong> должен быть строго больше одного максимума JIT funding.
                            </p>
                            <p>
                              {incoming.network.family === "tron"
                                ? "Для TRON: рекомендуется от 100 000 000 (100 TRX тест) до 1 000 000 000 (1 000 TRX прод)."
                                : incoming.network.family === "evm"
                                ? "Для Ethereum: рекомендуется от 40 000 000 000 000 000 (0.04 ETH)."
                                : ""}
                            </p>
                          </>
                        ),
                      }}
                      onChange={updateAmount}
                    />
                    <AmountFieldInput
                      field="maxFundingAmountMinor"
                      label="Максимум одного JIT funding"
                      value={form.maxFundingAmountMinor}
                      error={errors.maxFundingAmountMinor}
                      nativeDecimals={nativeDecimals}
                      nativeTokenSymbol={nativeSymbol}
                      help={{
                        title: "Максимум одного JIT funding",
                        content: (
                          <>
                            <p>
                              Потолок разового перевода на временный депозитный адрес. Переводится точный дефицит газа, но не более этого значения.
                            </p>
                            <p>
                              <strong>Формула:</strong> Лимит JIT ≥ Лимит комиссии + Остаток source wallet.
                            </p>
                            <p>
                              {incoming.network.family === "tron"
                                ? "Для TRON: 32 000 000 (32 TRX) при комиссии 30 TRX и буфере 1 TRX."
                                : incoming.network.family === "evm"
                                ? "Для Ethereum: 4 000 000 000 000 000 (0.004 ETH)."
                                : ""}
                            </p>
                          </>
                        ),
                      }}
                      onChange={updateAmount}
                    />
                    <AmountFieldInput
                      field="minFeeWalletBalanceMinor"
                      label="Остаток fee wallet"
                      value={form.minFeeWalletBalanceMinor}
                      error={errors.minFeeWalletBalanceMinor}
                      nativeDecimals={nativeDecimals}
                      nativeTokenSymbol={nativeSymbol}
                      help={{
                        title: "Остаток fee wallet",
                        content: (
                          <>
                            <p>
                              Неснижаемый баланс на кошельке сетевой комиссии после всех активных резервов.
                            </p>
                            <p>
                              {incoming.network.family === "tron"
                                ? "Для TRON: 50 000 000 (50 TRX тест) / 500 000 000 (500 TRX прод)."
                                : incoming.network.family === "evm"
                                ? "Для Ethereum: 5 000 000 000 000 000 (0.005 ETH)."
                                : ""}
                            </p>
                            <p className="font-medium text-amber-600 dark:text-amber-400">
                              Если остаток кошелька упадет ниже этого значения, JIT-пополнения заблокируются до внесения средств.
                            </p>
                          </>
                        ),
                      }}
                      onChange={updateAmount}
                    />
                  </>
                ) : null}
                <AmountFieldInput
                  field="minSourceWalletResidualMinor"
                  label="Остаток source wallet"
                  value={form.minSourceWalletResidualMinor}
                  error={errors.minSourceWalletResidualMinor}
                  nativeDecimals={nativeDecimals}
                  nativeTokenSymbol={nativeSymbol}
                  help={{
                    title: "Остаток source wallet",
                    content: (
                      <>
                        <p>
                          Сумма, гарантированно остающаяся на исходном адресе после свипа нативного токена.
                        </p>
                        <p>
                          {incoming.network.family === "tron"
                            ? "Для TRON: рекомендуется 1 000 000 (1 TRX) для предотвращения деактивации аккаунта в блокчейне."
                            : incoming.network.family === "evm"
                            ? "Для Ethereum: рекомендуется 100 000 000 000 000 (0.0001 ETH)."
                            : "Буфер против обнуления адреса."}
                        </p>
                      </>
                    ),
                  }}
                  onChange={updateAmount}
                />
              </FieldGroup>
            </FieldSet>

            {hasExternalResources ? (
              <>
                <Separator />
                <FieldSet>
                  <FieldLegend>Провайдеры ресурсов</FieldLegend>
                  <FieldDescription>Упорядоченный список вендоров аренды. Новый заказ идёт к первому здоровому; уже открытый заказ остаётся у своего провайдера.</FieldDescription>
                  <FieldGroup className="grid gap-4 md:grid-cols-2">
                    <Field data-invalid={Boolean(errors.resourceStrategy)}>
                      <div className="flex items-center justify-between gap-1.5">
                        <FieldLabel>Стратегия ресурсов</FieldLabel>
                        <FieldHelp title="Стратегия ресурсов (TRON Energy)">
                          <p className="mb-1.5">
                            Аренда энергии снижает затраты на переводы TRC-20 на 65–80%:
                          </p>
                          <ul className="list-disc pl-4 space-y-1">
                            <li>
                              <strong className="text-foreground">Аренда, иначе нативная комиссия</strong>{" "}
                              <em>(рекомендуется)</em>: заказ энергии у внешнего провайдера; при сбое — безопасный fallback на сжигание нативного TRX.
                            </li>
                            <li>
                              <strong className="text-foreground">Только аренда ресурсов</strong>:
                              при сбое провайдера операция блокируется до восстановления.
                            </li>
                            <li>
                              <strong className="text-foreground">Только нативная комиссия</strong>:
                              всегда сжигает TRX (не требует интеграции с сервисами аренды).
                            </li>
                          </ul>
                        </FieldHelp>
                      </div>
                      <Select
                        value={form.resourceStrategy}
                        onValueChange={(resourceStrategy: ResourceStrategy) =>
                          setForm((current) => ({ ...current, resourceStrategy }))
                        }
                      >
                        <SelectTrigger aria-invalid={Boolean(errors.resourceStrategy)}>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectGroup>
                            {(Object.keys(strategyLabels) as ResourceStrategy[]).map((strategy) => (
                              <SelectItem key={strategy} value={strategy}>{strategyLabels[strategy]}</SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FieldDescription>{strategyDescriptions[form.resourceStrategy]}</FieldDescription>
                      <FieldError>{errors.resourceStrategy}</FieldError>
                    </Field>
                    <div className="md:col-span-2">
                      <ProviderAccountsOrderSection
                        networkId={incoming.network.id}
                        accountIds={form.resourceProviderAccountIds}
                        onChange={(resourceProviderAccountIds) =>
                          setForm((current) => ({ ...current, resourceProviderAccountIds }))
                        }
                        error={errors.resourceProviderAccountIds}
                      />
                    </div>
                    <NumberField
                      field="resourceProviderFailureThreshold"
                      label="Порог сбоев провайдера"
                      value={form.resourceProviderFailureThreshold}
                      error={errors.resourceProviderFailureThreshold}
                      help={{
                        title: "Порог сбоев провайдера",
                        content: "Число подряд идущих ошибок провайдера аренды (таймауты, 5xx, 429), после которого он отключается.",
                      }}
                      onChange={updateNumber}
                    />
                    <NumberField
                      field="resourceProviderOpenSeconds"
                      label="Пауза провайдера после сбоя, с"
                      value={form.resourceProviderOpenSeconds}
                      error={errors.resourceProviderOpenSeconds}
                      unit="сек"
                      help={{
                        title: "Пауза провайдера после сбоя",
                        content: "Время нахождения провайдера аренды в карантине перед отправкой пробного заказа (обычно 60 сек).",
                      }}
                      onChange={updateNumber}
                    />
                  </FieldGroup>
                </FieldSet>
              </>
            ) : null}

            <Separator />
            <FieldSet>
              <FieldLegend>Retry и RPC circuit breaker</FieldLegend>
              <FieldGroup className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                <NumberField
                  field="retryDelaySeconds"
                  label="Пауза повторной попытки, с"
                  value={form.retryDelaySeconds}
                  error={errors.retryDelaySeconds}
                  unit="сек"
                  help={{
                    title: "Пауза повторной попытки",
                    content: "Интервал между повторными попытками при временных сбоях в сети (обычно 15–30 сек).",
                  }}
                  onChange={updateNumber}
                />
                <NumberField
                  field="rpcFailureThreshold"
                  label="Порог сбоев RPC"
                  value={form.rpcFailureThreshold}
                  error={errors.rpcFailureThreshold}
                  help={{
                    title: "Порог сбоев RPC",
                    content: "Число последовательных сбоев до временного исключения RPC-узла. Переключение на доступный резерв происходит уже при ошибке запроса.",
                  }}
                  onChange={updateNumber}
                />
                <NumberField
                  field="rpcOpenSeconds"
                  label="Пауза RPC после сбоя, с"
                  value={form.rpcOpenSeconds}
                  error={errors.rpcOpenSeconds}
                  unit="сек"
                  help={{
                    title: "Пауза RPC после сбоя",
                    content: "Время нахождения сбойного узла в карантине перед отправкой пробного запроса (обычно 30–60 сек).",
                  }}
                  onChange={updateNumber}
                />
              </FieldGroup>
            </FieldSet>
          </CardContent>

          <CardFooter className="justify-between gap-3">
            <p className="text-xs text-muted-foreground">
              Runtime: {formatTimestamp(incoming.runtime.observedAt)} · policy: {formatTimestamp(baseline.policy.updatedAt)}
            </p>
            <Button onClick={() => void save()} disabled={!isDirty || saving || Object.keys(errors).length > 0 || stale}>
              {saving ? <LoaderCircle data-icon="inline-start" className="animate-spin" /> : <Save data-icon="inline-start" />}
              Сохранить настройки
            </Button>
          </CardFooter>
        </CollapsibleContent>
      </Card>
    </Collapsible>
  )
}
