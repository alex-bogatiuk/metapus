import type { RpcconfigEVMSettings, RpcconfigIngestionAlertSettings, RpcconfigRPCNetworkSettings, RpcconfigTRONSettings } from "@/configuration/crypto/types/generated/http-contracts"

type RPCField = keyof RpcconfigEVMSettings | Exclude<keyof RpcconfigTRONSettings, "ingestionMode">
type AlertField = keyof RpcconfigIngestionAlertSettings
export type RPCSettingsField = RPCField | AlertField
export type RPCSettingsForm = Record<RPCSettingsField, string>

export interface RPCSettingsFieldDefinition {
  key: RPCSettingsField
  label: string
  unit: string
  group: "rpc" | "alerts" | "advanced"
  evmOnly?: boolean
  tronOnly?: boolean
  min: number
  max: number
}

export const rpcSettingsFields: RPCSettingsFieldDefinition[] = [
  { key: "attemptTimeoutMs", label: "Таймаут одной попытки", unit: "мс", group: "rpc", min: 1, max: 30_000 },
  { key: "readBudgetMs", label: "Бюджет операции с резервом", unit: "мс", group: "rpc", min: 1, max: 60_000 },
  { key: "maxLagSeconds", label: "Допустимое отставание", unit: "с", group: "alerts", min: 10, max: 3600 },
  { key: "fireAfterSeconds", label: "Длительность проблемы", unit: "с", group: "alerts", min: 30, max: 3600 },
  { key: "recoveryLagSeconds", label: "Отставание для восстановления", unit: "с", group: "alerts", min: 1, max: 3599 },
  { key: "recoverAfterSeconds", label: "Длительность восстановления", unit: "с", group: "alerts", min: 30, max: 3600 },
  { key: "logsTimeoutMs", label: "Таймаут поиска событий EVM", unit: "мс", group: "advanced", evmOnly: true, min: 1, max: 30_000 },
  { key: "maxAttempts", label: "Максимум попыток чтения", unit: "", group: "advanced", min: 1, max: 8 },
  { key: "requestsPerSecond", label: "Запросов в секунду на подключение", unit: "", group: "advanced", min: 1, max: 1000 },
  { key: "providerConcurrency", label: "Одновременных запросов на подключение", unit: "", group: "advanced", min: 2, max: 64 },
  { key: "backfillConcurrency", label: "Из них — запросов истории", unit: "", group: "advanced", min: 1, max: 63 },
  { key: "maxHeadAgeSeconds", label: "Допустимый возраст блока EVM", unit: "с", group: "advanced", evmOnly: true, min: 5, max: 600 },
  { key: "nativeConcurrency", label: "Одновременных загрузок блоков EVM", unit: "", group: "advanced", evmOnly: true, min: 1, max: 16 },
  { key: "prefetchBlocks", label: "Блоков в окне предзагрузки EVM", unit: "", group: "advanced", evmOnly: true, min: 1, max: 128 },
  { key: "solidifiedCatchUpBlocks", label: "Блоков в окне догона TRON", unit: "", group: "advanced", tronOnly: true, min: 4, max: 64 },
  { key: "maxResponseBytes", label: "Максимальный размер ответа", unit: "байт", group: "advanced", min: 1024, max: 64 * 1024 * 1024 },
  { key: "maxBufferedBytes", label: "Память предзагрузки EVM", unit: "байт", group: "advanced", evmOnly: true, min: 1024, max: 512 * 1024 * 1024 },
]

function fieldValue(settings: RpcconfigRPCNetworkSettings, key: RPCSettingsField): number {
  if (key in settings.alerts) return settings.alerts[key as AlertField]
  if (settings.evm) return settings.evm[key as keyof RpcconfigEVMSettings] ?? 0
  if (settings.tron && key in settings.tron) return settings.tron[key as keyof Omit<typeof settings.tron, "ingestionMode">] ?? 0
  return 0
}

