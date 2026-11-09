"use client"
import type { JsonObject } from "@/lib/json-value"
import type { WidgetType } from "@/types/dashboard"
import { Field, FieldGroup, FieldLabel, FieldDescription } from "@/components/ui/field"
import { Select, SelectTrigger, SelectValue, SelectContent, SelectGroup, SelectItem } from "@/components/ui/select"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { counterLabels, queueKinds } from "@/lib/admin-dashboard"

export function WidgetConfigPanel({ widgetType, config, onConfigChange }: { widgetType: WidgetType; config: JsonObject; onConfigChange: (value: JsonObject) => void }) {
  const select = (key: string, label: string, options: Record<string, string>) => <Field><FieldLabel>{label}</FieldLabel><Select value={String(config[key])} onValueChange={v => onConfigChange({ ...config, [key]: v })}><SelectTrigger aria-label={label}><SelectValue /></SelectTrigger><SelectContent><SelectGroup>{Object.entries(options).map(([value, title]) => <SelectItem key={value} value={value}>{title}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>
  return <FieldGroup>
    {widgetType === "attention-counter" && select("metric", "Показатель", counterLabels)}
    {widgetType === "attention-queue" && select("kind", "Очередь", queueKinds)}
    {(widgetType === "attention-queue" || widgetType === "custody-reconciliation") && <Field><FieldLabel htmlFor="widget-limit">Количество строк</FieldLabel><Input id="widget-limit" type="number" min={1} max={20} value={Number(config.limit)} onChange={e => { const value = Number(e.target.value); if (Number.isInteger(value) && value >= 1 && value <= 20) onConfigChange({ ...config, limit: value }) }} /><FieldDescription>От 1 до 20. Итоги учитывают все операции.</FieldDescription></Field>}
    {widgetType === "financial-summary" && select("view", "Остатки", { obligations: "Обязательства перед мерчантами", wallets: "Средства на кошельках" })}
    {widgetType === "processing-health" && <Field orientation="horizontal"><FieldLabel htmlFor="widget-workers">Показывать статистику задач</FieldLabel><Switch id="widget-workers" checked={Boolean(config.showWorkerStats)} onCheckedChange={showWorkerStats => onConfigChange({ ...config, showWorkerStats })} /></Field>}
    {["receipts-volume", "platform-fees", "payment-results"].includes(widgetType) && <>{select("period", "Период по умолчанию", { "7d": "7 дней", "30d": "30 дней", "90d": "90 дней" })}<FieldDescription>Токен и сеть можно выбрать в самом виджете.</FieldDescription></>}
  </FieldGroup>
}
