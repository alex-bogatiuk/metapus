"use client"

import { useId } from "react"
import { isRuntimeFresh, networkReadinessProblem } from "@/configuration/crypto/lib/crypto-network-readiness"
import { HelpCircle } from "lucide-react"

import type {
  CryptoNetworkPolicy,
  CryptoNetworkPolicyPatch,
  CryptoNetworkState,
  CryptoRuntimeEndpoint,
  FeeFundingMode,
  ResourceStrategy,
} from "@/configuration/crypto/types/crypto-network"
import type { BadgeProps } from "@/components/ui/badge"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"

export interface EditablePolicy {
  version: number
  enabled: boolean
  perTransactionMaxFeeMinor: string
  dailyNetworkFeeBudgetMinor: string
  feeFundingMode: FeeFundingMode
  dailyJitFundingBudgetMinor: string
  minFeeWalletBalanceMinor: string
  minSourceWalletResidualMinor: string
  maxFundingAmountMinor: string
  resourceStrategy: ResourceStrategy
  resourceProviderAccountIds: string[]
  resourceProviderFailureThreshold: number
  resourceProviderOpenSeconds: number
  retryDelaySeconds: number
  rpcFailureThreshold: number
  rpcOpenSeconds: number
}

export type AmountField =
  | "perTransactionMaxFeeMinor"
  | "dailyNetworkFeeBudgetMinor"
  | "dailyJitFundingBudgetMinor"
  | "minFeeWalletBalanceMinor"
  | "minSourceWalletResidualMinor"
  | "maxFundingAmountMinor"

export type NumericField =
  | "retryDelaySeconds"
  | "rpcFailureThreshold"
  | "rpcOpenSeconds"
  | "resourceProviderFailureThreshold"
  | "resourceProviderOpenSeconds"

export const strategyLabels: Record<ResourceStrategy, string> = {
  rental_required: "Только аренда ресурсов",
  prefer_rental: "Аренда, иначе нативная комиссия",
  burn_only: "Только нативная комиссия",
}

export const strategyDescriptions: Record<ResourceStrategy, string> = {
  rental_required: "Если аренда ресурса недоступна, операция не выполняется.",
  prefer_rental: "При доступной аренде используются ресурсы; при сбое комиссия оплачивается нативным токеном.",
  burn_only: "Внешние ресурсы не арендуются.",
}

export const endpointLabels: Record<string, string> = {
  rpc: "Ethereum RPC",
  indexer: "Индексатор",
  fullnode: "Полный узел",
  solidity: "SolidityNode",
  resource: "Аренда ресурсов",
}

export const stateLabels: Record<CryptoRuntimeEndpoint["state"], string> = {
  closed: "Готов",
  open: "Запросы приостановлены",
  half_open: "Проверка",
  degraded: "Нестабильно",
  lagging: "Отстаёт",
  invalid: "Некорректный ответ",
  wrong_chain: "Неверный chain ID",
}

export const stateVariants: Record<CryptoRuntimeEndpoint["state"], NonNullable<BadgeProps["variant"]>> = {
  closed: "success",
  open: "destructive",
  half_open: "warning",
  degraded: "warning",
  lagging: "warning",
  invalid: "destructive",
  wrong_chain: "destructive",
}

export const finalityLabels = {
  confirmations: "по числу подтверждений",
  protocol_finalized: "по protocol finalized",
  confirmations_or_finalized: "по подтверждениям или protocol finalized",
} as const

export const feeFundingLabels: Record<FeeFundingMode, string> = {
  source_only: "Комиссия с исходного кошелька",
  jit: "Автоматическое пополнение комиссии",
}

export const EXPANDED_NETWORKS_STORAGE_KEY = "metapus-admin-crypto-networks-expanded"

export interface FieldHelpInfo {
  title: string
  content: React.ReactNode
}

export interface FieldHelpProps {
  title?: string
  children: React.ReactNode
}

