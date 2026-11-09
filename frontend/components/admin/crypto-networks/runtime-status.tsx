"use client"

import type { CryptoNetworkState, ResourceStrategy } from "@/types/crypto-network"
import { Badge } from "@/components/ui/badge"
import { RPCConnectionsSection } from "./rpc-connections-section"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

import {
  endpointLabels,
  isRuntimeFresh,
  stateLabels,
  stateVariants,
} from "./types-and-helpers"

export function RuntimeStatus({
  state,
  resourceStrategy,
  onRPCDirtyChange,
}: {
  state: CryptoNetworkState
  resourceStrategy: ResourceStrategy
  onRPCDirtyChange: (dirty: boolean) => void
}) {
  const resourceProviderRequired =
    state.capabilities.externalResourceMode !== "none" && resourceStrategy !== "burn_only"

  return (
    <div className="flex flex-col gap-3">
      <div>
        <p className="text-sm font-medium">Готовность инфраструктуры</p>
        <p className="text-sm text-muted-foreground">
          Обязательные RPC-роли: {state.capabilities.requiredRpcRoles.join(", ") || "нет"}.
        </p>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={state.runtime.signerReady ? "success" : "destructive"}>
          Signer: {state.runtime.signerReady ? "готов" : "недоступен"}
        </Badge>
        {state.capabilities.externalResourceMode !== "none" ? (
          <Badge variant={!resourceProviderRequired ? "outline" : state.runtime.resourceProviderReady ? "success" : "warning"}>
            Ресурсы: {!resourceProviderRequired ? "не требуются" : state.runtime.resourceProviderReady ? "готов" : "недоступен"}
          </Badge>
        ) : null}
        <Badge variant={isRuntimeFresh(state.runtime.observedAt) ? "outline" : "destructive"}>
          Данные: {isRuntimeFresh(state.runtime.observedAt) ? "актуальны" : "устарели"}
        </Badge>
      </div>
      <RPCConnectionsSection state={state} onDirtyChange={onRPCDirtyChange} />
      {(state.runtime.resourceProviders?.length ?? 0) > 0 ? <div className="overflow-hidden rounded-md border" aria-label="Провайдеры аренды ресурсов">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Роль</TableHead>
              <TableHead>Провайдер</TableHead>
              <TableHead>Состояние</TableHead>
              <TableHead className="text-right">Ошибки</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(state.runtime.resourceProviders?.length ?? 0) > 0 ? (
              (state.runtime.resourceProviders ?? []).map((endpoint) => (
                <TableRow key={`${endpoint.role}:${endpoint.providerId}`}>
                  <TableCell>{endpointLabels[endpoint.role] ?? endpoint.role}</TableCell>
                  <TableCell className="font-mono text-xs">{endpoint.providerId}</TableCell>
                  <TableCell><Badge variant={stateVariants[endpoint.state] ?? "outline"}>{stateLabels[endpoint.state] ?? "Недоступен"}</Badge></TableCell>
                  <TableCell className="text-right tabular-nums">{endpoint.failures}</TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={4} className="text-center text-muted-foreground">Воркер ещё не опубликовал статус RPC</TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div> : null}
    </div>
  )
}
