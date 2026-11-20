"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import {
  AlertTriangle,
  ChevronRight,
  LoaderCircle,
  RefreshCw,
  Save,
} from "lucide-react"
import { toast } from "sonner"

import { ApiError } from "@/lib/api"
import type {
  WalletPoolActivationPolicy,
  WalletPoolPolicy,
  WalletPoolPolicyPatch,
  WalletPoolState,
} from "@/configuration/crypto/types/wallet-pool"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge, type BadgeProps } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { FloatingSaveButton, FloatingSaveRail } from "@/components/shared/floating-save-rail"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
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
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useExpandedCards } from "@/hooks/useExpandedCards"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { cn } from "@/lib/utils"

import { cryptoApi } from "@/configuration/crypto/lib/api"
type EditablePolicy = Pick<
  WalletPoolPolicy,
  | "enabled"
  | "lowWatermark"
  | "targetReady"
  | "maxTotalWallets"
  | "maxProvisionBatch"
  | "maxActiveInvoiceLeasesPerMerchant"
  | "maxPersistentAddressesPerMerchant"
  | "maxLeaseStartsPerMinutePerMerchant"
  | "minGlobalReadyReserve"
  | "retryAfterSeconds"
  | "addressReuseCooldownSeconds"
  | "maxWatcherLagSeconds"
  | "version"
> & { activation: WalletPoolActivationPolicy }

const healthLabels: Record<WalletPoolState["state"], string> = {
  healthy: "Адресов достаточно",
  low: "Мало готовых адресов",
  depleted: "Свободных адресов нет",
  paused: "Автоматическое создание выключено",
}

const operationalErrorMessages: Record<string, string> = {
  VAULT_UNAVAILABLE:
    "Vault временно недоступен. Создание адресов продолжится автоматически.",
  VAULT_INVALID_RESPONSE:
    "Vault вернул некорректные данные. Проверьте журнал задач воркера.",
  CHAIN_UNAVAILABLE:
    "RPC сети временно недоступен. Проверка кошельков повторится автоматически.",
  NETWORK_DRIVER_UNAVAILABLE:
    "Runtime-адаптер сети не готов. Операция продолжится после восстановления конфигурации.",
  WALLET_PROVISIONING_UNSUPPORTED:
    "В текущей сборке нет адаптера автоматического создания адресов для этой сети.",
  WALLET_POOL_POLICY_INVALID:
    "Политика пула не соответствует возможностям установленного адаптера сети.",
  ACTIVATION_HELD:
    "Активация кошельков приостановлена. Проверьте лимиты и журнал задач воркера.",
}

function poolAlertTitle(state: WalletPoolState): string {
  if (state.capabilities.walletProvisioning === "unsupported") {
    return "Автоматическое создание не поддерживается"
  }
  if (state.policy.needsReview) return "Автоматическое создание недоступно"
  if (state.policy.leaseBlockedReason === "watcher_lag") return "Выдача адресов приостановлена"
  if (state.state === "depleted") return "Свободных адресов нет"
  return "Создание адресов приостановлено"
}

function poolAlertDescription(state: WalletPoolState): string {
  const { policy } = state
  if (state.capabilities.walletProvisioning === "unsupported") {
    return "Установите family adapter с поддержкой provisioning, прежде чем включать пул адресов."
  }
  if (policy.needsReview) {
    return "Подтвердите пространство путей деривации в конфигурации сети, затем включите автоматическое создание адресов."
  }
  const operationalMessage = policy.lastErrorCode
    ? operationalErrorMessages[policy.lastErrorCode] ??
      "Проверьте журнал задач воркера и повторите создание адресов."
    : ""
  if (policy.leaseBlockedReason === "watcher_lag") {
    return "Наблюдатель сети отстаёт. Новые адреса не выдаются, пока checkpoint не догонит цепь. Sweep и выводы продолжаются."
  }
  if (state.state === "depleted") {
    const unavailable = `Новые инвойсы временно не получат адрес. Повторный запрос возможен через ${policy.retryAfterSeconds} с.`
    return operationalMessage ? `${unavailable} ${operationalMessage}` : unavailable
  }
  return operationalMessage || "Проверьте состояние сети и повторите создание адресов."
}

