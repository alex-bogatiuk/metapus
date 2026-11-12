"use client"

import { useEffect } from "react"
import useSWR from "swr"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { integrationRuntime } from "@/lib/api/integrations"
import type { IntegrationRPCKeyOptionsResponse } from "@/types/generated/http-contracts"
import type { JsonValue } from "@/lib/json-value"

type Draft = Record<string, JsonValue | undefined>

const text = (value: JsonValue | undefined) => typeof value === "string" ? value : ""

/** Accounts with credentials that lack the key; unconfigured accounts are shown elsewhere. */
export function accountsWithoutKey(options: IntegrationRPCKeyOptionsResponse, name: string): string[] {
  return options.accounts.filter(account => account.hasCredentials && !account.keys.includes(name)).map(account => account.name)
}

/**
 * The value the field should hold for the current options, or undefined to keep it.
 * A provider profile fixes the name; otherwise a single common name is preselected.
 */
export function suggestedKeyName(options: IntegrationRPCKeyOptionsResponse, current: string): string | undefined {
  if (options.required) return current === options.required ? undefined : options.required
  if (options.names.length === 1 && !options.names.includes(current)) return options.names[0]
  return undefined
}

/**
 * "Имя ключа" of an RPC connection: which named key of the selected pool account
 * the connection reads. The list comes from the pool accounts; key values never
 * reach the browser.
 */
export function RPCKeyNameField({ value, values, onChange, readOnly }: {
  value: JsonValue | undefined
  values: Draft
  onChange: (value: string) => void
  readOnly: boolean
}) {
  const current = text(value)
  const poolId = text(values.poolId)
  const draft = { networkId: text(values.networkId), role: text(values.role), url: text(values.url) }
  const { data, error, isLoading } = useSWR(
    poolId ? ["integration-rpc-key-names", poolId, draft.networkId, draft.role, draft.url] : null,
    () => integrationRuntime.rpcKeyNames(poolId, draft),
    { keepPreviousData: true },
  )
  const suggestion = data && !readOnly ? suggestedKeyName(data, current) : undefined
  useEffect(() => {
    if (suggestion !== undefined) onChange(suggestion)
  }, [suggestion, onChange])

  if (!poolId) {
    return <div className="grid gap-1">
      <Input id="credentialSlot" value="—" readOnly disabled />
      <p className="text-xs text-muted-foreground">Без пула аккаунтов ключ не используется.</p>
    </div>
  }
  if (error) return <p role="alert" className="text-xs text-destructive">Не удалось загрузить ключи аккаунтов пула</p>
  if (!data) return <Input id="credentialSlot" value={isLoading ? "Загрузка…" : current} readOnly disabled />

  const name = data.required || current
  const missing = name ? accountsWithoutKey(data, name) : []
  const warning = missing.length > 0 && <Alert variant="destructive" className="mt-2">
    <AlertDescription>
      {missing.length === 1 ? "В аккаунте" : "В аккаунтах"} {missing.map(account => `«${account}»`).join(", ")} нет ключа «{name}». Добавьте его в аккаунт, иначе запись будет отклонена.
    </AlertDescription>
  </Alert>

  if (data.required) {
    return <div className="grid gap-1">
      <Input id="credentialSlot" value={data.required} readOnly />
      <p className="text-xs text-muted-foreground">Задаётся профилем провайдера для этой сети и роли.</p>
      {warning}
    </div>
  }
  const choices = current && !data.names.includes(current) ? [...data.names, current] : data.names
  return <div className="grid gap-1">
    <Select value={current || undefined} onValueChange={onChange} disabled={readOnly || choices.length === 0}>
      <SelectTrigger id="credentialSlot" aria-label="Имя ключа">
        <SelectValue placeholder={choices.length ? "Выберите ключ…" : "Нет общих ключей"} />
      </SelectTrigger>
      <SelectContent>
        {choices.map(choice => <SelectItem key={choice} value={choice}>
          {data.names.includes(choice) ? choice : `${choice} — нет в аккаунтах пула`}
        </SelectItem>)}
      </SelectContent>
    </Select>
    <p className="text-xs text-muted-foreground">
      {data.names.length ? "Ключи, которые есть во всех аккаунтах пула с заданными ключами." : "У аккаунтов пула нет общего имени ключа. Добавьте ключ в аккаунты."}
    </p>
    {warning}
  </div>
}