export function rpcSettingsForm(settings: RpcconfigRPCNetworkSettings): RPCSettingsForm {
  return Object.fromEntries(rpcSettingsFields.map(field => [field.key, String(fieldValue(settings, field.key))])) as RPCSettingsForm
}

export function rpcSettingsValue(form: RPCSettingsForm, defaults: RpcconfigRPCNetworkSettings, key: RPCSettingsField): number {
  return Number(form[key]) || fieldValue(defaults, key)
}

// A family-specific field applies only to its own chain family.
export function rpcSettingsFieldApplies(field: RPCSettingsFieldDefinition, family: string): boolean {
  return (!field.evmOnly || family === "evm") && (!field.tronOnly || family === "tron")
}

export function rpcSettingsErrors(form: RPCSettingsForm, defaults: RpcconfigRPCNetworkSettings, family: string): Partial<Record<RPCSettingsField, string>> {
  const errors: Partial<Record<RPCSettingsField, string>> = {}
  for (const field of rpcSettingsFields) {
    if (!rpcSettingsFieldApplies(field, family)) continue
    const raw = form[field.key]
    const value = Number(raw)
    if (!/^\d+$/.test(raw) || !Number.isSafeInteger(value) || (value !== 0 && (value < field.min || value > field.max))) {
      errors[field.key] = `Укажите 0 или целое число от ${field.min} до ${field.max}`
    }
  }
  const value = (key: RPCSettingsField) => rpcSettingsValue(form, defaults, key)
  if (value("readBudgetMs") < 2 * value("attemptTimeoutMs")) errors.readBudgetMs = "Бюджет должен вмещать как минимум две попытки RPC"
  if (value("recoveryLagSeconds") >= value("maxLagSeconds")) errors.recoveryLagSeconds = "Порог восстановления должен быть меньше допустимого отставания"
  if (value("backfillConcurrency") >= value("providerConcurrency")) errors.backfillConcurrency = "Оставьте часть запросов для актуальных данных"
  if (family === "evm") {
    if (value("logsTimeoutMs") > value("readBudgetMs")) errors.logsTimeoutMs = "Не больше бюджета операции"
    if (value("prefetchBlocks") < value("nativeConcurrency")) errors.prefetchBlocks = "Не меньше числа одновременных загрузок блоков"
    if (value("maxBufferedBytes") < value("maxResponseBytes") * value("nativeConcurrency")) errors.maxBufferedBytes = "Памяти должно хватать на все одновременные ответы"
  }
  return errors
}

// Preserve mode and other read settings not represented by editable controls.
export function rpcSettingsFromForm(form: RPCSettingsForm, before: RpcconfigRPCNetworkSettings, family: string): RpcconfigRPCNetworkSettings {
  const values = Object.fromEntries(rpcSettingsFields.filter(field => field.group !== "alerts" && rpcSettingsFieldApplies(field, family)).map(field => [field.key, Number(form[field.key])]))
  const alerts: RpcconfigIngestionAlertSettings = {
    maxLagSeconds: Number(form.maxLagSeconds), fireAfterSeconds: Number(form.fireAfterSeconds),
    recoveryLagSeconds: Number(form.recoveryLagSeconds), recoverAfterSeconds: Number(form.recoverAfterSeconds),
  }
  const baseline = rpcSettingsForm(before)
  const rpcChanged = rpcSettingsFields.some(field => field.group !== "alerts" && rpcSettingsFieldApplies(field, family) && form[field.key] !== baseline[field.key])
  if (!rpcChanged) return { ...before, alerts }
  if (family === "evm") return { ...before, evm: { ...before.evm, ...values } as RpcconfigEVMSettings, alerts }
  return { ...before, tron: { ...before.tron, ...values } as NonNullable<RpcconfigRPCNetworkSettings["tron"]>, alerts }
}

export function rpcSettingsFormsEqual(a: RPCSettingsForm, b: RPCSettingsForm): boolean {
  return rpcSettingsFields.every(field => a[field.key] === b[field.key])
}