const healthVariants: Record<
  WalletPoolState["state"],
  NonNullable<BadgeProps["variant"]>
> = {
  healthy: "success",
  low: "warning",
  depleted: "destructive",
  paused: "outline",
}

function toEditable(policy: WalletPoolPolicy): EditablePolicy {
  return {
    enabled: policy.enabled,
    lowWatermark: policy.lowWatermark,
    targetReady: policy.targetReady,
    maxTotalWallets: policy.maxTotalWallets,
    maxProvisionBatch: policy.maxProvisionBatch,
    maxActiveInvoiceLeasesPerMerchant:
      policy.maxActiveInvoiceLeasesPerMerchant,
    maxPersistentAddressesPerMerchant:
      policy.maxPersistentAddressesPerMerchant,
    maxLeaseStartsPerMinutePerMerchant:
      policy.maxLeaseStartsPerMinutePerMerchant,
    minGlobalReadyReserve: policy.minGlobalReadyReserve,
    activation: policy.activation ?? {
      maxConcurrent: 0,
      reserveMinor: "0",
      minHotBalanceMinor: "0",
      dailyBudgetMinor: "0",
    },
    retryAfterSeconds: policy.retryAfterSeconds,
    addressReuseCooldownSeconds: policy.addressReuseCooldownSeconds ?? 0,
    maxWatcherLagSeconds: policy.maxWatcherLagSeconds ?? 0,
    version: policy.version,
  }
}

export function WalletPoolSettings() {
  const [items, setItems] = useState<WalletPoolState[]>([])
  const [loading, setLoading] = useState(true)
  const dirtyNetworks = useRef(new Set<string>())
  const { markDirty, markClean } = useTabDirty()

  const handleDirtyChange = useCallback(
    (networkId: string, dirty: boolean) => {
      if (dirty) {
        dirtyNetworks.current.add(networkId)
      } else {
        dirtyNetworks.current.delete(networkId)
      }
      if (dirtyNetworks.current.size > 0) {
        markDirty()
      } else {
        markClean()
      }
    },
    [markClean, markDirty]
  )

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const response = await cryptoApi.walletPools.list()
      setItems(response.items)
    } catch {
      toast.error(
        "Не удалось загрузить пулы кошельков. Обновите страницу или проверьте соединение."
      )
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const networkIds = useMemo(() => items.map((item) => item.policy.networkId), [items])
  const expansion = useExpandedCards("metapus-settings-wallet-pools-expanded", networkIds)

  if (loading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-64 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    )
  }

  if (items.length === 0) {
    return (
      <Alert>
        <AlertTriangle />
        <AlertTitle>Блокчейн-сети не настроены</AlertTitle>
        <AlertDescription>
          Примените миграции к базе данных тенанта — после этого здесь появятся настройки сетей.
        </AlertDescription>
      </Alert>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {items.length > 1 ? (
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={() => expansion.setAll(true)} disabled={expansion.allExpanded}>
            Развернуть все
          </Button>
          <Button variant="outline" size="sm" onClick={() => expansion.setAll(false)} disabled={expansion.allCollapsed}>
            Свернуть все
          </Button>
        </div>
      ) : null}
      {items.map((item) => (
        <WalletPoolNetworkCard
          key={item.policy.networkId}
          initial={item}
          expanded={expansion.isExpanded(item.policy.networkId)}
          onExpandedChange={(expanded) => expansion.setExpanded(item.policy.networkId, expanded)}
          onChanged={(updated) =>
            setItems((current) =>
              current.map((candidate) =>
                candidate.policy.networkId === updated.policy.networkId
                  ? updated
                  : candidate
              )
            )
          }
          onReload={load}
          onDirtyChange={handleDirtyChange}
        />
      ))}
    </div>
  )
}

