"use client"

import { useEffect } from "react"
import useSWR from "swr"

import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { cryptoApi } from "@/configuration/crypto/lib/api"
import type { JsonValue } from "@/lib/json-value"

type Draft = Record<string, JsonValue | undefined>
type EnumOption = { value: string; label: string }

const text = (value: JsonValue | undefined) => typeof value === "string" ? value : ""

/** Roles a network family supports; mirrors rpcconfig.RolesForFamily, which the server enforces. */
export function rolesForFamily(family: string): string[] {
  switch (family) {
    case "evm": return ["rpc"]
    case "tron": return ["fullnode", "solidity", "indexer"]
    default: return []
  }
}

/**
 * The role the field should hold for the network, or undefined to keep it.
 * A single allowed role is filled in; a role the network lacks is cleared.
 */
export function suggestedRole(allowed: readonly string[], current: string): string | undefined {
  if (!allowed.length || allowed.includes(current)) return undefined
  if (allowed.length === 1) return allowed[0]
  return current ? "" : undefined
}

/** "Назначение подключения": only the purposes the selected network has. */
export function RPCRoleField({ value, values, options, onChange, readOnly }: {
  value: JsonValue | undefined
  values: Draft
  options: readonly EnumOption[]
  onChange: (value: string) => void
  readOnly: boolean
}) {
  const current = text(value)
  const networkId = text(values.networkId)
  const { data: network } = useSWR(networkId ? ["blockchain-network", networkId] : null, () => cryptoApi.blockchainNetworks.get(networkId))
  const allowed = network ? rolesForFamily(network.chainFamily) : []
  const choices = allowed.length ? options.filter(option => allowed.includes(option.value)) : options
  const suggestion = readOnly ? undefined : suggestedRole(allowed, current)
  useEffect(() => {
    if (suggestion !== undefined) onChange(suggestion)
  }, [suggestion, onChange])

  return <div className="grid gap-1">
    <Select value={current || undefined} onValueChange={onChange} disabled={readOnly || choices.length === 1}>
      <SelectTrigger id="role" aria-label="Назначение подключения">
        <SelectValue placeholder="Выберите назначение…" />
      </SelectTrigger>
      <SelectContent>
        {choices.map(option => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
      </SelectContent>
    </Select>
    {!readOnly && <p className="text-xs text-muted-foreground">
      {!networkId ? "Сначала выберите сеть." : choices.length === 1 ? "У этой сети одно назначение подключения." : "Какие запросы пойдут через это подключение."}
    </p>}
  </div>
}

/** "Кто обслуживает RPC": the operator, needed only for an address no provider profile knows. */
export function RPCTrustGroupField({ value, onChange, readOnly }: {
  value: JsonValue | undefined
  onChange: (value: string) => void
  readOnly: boolean
}) {
  const current = text(value)
  return <div className="grid gap-1">
    <Input id="trustGroup" value={current} readOnly={readOnly} placeholder="Определим по адресу RPC" onChange={event => onChange(event.target.value)} />
    {!readOnly && <p className="text-xs text-muted-foreground">
      Для известного провайдера оператор определяется по адресу при записи. Укажите его только для собственного узла или прокси. Адреса одного оператора считаются одним источником для сверки блоков.
    </p>}
  </div>
}

/** "Код источника": assigned by the server once and kept, because block evidence names it. */
export function RPCSourceCodeField({ value }: { value: JsonValue | undefined }) {
  const current = text(value)
  return <div className="grid gap-1">
    <Input id="providerId" value={current || "Назначим при записи"} readOnly disabled={!current} />
    <p className="text-xs text-muted-foreground">Под этим кодом подключение записано в результатах сверки. Он не меняется.</p>
  </div>
}