export function FieldHelp({ title, children }: FieldHelpProps) {
  return (
    <TooltipProvider delayDuration={150}>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            className="inline-flex size-4 shrink-0 items-center justify-center rounded-full text-muted-foreground/70 transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
            aria-label={title ? `Справка: ${title}` : "Справка по настройке"}
          >
            <HelpCircle className="size-3.5" />
          </button>
        </TooltipTrigger>
        <TooltipContent
          side="top"
          align="start"
          className="max-w-xs space-y-1.5 p-3 text-left font-normal leading-relaxed shadow-lg"
        >
          {title ? <p className="font-semibold text-foreground">{title}</p> : null}
          <div className="space-y-1 text-xs text-muted-foreground">{children}</div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

export function getNativeTokenSymbol(family: import("@/configuration/crypto/types/crypto-network").ChainFamily): string {
  switch (family) {
    case "tron":
      return "TRX"
    case "evm":
      return "ETH"
    case "ton":
      return "TON"
    case "solana":
      return "SOL"
    default:
      return "NATIVE"
  }
}

export function formatMinorToHuman(rawMinor: string, decimals: number, symbol: string): string | null {
  if (!rawMinor || !/^[0-9]+$/.test(rawMinor)) return null
  try {
    const rawBigInt = BigInt(rawMinor)
    if (rawBigInt === 0n) return `≈ 0 ${symbol}`

    const divisor = 10n ** BigInt(decimals)
    const integerPart = rawBigInt / divisor
    const remainder = rawBigInt % divisor

    let fractionalStr = remainder.toString().padStart(decimals, "0")
    fractionalStr = fractionalStr.replace(/0+$/, "")

    const formattedInt = new Intl.NumberFormat("ru-RU").format(integerPart)
    const humanAmount = fractionalStr ? `${formattedInt}.${fractionalStr}` : formattedInt

    let subUnitText = ""
    const symUpper = symbol.toUpperCase()
    if (decimals === 6 && (symUpper === "TRX" || symUpper === "USDT")) {
      const sunFormatted = new Intl.NumberFormat("ru-RU").format(rawBigInt)
      subUnitText = ` (${sunFormatted} Sun)`
    } else if (
      decimals === 18 &&
      (symUpper.includes("ETH") ||
        symUpper === "BNB" ||
        symUpper === "POL" ||
        symUpper === "MATIC" ||
        symUpper === "AVAX")
    ) {
      if (rawBigInt >= 1_000_000_000n && rawBigInt < 10n ** 16n) {
        const gwei = rawBigInt / 1_000_000_000n
        subUnitText = ` (${new Intl.NumberFormat("ru-RU").format(gwei)} Gwei)`
      } else if (rawBigInt < 10n ** 12n) {
        const weiFormatted = new Intl.NumberFormat("ru-RU").format(rawBigInt)
        subUnitText = ` (${weiFormatted} Wei)`
      }
    } else if (decimals === 9 && (symUpper === "SOL" || symUpper === "TON")) {
      const subUnitName = symUpper === "SOL" ? "Lamports" : "nanoTON"
      if (rawBigInt < 10n ** 9n) {
        const subFormatted = new Intl.NumberFormat("ru-RU").format(rawBigInt)
        subUnitText = ` (${subFormatted} ${subUnitName})`
      }
    } else if (decimals === 8 && symUpper === "BTC") {
      if (rawBigInt < 10n ** 8n) {
        const satFormatted = new Intl.NumberFormat("ru-RU").format(rawBigInt)
        subUnitText = ` (${satFormatted} Sat)`
      }
    }

    return `≈ ${humanAmount} ${symbol}${subUnitText}`
  } catch {
    return null
  }
}

export function toEditable(policy: CryptoNetworkPolicy): EditablePolicy {
  return {
    version: policy.version,
    enabled: policy.enabled,
    perTransactionMaxFeeMinor: policy.perTransactionMaxFeeMinor,
    dailyNetworkFeeBudgetMinor: policy.dailyNetworkFeeBudgetMinor,
    feeFundingMode: policy.feeFunding.mode,
    dailyJitFundingBudgetMinor: policy.feeFunding.dailyBudgetMinor,
    minFeeWalletBalanceMinor: policy.feeFunding.minFeeWalletBalanceMinor,
    minSourceWalletResidualMinor: policy.feeFunding.minSourceWalletResidualMinor,
    maxFundingAmountMinor: policy.feeFunding.maxFundingAmountMinor,
    resourceStrategy: policy.externalResources?.strategy ?? "burn_only",
    resourceProviderAccountIds: policy.externalResources?.providerAccountIds ?? [],
    resourceProviderFailureThreshold: policy.externalResources?.providerFailureThreshold ?? 3,
    resourceProviderOpenSeconds: policy.externalResources?.providerOpenSeconds ?? 60,
    retryDelaySeconds: policy.retryDelaySeconds,
    rpcFailureThreshold: policy.rpcFailureThreshold,
    rpcOpenSeconds: policy.rpcOpenSeconds,
  }
}

export function toPatch(form: EditablePolicy, hasExternalResources: boolean): CryptoNetworkPolicyPatch {
  return {
    version: form.version,
    enabled: form.enabled,
    perTransactionMaxFeeMinor: form.perTransactionMaxFeeMinor,
    dailyNetworkFeeBudgetMinor: form.dailyNetworkFeeBudgetMinor,
    feeFunding: {
      mode: form.feeFundingMode,
      dailyBudgetMinor: form.dailyJitFundingBudgetMinor,
      minFeeWalletBalanceMinor: form.minFeeWalletBalanceMinor,
      minSourceWalletResidualMinor: form.minSourceWalletResidualMinor,
      maxFundingAmountMinor: form.maxFundingAmountMinor,
    },
    externalResources: hasExternalResources
      ? {
          strategy: form.resourceStrategy,
          providerAccountIds: form.resourceProviderAccountIds,
          providerFailureThreshold: form.resourceProviderFailureThreshold,
          providerOpenSeconds: form.resourceProviderOpenSeconds,
        }
      : undefined,
    clearExternalResources: !hasExternalResources,
    retryDelaySeconds: form.retryDelaySeconds,
    rpcFailureThreshold: form.rpcFailureThreshold,
    rpcOpenSeconds: form.rpcOpenSeconds,
  }
}

export function policiesEqual(left: EditablePolicy, right: EditablePolicy): boolean {
  return JSON.stringify(left) === JSON.stringify(right)
}

export { isRuntimeFresh }


export const EMPTY_EXPANDED_NETWORKS: Record<string, boolean> = {}
let expandedNetworksSnapshot: Record<string, boolean> = EMPTY_EXPANDED_NETWORKS
const expandedNetworksListeners = new Set<() => void>()

export function readExpandedNetworks(): Record<string, boolean> {
  if (typeof window === "undefined") return EMPTY_EXPANDED_NETWORKS
  try {
    const raw = localStorage.getItem(EXPANDED_NETWORKS_STORAGE_KEY)
    if (!raw) return EMPTY_EXPANDED_NETWORKS
    const parsed = JSON.parse(raw) as Record<string, boolean>
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return EMPTY_EXPANDED_NETWORKS
    const result: Record<string, boolean> = {}
    for (const [id, value] of Object.entries(parsed)) {
      if (typeof value === "boolean") result[id] = value
    }
    return Object.keys(result).length > 0 ? result : EMPTY_EXPANDED_NETWORKS
  } catch {
    return EMPTY_EXPANDED_NETWORKS
  }
}

export function getExpandedNetworksSnapshot(): Record<string, boolean> {
  return expandedNetworksSnapshot
}

export function getExpandedNetworksServerSnapshot(): Record<string, boolean> {
  return EMPTY_EXPANDED_NETWORKS
}

export function subscribeExpandedNetworks(onStoreChange: () => void): () => void {
  expandedNetworksListeners.add(onStoreChange)
  return () => {
    expandedNetworksListeners.delete(onStoreChange)
  }
}

export function commitExpandedNetworks(next: Record<string, boolean>) {
  expandedNetworksSnapshot = next
  localStorage.setItem(EXPANDED_NETWORKS_STORAGE_KEY, JSON.stringify(next))
  expandedNetworksListeners.forEach((listener) => listener())
}

if (typeof window !== "undefined") {
  expandedNetworksSnapshot = readExpandedNetworks()
}

export function isNetworkCardExpanded(
  networkId: string,
  expandedById: Record<string, boolean>,
  networkCount: number
): boolean {
  return expandedById[networkId] ?? networkCount === 1
}

export function networkHeaderDescription(
  state: CryptoNetworkState,
  form: EditablePolicy,
  hasExternalResources: boolean,
  policyVersion: number,
  expanded: boolean
): string {
  const finality =
    state.network.finalityMode === "protocol_finalized"
      ? `финальность ${finalityLabels[state.network.finalityMode]}`
      : `финальность ${finalityLabels[state.network.finalityMode]} (${state.network.requiredConfirmations} подтверждений)`
  const parts = [
    `Chain ID: ${state.network.networkIdentifier}`,
    finality,
    feeFundingLabels[form.feeFundingMode],
  ]
  if (hasExternalResources) {
    parts.push(strategyLabels[form.resourceStrategy])
  }
  if (expanded) {
    parts.push(`native decimals: ${state.network.nativeDecimals}`)
    parts.push(`версия: ${policyVersion}`)
  }
  return parts.join(" · ")
}

export function networkReady(state: CryptoNetworkState, form: EditablePolicy): boolean {
  return networkReadinessProblem(state, form.resourceStrategy) === null
}

export interface NumberFieldProps {
  field: NumericField
  label: string
  description?: string
  value: number
  error?: string
  help?: FieldHelpInfo
  unit?: string
  onChange: (field: NumericField, raw: string) => void
}

export function NumberField({ field, label, description, value, error, help, unit, onChange }: NumberFieldProps) {
  const inputId = useId()
  return (
    <Field data-invalid={Boolean(error)}>
      <div className="flex items-center justify-between gap-1.5">
        <FieldLabel htmlFor={inputId}>{label}</FieldLabel>
        {help ? <FieldHelp title={help.title}>{help.content}</FieldHelp> : null}
      </div>
      <Input
        id={inputId}
        name={field}
        type="number"
        min={0}
        step={1}
        value={value}
        aria-invalid={Boolean(error)}
        onChange={(event) => onChange(field, event.target.value)}
      />
      {unit && value > 0 ? (
        <p className="text-xs font-mono text-muted-foreground tabular-nums">
          {value} {unit}
        </p>
      ) : null}
      {description ? <FieldDescription>{description}</FieldDescription> : null}
      <FieldError>{error}</FieldError>
    </Field>
  )
}

export interface AmountFieldInputProps {
  field: AmountField
  label: string
  description?: string
  value: string
  error?: string
  help?: FieldHelpInfo
  nativeDecimals?: number
  nativeTokenSymbol?: string
  onChange: (field: AmountField, raw: string) => void
}

export function AmountFieldInput({
  field,
  label,
  description,
  value,
  error,
  help,
  nativeDecimals,
  nativeTokenSymbol,
  onChange,
}: AmountFieldInputProps) {
  const inputId = useId()
  const livePreview =
    nativeDecimals !== undefined && nativeTokenSymbol
      ? formatMinorToHuman(value, nativeDecimals, nativeTokenSymbol)
      : null

  return (
    <Field data-invalid={Boolean(error)}>
      <div className="flex items-center justify-between gap-1.5">
        <FieldLabel htmlFor={inputId}>{label}</FieldLabel>
        {help ? <FieldHelp title={help.title}>{help.content}</FieldHelp> : null}
      </div>
      <Input
        id={inputId}
        name={field}
        type="text"
        inputMode="numeric"
        pattern="[0-9]*"
        value={value}
        aria-invalid={Boolean(error)}
        onChange={(event) => onChange(field, event.target.value)}
      />
      {livePreview ? (
        <p className="text-xs font-mono font-medium text-emerald-600 dark:text-emerald-400 tabular-nums">
          {livePreview}
        </p>
      ) : null}
      {description ? <FieldDescription>{description}</FieldDescription> : null}
      <FieldError>{error}</FieldError>
    </Field>
  )
}

export function validatePolicy(policy: EditablePolicy, hasExternalResources: boolean): Partial<Record<keyof EditablePolicy, string>> {
  const errors: Partial<Record<keyof EditablePolicy, string>> = {}
  const amounts: AmountField[] = [
    "perTransactionMaxFeeMinor",
    "dailyNetworkFeeBudgetMinor",
    "dailyJitFundingBudgetMinor",
    "minFeeWalletBalanceMinor",
    "minSourceWalletResidualMinor",
    "maxFundingAmountMinor",
  ]
  const positive: NumericField[] = ["retryDelaySeconds", "rpcFailureThreshold", "rpcOpenSeconds"]
  if (hasExternalResources) {
    positive.push("resourceProviderFailureThreshold", "resourceProviderOpenSeconds")
    if (policy.resourceStrategy !== "burn_only") {
      if (policy.resourceProviderAccountIds.length === 0) {
        errors.resourceProviderAccountIds = "Выберите хотя бы один аккаунт провайдера"
      }
    }
  }
  for (const field of amounts) {
    if (!isCanonicalRawAmount(policy[field])) errors[field] = "Укажите неотрицательное целое число"
  }
  for (const field of positive) {
    if (!Number.isSafeInteger(policy[field]) || policy[field] <= 0) errors[field] = "Укажите целое число больше нуля"
  }
  if (policy.feeFundingMode !== "source_only" && policy.feeFundingMode !== "jit") {
    errors.feeFundingMode = "Выберите режим финансирования"
  }
  if (policy.feeFundingMode === "jit") {
    if (!isPositiveRawAmount(policy.maxFundingAmountMinor)) errors.maxFundingAmountMinor = "Лимит должен быть больше нуля"
    if (!isPositiveRawAmount(policy.dailyJitFundingBudgetMinor)) errors.dailyJitFundingBudgetMinor = "Лимит должен быть больше нуля"
    if (
      isCanonicalRawAmount(policy.perTransactionMaxFeeMinor) &&
      isCanonicalRawAmount(policy.minSourceWalletResidualMinor) &&
      isCanonicalRawAmount(policy.maxFundingAmountMinor)
    ) {
      const minimum = BigInt(policy.perTransactionMaxFeeMinor) + BigInt(policy.minSourceWalletResidualMinor)
      if (BigInt(policy.maxFundingAmountMinor) < minimum) {
        errors.maxFundingAmountMinor = `Укажите не меньше ${minimum}: fee cap плюс source residual`
      }
    }
    if (
      isCanonicalRawAmount(policy.maxFundingAmountMinor) &&
      isCanonicalRawAmount(policy.dailyJitFundingBudgetMinor) &&
      BigInt(policy.dailyJitFundingBudgetMinor) <= BigInt(policy.maxFundingAmountMinor)
    ) {
      errors.dailyJitFundingBudgetMinor = "Дневной лимит должен быть больше одного JIT cap"
    }
  }
  if (policy.enabled) {
    if (!isPositiveRawAmount(policy.perTransactionMaxFeeMinor)) errors.perTransactionMaxFeeMinor = "Для включения cap должен быть больше нуля"
    if (!isPositiveRawAmount(policy.dailyNetworkFeeBudgetMinor)) errors.dailyNetworkFeeBudgetMinor = "Для включения бюджет должен быть больше нуля"
  }
  return errors
}

export function isCanonicalRawAmount(value: string): boolean {
  return /^(0|[1-9]\d*)$/.test(value)
}

export function isPositiveRawAmount(value: string): boolean {
  return isCanonicalRawAmount(value) && value !== "0"
}

export function formatTimestamp(value: string): string {
  const timestamp = Date.parse(value)
  return Number.isFinite(timestamp) ? new Date(timestamp).toLocaleString("ru-RU") : "нет данных"
}