interface WalletPoolNetworkCardProps {
  initial: WalletPoolState
  onChanged: (state: WalletPoolState) => void
  onReload: () => Promise<void>
  onDirtyChange: (networkId: string, dirty: boolean) => void
  expanded: boolean
  onExpandedChange: (expanded: boolean) => void
}

function WalletPoolNetworkCard({
  initial,
  onChanged,
  onReload,
  onDirtyChange,
  expanded,
  onExpandedChange,
}: WalletPoolNetworkCardProps) {
  const [state, setState] = useState(initial)
  const [form, setForm] = useState<EditablePolicy>(() =>
    toEditable(initial.policy)
  )
  const [saving, setSaving] = useState(false)
  const [reconciling, setReconciling] = useState(false)

  useEffect(() => {
    setState(initial)
    setForm(toEditable(initial.policy))
    onDirtyChange(initial.policy.networkId, false)
  }, [initial, onDirtyChange])

  const provisioningSupported =
    state.capabilities.walletProvisioning !== "unsupported"
  const requiresActivation =
    state.capabilities.walletProvisioning === "derived_with_activation"
  const errors = useMemo(
    () => validatePolicy(form, requiresActivation),
    [form, requiresActivation]
  )
  const hasErrors = Object.keys(errors).length > 0
  const isDirty = !editablePolicyEquals(form, state.policy)
  const progress =
    state.policy.targetReady > 0
      ? Math.min(100, (state.stats.ready / state.policy.targetReady) * 100)
      : 0

  const updateNumber = (
    field: Exclude<keyof EditablePolicy, "activation" | "enabled" | "version">,
    raw: string
  ) => {
    const parsed = Number(raw)
    const next = {
      ...form,
      [field]: Number.isFinite(parsed) ? Math.trunc(parsed) : 0,
    }
    setForm(next)
    onDirtyChange(state.policy.networkId, !editablePolicyEquals(next, state.policy))
  }

  const updateActivationNumber = (
    field: keyof WalletPoolActivationPolicy,
    raw: string
  ) => {
	const value = field === "maxConcurrent"
	  ? Math.trunc(Number(raw) || 0)
	  : raw.replace(/\D/g, "")
    const next = {
      ...form,
      activation: {
        ...form.activation,
		[field]: value,
      },
    }
    setForm(next)
    onDirtyChange(state.policy.networkId, !editablePolicyEquals(next, state.policy))
  }

  const save = async () => {
    if (hasErrors) {
      toast.error("Исправьте ошибки в настройках")
      return
    }
    setSaving(true)
    try {
      const { activation, ...common } = form
      const payload: WalletPoolPolicyPatch = {
        ...common,
        ...(requiresActivation ? { activation } : {}),
      }
      const updated = await cryptoApi.walletPools.updatePolicy(
        state.policy.networkId,
        payload
      )
      setState(updated)
      setForm(toEditable(updated.policy))
      onDirtyChange(updated.policy.networkId, false)
      onChanged(updated)
      toast.success("Настройки пула сохранены")
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        toast.error("Настройки уже изменены. Загружаю актуальную версию.")
        await onReload()
      } else {
        toast.error("Не удалось сохранить настройки. Проверьте значения и повторите попытку.")
      }
    } finally {
      setSaving(false)
    }
  }

  const reconcile = async () => {
    setReconciling(true)
    try {
      const response = await cryptoApi.walletPools.reconcile(state.policy.networkId)
      setState(response.state)
      setForm(toEditable(response.state.policy))
      onDirtyChange(response.state.policy.networkId, false)
      onChanged(response.state)
      const created = response.results.reduce(
        (total, result) => total + result.created,
        0
      )
      toast.success(
        created > 0
          ? `Запланировано создание кошельков: ${created}`
          : "Создание уже запланировано или целевое количество адресов достигнуто"
      )
    } catch {
      toast.error(
        "Не удалось запланировать создание адресов. Проверьте состояние сети и повторите попытку."
      )
    } finally {
      setReconciling(false)
    }
  }

  const policy = state.policy
  const footerRef = useRef<HTMLDivElement>(null)
  const saveIcon = saving ? <LoaderCircle data-icon="inline-start" className="animate-spin" /> : <Save data-icon="inline-start" />

  return (
    <Collapsible open={expanded} onOpenChange={onExpandedChange} className="flex items-stretch gap-3">
      <Card className="min-w-0 flex-1">
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
                  aria-label={expanded ? `Свернуть пул ${policy.networkName}` : `Развернуть пул ${policy.networkName}`}
                  title={expanded ? "Свернуть" : "Развернуть"}
                >
                  <ChevronRight className={cn("transition-transform duration-200", expanded && "rotate-90")} />
                </Button>
              </CollapsibleTrigger>
              <div className="flex min-w-0 flex-col gap-1.5">
                <CardTitle>{policy.networkName}</CardTitle>
                <CardDescription>
                  {expanded
                    ? `${policy.networkCode} · суммы в минимальных единицах ${policy.nativeTokenSymbol}: 10⁻${policy.nativeDecimals}`
                    : `${policy.networkCode} · готово ${state.stats.ready} из ${policy.targetReady} · ${policy.enabled ? "создание адресов включено" : "создание адресов выключено"}`}
                </CardDescription>
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {isDirty ? <Badge variant="warning">Не сохранено</Badge> : null}
              <Badge variant={healthVariants[state.state]}>
                {healthLabels[state.state]}
              </Badge>
            </div>
          </div>
        </CardHeader>

        <CollapsibleContent className="data-[state=closed]:hidden">
          <CardContent className="flex flex-col gap-6">
            {(state.state === "depleted" ||
              !provisioningSupported ||
              policy.needsReview ||
              policy.leaseBlockedReason === "watcher_lag" ||
              policy.lastErrorCode) && (
              <Alert variant={state.state === "depleted" ? "destructive" : "default"}>
                <AlertTriangle />
                <AlertTitle>
                  {poolAlertTitle(state)}
                </AlertTitle>
                <AlertDescription>{poolAlertDescription(state)}</AlertDescription>
              </Alert>
            )}

            <div className="flex flex-col gap-2">
              <div className="flex items-center justify-between gap-3 text-sm">
                <span>Готовые адреса</span>
                <span>
                  {state.stats.ready} / {policy.targetReady}
                </span>
              </div>
              <Progress
                value={progress}
                aria-label={`Готово ${state.stats.ready} из ${policy.targetReady}`}
              />
            </div>

            <dl className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
              <PoolMetric label="Карантин" value={state.stats.cooldown} />
              <PoolMetric label="В аренде" value={state.stats.leased} />
              <PoolMetric label="Постоянные" value={state.stats.assigned} />
              <PoolMetric label="Создаются" value={state.stats.provisioning} />
              {requiresActivation ? (
                <PoolMetric label="Активируются" value={state.stats.activating} />
              ) : null}
              <PoolMetric label="Ждут консолидации" value={state.stats.sweepPending} />
              <PoolMetric label="Удержано" value={state.stats.held} />
              <PoolMetric label="С ошибкой" value={state.stats.failed} />
              <PoolMetric label="Всего" value={state.stats.total} />
              <PoolMetric label="Inbox unmatched" value={state.stats.inboxUnmatched} />
              <PoolMetric label="Inbox review" value={state.stats.inboxNeedsReview} />
              <PoolMetric label="Inbox poison" value={state.stats.inboxPoison} />
            </dl>

            <Field
              orientation="horizontal"
              data-disabled={policy.needsReview || !provisioningSupported}
            >
              <FieldContent>
                <FieldLabel htmlFor={`${policy.networkId}-enabled`}>
                  Автоматическое создание адресов
                </FieldLabel>
                <FieldDescription>
                  Воркер создаёт адреса, когда их количество снижается до порога
                  {requiresActivation ? ", и активирует их в блокчейне" : ""}.
                </FieldDescription>
              </FieldContent>
              <Switch
                id={`${policy.networkId}-enabled`}
                checked={form.enabled}
                disabled={policy.needsReview || !provisioningSupported}
                onCheckedChange={(enabled) => {
                  const next = { ...form, enabled }
                  setForm(next)
                  onDirtyChange(
                    state.policy.networkId,
                    !editablePolicyEquals(next, state.policy)
                  )
                }}
              />
            </Field>

            <Separator />

            <FieldSet>
              <FieldLegend>Количество готовых адресов</FieldLegend>
              <FieldDescription>
                Когда готовых адресов становится не больше порога, воркер планирует новые кошельки до целевого количества. Выдача останавливается при достижении минимального остатка.
              </FieldDescription>
              <FieldGroup className="grid gap-4 md:grid-cols-2">
                <PolicyNumberField
                  id={`${policy.networkId}-low`}
                  label="Порог создания адресов"
                  description="Создание новых адресов планируется, когда готовых адресов не больше этого значения."
                  value={form.lowWatermark}
                  error={errors.lowWatermark}
                  onChange={(value) => updateNumber("lowWatermark", value)}
                />
                <PolicyNumberField
                  id={`${policy.networkId}-target`}
                  label="Целевое количество"
                  description="Столько активных свободных адресов воркер старается поддерживать."
                  value={form.targetReady}
                  error={errors.targetReady}
                  onChange={(value) => updateNumber("targetReady", value)}
                />
                <PolicyNumberField
                  id={`${policy.networkId}-max-total`}
                  label="Общий лимит кошельков"
                  description="Включает существующие кошельки и незавершённые задачи создания."
                  value={form.maxTotalWallets}
                  error={errors.maxTotalWallets}
                  onChange={(value) => updateNumber("maxTotalWallets", value)}
                />
                <PolicyNumberField
                  id={`${policy.networkId}-global-reserve`}
                  label="Минимальный остаток готовых адресов"
                  description="Если готовых адресов осталось не больше этого значения, новые адреса не выдаются. Значение не выше порога создания."
                  value={form.minGlobalReadyReserve}
                  error={errors.minGlobalReadyReserve}
                  onChange={(value) => updateNumber("minGlobalReadyReserve", value)}
                />
                <PolicyNumberField
                  id={`${policy.networkId}-retry-after`}
                  label="Пауза повторного запроса, с"
                  description="Передаётся клиенту в Retry-After, если свободных адресов нет."
                  value={form.retryAfterSeconds}
                  error={errors.retryAfterSeconds}
                  onChange={(value) => updateNumber("retryAfterSeconds", value)}
                />
                <PolicyNumberField
                  id={`${policy.networkId}-reuse-cooldown`}
                  label="Карантин повторной выдачи, с"
                  description="После закрытия сессии адрес не выдаётся новому клиенту. 0 — выключено. Nile: 1800, Sepolia: 1200."
                  value={form.addressReuseCooldownSeconds}
                  error={errors.addressReuseCooldownSeconds}
                  onChange={(value) => updateNumber("addressReuseCooldownSeconds", value)}
                />
                <PolicyNumberField
                  id={`${policy.networkId}-watcher-lag`}
                  label="Допустимое отставание watcher, с"
                  description="Если checkpoint отстаёт сильнее, выдача адресов останавливается. Обязательно при карантине больше 0. Nile: 900, Sepolia: 600."
                  value={form.maxWatcherLagSeconds}
                  error={errors.maxWatcherLagSeconds}
                  onChange={(value) => updateNumber("maxWatcherLagSeconds", value)}
                />
              </FieldGroup>
            </FieldSet>

            <Separator />

            <FieldSet>
              <FieldLegend>Лимиты мерчанта</FieldLegend>
              <FieldDescription>
                Значения действуют по умолчанию в этой сети. Индивидуальные настройки мерчанта имеют приоритет.
              </FieldDescription>
              <FieldGroup className="grid gap-4 md:grid-cols-2">
                <PolicyNumberField
                  id={`${policy.networkId}-merchant-active-leases`}
                  label="Активные аренды на мерчанта"
                  description="Максимум одновременно действующих адресов, выданных инвойсам мерчанта."
                  value={form.maxActiveInvoiceLeasesPerMerchant}
                  error={errors.maxActiveInvoiceLeasesPerMerchant}
                  onChange={(value) =>
                    updateNumber("maxActiveInvoiceLeasesPerMerchant", value)
                  }
                />
                <PolicyNumberField
                  id={`${policy.networkId}-merchant-persistent`}
                  label="Постоянные адреса на мерчанта"
                  description="Максимум адресов, закреплённых за клиентами мерчанта без срока аренды."
                  value={form.maxPersistentAddressesPerMerchant}
                  error={errors.maxPersistentAddressesPerMerchant}
                  onChange={(value) =>
                    updateNumber("maxPersistentAddressesPerMerchant", value)
                  }
                />
                <PolicyNumberField
                  id={`${policy.networkId}-merchant-rate`}
                  label="Новые аренды в минуту на мерчанта"
                  description="Ограничивает всплески запросов. Идемпотентный повтор лимит не расходует."
                  value={form.maxLeaseStartsPerMinutePerMerchant}
                  error={errors.maxLeaseStartsPerMinutePerMerchant}
                  onChange={(value) =>
                    updateNumber("maxLeaseStartsPerMinutePerMerchant", value)
                  }
                />
              </FieldGroup>
            </FieldSet>

            <Separator />

            <FieldSet>
              <FieldLegend>
                {requiresActivation ? "Создание и активация" : "Создание адресов"}
              </FieldLegend>
              <FieldDescription>
                Воркер получает адреса через Vault и сверяет их с блокчейном.
                {requiresActivation
                  ? ` Параметры активации указаны в минимальных единицах ${policy.nativeTokenSymbol}: 10⁻${policy.nativeDecimals}.`
                  : " Отдельная on-chain активация для этой family не требуется."}
              </FieldDescription>
              <FieldGroup className="grid gap-4 md:grid-cols-2">
                <PolicyNumberField
                  id={`${policy.networkId}-batch`}
                  label="Кошельки за один цикл"
                  description="Максимум задач, планируемых за один цикл автоматического создания адресов."
                  value={form.maxProvisionBatch}
                  error={errors.maxProvisionBatch}
                  onChange={(value) => updateNumber("maxProvisionBatch", value)}
                />
                {requiresActivation ? (
                  <>
                    <PolicyNumberField
                      id={`${policy.networkId}-concurrency`}
                      label="Одновременные активации"
                      description="Ограничивает число параллельных активаций и резервов комиссии."
                      value={form.activation.maxConcurrent}
                      error={errors.maxConcurrent}
                      onChange={(value) =>
                        updateActivationNumber("maxConcurrent", value)
                      }
                    />
                    <PolicyNumberField
                      id={`${policy.networkId}-activation-reserve`}
                      label="Резерв на одну активацию"
                      description="Эта сумма резервируется на горячем кошельке перед подписанием транзакции активации."
                      value={form.activation.reserveMinor}
                      error={errors.reserveMinor}
                      onChange={(value) =>
                        updateActivationNumber("reserveMinor", value)
                      }
                    />
                    <PolicyNumberField
                      id={`${policy.networkId}-hot-reserve`}
                      label="Неснижаемый остаток горячего кошелька"
                      description="Новая активация не начинается, если после резервирования баланс станет ниже этого значения."
                      value={form.activation.minHotBalanceMinor}
                      error={errors.minHotBalanceMinor}
                      onChange={(value) =>
                        updateActivationNumber("minHotBalanceMinor", value)
                      }
                    />
                    <PolicyNumberField
                      id={`${policy.networkId}-daily-budget`}
                      label="Дневной бюджет активаций"
                      description="Лимит расходов за сутки UTC с учётом уже потраченных и зарезервированных сумм."
                      value={form.activation.dailyBudgetMinor}
                      error={errors.dailyBudgetMinor}
                      onChange={(value) =>
                        updateActivationNumber("dailyBudgetMinor", value)
                      }
                    />
                  </>
                ) : null}
              </FieldGroup>
            </FieldSet>
          </CardContent>

          <CardFooter ref={footerRef} className="flex flex-wrap justify-between gap-3">
            <Button
              variant="outline"
              disabled={
                reconciling || !policy.enabled || policy.needsReview || isDirty
                || !provisioningSupported
              }
              onClick={reconcile}
            >
              {reconciling ? (
                <LoaderCircle data-icon="inline-start" className="animate-spin" />
              ) : (
                <RefreshCw data-icon="inline-start" />
              )}
              Запланировать создание адресов
            </Button>
            <Button disabled={saving || hasErrors || !isDirty} onClick={save}>
              {saveIcon}
              Сохранить настройки
            </Button>
          </CardFooter>
        </CollapsibleContent>
      </Card>
      {/* A pool's settings run long: while they are unsaved, the save action waits beside the card. */}
      <FloatingSaveRail active={expanded && isDirty} anchorRef={footerRef}>
        <FloatingSaveButton disabled={saving || hasErrors || !isDirty} onClick={() => void save()}
          aria-label={`Сохранить настройки пула ${policy.networkName}`}>
          {saveIcon}
          Сохранить
        </FloatingSaveButton>
      </FloatingSaveRail>
    </Collapsible>
  )
}

