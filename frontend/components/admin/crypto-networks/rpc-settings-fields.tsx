"use client"

import { useId } from "react"
import { ChevronRight } from "lucide-react"

import type { IntegrationsRPCNetworkSettings } from "@/types/generated/http-contracts"
import { rpcSettingsFields, rpcSettingsValue, type RPCSettingsField, type RPCSettingsForm } from "@/lib/integrations/rpc-network-settings"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from "@/components/ui/field"
import { Input } from "@/components/ui/input"

interface RPCSettingsFieldsProps {
  family: string
  form: RPCSettingsForm
  defaults: IntegrationsRPCNetworkSettings
  editing: boolean
  disabled: boolean
  errors: Partial<Record<RPCSettingsField, string>>
  onChange: (field: RPCSettingsField, raw: string) => void
}

export function RPCSettingsFields({ family, form, defaults, editing, disabled, errors, onChange }: RPCSettingsFieldsProps) {
  const prefix = useId()
  const fields = (group: "rpc" | "alerts" | "advanced") => <FieldGroup className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
    {rpcSettingsFields.filter(field => field.group === group && (!field.evmOnly || family === "evm")).map(field => {
      const inputId = `${prefix}-${field.key}`
      const effective = rpcSettingsValue(form, defaults, field.key)
      const automatic = form[field.key] === "0"
      const error = errors[field.key]
      return <Field key={field.key} data-invalid={!!error} data-disabled={disabled && editing}>
        <FieldLabel htmlFor={editing ? inputId : undefined}>{field.label}{field.unit ? `, ${field.unit}` : ""}</FieldLabel>
        {editing ? <Input id={inputId} type="number" min={0} max={field.max} step={1} value={form[field.key]} disabled={disabled} aria-invalid={!!error} aria-describedby={`${inputId}-description${error ? ` ${inputId}-error` : ""}`} onChange={event => onChange(field.key, event.target.value)} /> : null}
        <FieldDescription id={`${inputId}-description`}>
          {automatic ? `Автоматически: ${effective}` : editing ? `0 — автоматически. Будет применено: ${effective}` : `Задано: ${effective}`}{field.unit ? ` ${field.unit}` : ""}
        </FieldDescription>
        {error ? <FieldError id={`${inputId}-error`}>{error}</FieldError> : null}
      </Field>
    })}
  </FieldGroup>

  return <FieldGroup className="gap-5">
    <FieldSet className="gap-3">
      <FieldLegend>Время ожидания RPC</FieldLegend>
      <FieldDescription>Бюджет охватывает всю операцию и обращения к резервным источникам. 0 — значение по умолчанию.</FieldDescription>
      {fields("rpc")}
    </FieldSet>
    <FieldSet className="gap-3">
      <FieldLegend>Уведомления об обработке оплат</FieldLegend>
      <FieldDescription>Проблема и восстановление должны сохраняться в течение заданного времени. Отставание для восстановления должно быть меньше допустимого. Монитор перечитывает пороги при каждой проверке; они не меняют ограничения безопасности приёма оплат.</FieldDescription>
      {fields("alerts")}
    </FieldSet>
    <Collapsible>
      <CollapsibleTrigger asChild><Button type="button" variant="ghost" size="sm"><ChevronRight data-icon="inline-start" />Дополнительные настройки RPC</Button></CollapsibleTrigger>
      <CollapsibleContent className="pt-3">
        <FieldSet className="gap-3">
          <FieldLegend className="sr-only">Лимиты RPC и предзагрузки</FieldLegend>
          <FieldDescription>Сетевые лимиты действуют на каждое подключение. Квоты аккаунтов и пулов могут дополнительно ограничивать запросы. Для текущих данных должна оставаться свободная параллельность.</FieldDescription>
          {fields("advanced")}
        </FieldSet>
      </CollapsibleContent>
    </Collapsible>
  </FieldGroup>
}
