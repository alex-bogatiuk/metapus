"use client"

import type { IntegrationKindResponse, IntegrationProviderResponse } from "@/types/generated/http-contracts"
import { Field, FieldLabel, FieldDescription, FieldError } from "@/components/ui/field"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Input } from "@/components/ui/input"

interface IntegrationProviderFieldsProps {
  prefix: string
  kinds: readonly IntegrationKindResponse[]
  providers: readonly IntegrationProviderResponse[]
  kind: string
  provider: string
  locked: boolean
  error?: string
  kindError?: string
  onKindChange: (kind: string) => void
  onProviderChange: (provider: string) => void
}

export function IntegrationProviderFields({ prefix, kinds, providers, kind, provider, locked, error, kindError, onKindChange, onProviderChange }: IntegrationProviderFieldsProps) {
  const selected = providers.find(item => item.id === provider)
  const availableKinds = kinds.filter(item => providers.some(p => p.canCreate && p.kinds.includes(item.id)))
  const availableProviders = providers.filter(item => item.canCreate && item.kinds.includes(kind))
  const description = locked
    ? kinds.filter(item => selected?.kinds.includes(item.id)).map(item => item.label).join(", ")
    : kinds.find(item => item.id === kind)?.description
  return <>
    <Field className="gap-2 md:col-span-2" data-field="kind" data-invalid={!!kindError}>
      <FieldLabel htmlFor={`${prefix}-kind`}>Назначение интеграции{!locked && <span aria-hidden="true"> *</span>}</FieldLabel>
      {locked ? <Input id={`${prefix}-kind`} readOnly value={description || "Неизвестное назначение"} /> :
        <ToggleGroup id={`${prefix}-kind`} type="single" variant="outline" value={kind} onValueChange={value => { if (value) onKindChange(value) }} aria-label="Назначение интеграции" aria-invalid={!!kindError} aria-describedby={`${prefix}-kind-help`} className="flex-wrap justify-start">
          {availableKinds.map(item => <ToggleGroupItem key={item.id} value={item.id}>{item.label}</ToggleGroupItem>)}
        </ToggleGroup>}
      <FieldDescription id={`${prefix}-kind-help`}>{locked ? "Назначения определяются возможностями провайдера." : description || "Выберите, для чего будет использоваться аккаунт."}</FieldDescription>
      {kindError && <FieldError>{kindError}</FieldError>}
    </Field>
    <Field className="gap-2" data-field="provider" data-invalid={!!error}>
      <FieldLabel htmlFor={`${prefix}-provider`}>Провайдер{!locked && <span aria-hidden="true"> *</span>}</FieldLabel>
      <Select value={provider} disabled={locked || !kind} onValueChange={onProviderChange}>
        <SelectTrigger id={`${prefix}-provider`} aria-label="Провайдер" aria-invalid={!!error} aria-describedby={error ? `${prefix}-provider-error` : `${prefix}-provider-help`}><SelectValue placeholder={kind ? "Выберите провайдера" : "Сначала выберите назначение"} /></SelectTrigger>
        <SelectContent><SelectGroup>{(locked && selected ? [selected] : availableProviders).map(item => <SelectItem key={item.id} value={item.id}>{item.label}</SelectItem>)}</SelectGroup></SelectContent>
      </Select>
      <FieldDescription id={`${prefix}-provider-help`}>{locked ? "Для другого провайдера создайте отдельную запись." : selected?.description || "Показаны провайдеры выбранного назначения."}</FieldDescription>
      {error && <FieldError id={`${prefix}-provider-error`}>{error}</FieldError>}
      {locked && selected && !selected.canCreate && <FieldDescription>{selected.description}</FieldDescription>}
    </Field>
  </>
}