function PoolMetric({ label, value }: { label: string; value: number }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium">{value}</dd>
    </div>
  )
}

interface PolicyNumberFieldProps {
  id: string
  label: string
  description?: string
  value: number | string
  max?: number
  error?: string
  onChange: (value: string) => void
}

function PolicyNumberField({
  id,
  label,
  description,
  value,
  max,
  error,
  onChange,
}: PolicyNumberFieldProps) {
  return (
    <Field data-invalid={Boolean(error)}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
		type="text"
		inputMode="numeric"
		pattern="[0-9]*"
        min={0}
        max={max}
        step={1}
        value={value}
        aria-invalid={Boolean(error)}
        onChange={(event) => onChange(event.target.value)}
      />
      {description && <FieldDescription>{description}</FieldDescription>}
      <FieldError>{error}</FieldError>
    </Field>
  )
}

function validatePolicy(
  policy: EditablePolicy,
  requiresActivation: boolean
): Record<string, string> {
  const errors: Record<string, string> = {}
  if (policy.lowWatermark < 0) {
    errors.lowWatermark = "Значение не может быть отрицательным."
  }
  if (policy.lowWatermark >= policy.targetReady) {
    errors.lowWatermark = "Порог должен быть меньше целевого количества."
    errors.targetReady = "Целевое количество должно быть больше порога."
  }
  if (policy.targetReady > policy.maxTotalWallets) {
    errors.targetReady = "Не может превышать максимум кошельков."
    errors.maxTotalWallets = "Общий лимит должен быть не меньше целевого количества."
  }
  if (policy.maxProvisionBatch <= 0) {
    errors.maxProvisionBatch = "Укажите число больше нуля."
  }
  if (policy.maxActiveInvoiceLeasesPerMerchant <= 0) {
    errors.maxActiveInvoiceLeasesPerMerchant =
      "Укажите число больше нуля."
  }
  if (policy.maxPersistentAddressesPerMerchant <= 0) {
    errors.maxPersistentAddressesPerMerchant =
      "Укажите число больше нуля."
  }
  if (policy.maxLeaseStartsPerMinutePerMerchant <= 0) {
    errors.maxLeaseStartsPerMinutePerMerchant =
      "Укажите число больше нуля."
  }
  if (policy.minGlobalReadyReserve < 0) {
    errors.minGlobalReadyReserve = "Значение не может быть отрицательным."
  }
  if (policy.minGlobalReadyReserve > policy.lowWatermark) {
    errors.minGlobalReadyReserve = "Минимальный остаток не может превышать порог создания."
    errors.lowWatermark = "Порог должен быть не меньше минимального остатка."
  }
  if (requiresActivation) {
    if (policy.activation.maxConcurrent <= 0) {
      errors.maxConcurrent = "Укажите число больше нуля."
    }
	if (!isCanonicalRawAmount(policy.activation.reserveMinor)) {
      errors.reserveMinor = "Значение не может быть отрицательным."
    }
	if (!isCanonicalRawAmount(policy.activation.minHotBalanceMinor)) {
      errors.minHotBalanceMinor = "Значение не может быть отрицательным."
    }
	if (!isCanonicalRawAmount(policy.activation.dailyBudgetMinor)) {
      errors.dailyBudgetMinor = "Значение не может быть отрицательным."
    }
  }
  if (policy.retryAfterSeconds <= 0) {
    errors.retryAfterSeconds = "Укажите число больше нуля."
  }
  if (policy.addressReuseCooldownSeconds < 0) {
    errors.addressReuseCooldownSeconds = "Значение не может быть отрицательным."
  }
  if (policy.maxWatcherLagSeconds < 0) {
    errors.maxWatcherLagSeconds = "Значение не может быть отрицательным."
  }
  if (policy.addressReuseCooldownSeconds > 0 && policy.maxWatcherLagSeconds <= 0) {
    errors.maxWatcherLagSeconds = "При включённом карантине укажите допустимое отставание watcher."
  }
  return errors
}

function isCanonicalRawAmount(value: string): boolean {
  return /^(0|[1-9]\d*)$/.test(value)
}

function editablePolicyEquals(
  editable: EditablePolicy,
  policy: WalletPoolPolicy
): boolean {
  const current = toEditable(policy)
  return (
    editable.enabled === current.enabled &&
    editable.lowWatermark === current.lowWatermark &&
    editable.targetReady === current.targetReady &&
    editable.maxTotalWallets === current.maxTotalWallets &&
    editable.maxProvisionBatch === current.maxProvisionBatch &&
    editable.maxActiveInvoiceLeasesPerMerchant ===
      current.maxActiveInvoiceLeasesPerMerchant &&
    editable.maxPersistentAddressesPerMerchant ===
      current.maxPersistentAddressesPerMerchant &&
    editable.maxLeaseStartsPerMinutePerMerchant ===
      current.maxLeaseStartsPerMinutePerMerchant &&
    editable.minGlobalReadyReserve === current.minGlobalReadyReserve &&
    editable.activation.maxConcurrent === current.activation.maxConcurrent &&
    editable.activation.reserveMinor === current.activation.reserveMinor &&
    editable.activation.minHotBalanceMinor ===
      current.activation.minHotBalanceMinor &&
    editable.activation.dailyBudgetMinor ===
      current.activation.dailyBudgetMinor &&
    editable.retryAfterSeconds === current.retryAfterSeconds &&
    editable.addressReuseCooldownSeconds === current.addressReuseCooldownSeconds &&
    editable.maxWatcherLagSeconds === current.maxWatcherLagSeconds &&
    editable.version === current.version
  )
}
