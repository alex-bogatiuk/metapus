"use client"

import { useCallback, useRef, useSyncExternalStore } from "react"
import useSWR from "swr"
import { AlertTriangle, Network, RefreshCw } from "lucide-react"

import { useTabDirty } from "@/hooks/useTabDirty"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"

import {
  commitExpandedNetworks,
  getExpandedNetworksServerSnapshot,
  getExpandedNetworksSnapshot,
  isNetworkCardExpanded,
  subscribeExpandedNetworks,
} from "./crypto-networks/types-and-helpers"
import { CryptoNetworkCard } from "./crypto-networks/crypto-network-card"

import { cryptoApi } from "@/configuration/crypto/lib/api"
export function CryptoNetworksConsole() {
  const dirtyNetworks = useRef(new Set<string>())
  const { markDirty, markClean } = useTabDirty()
  const { data, error, isLoading, isValidating, mutate } = useSWR(
    "admin-crypto-networks",
    cryptoApi.cryptoNetworks.list,
    { refreshInterval: 15_000, revalidateOnFocus: true }
  )
  const expandedById = useSyncExternalStore(
    subscribeExpandedNetworks,
    getExpandedNetworksSnapshot,
    getExpandedNetworksServerSnapshot
  )

  const handleDirtyChange = useCallback(
    (networkId: string, dirty: boolean) => {
      if (dirty) dirtyNetworks.current.add(networkId)
      else dirtyNetworks.current.delete(networkId)
      if (dirtyNetworks.current.size > 0) markDirty()
      else markClean()
    },
    [markClean, markDirty]
  )

  const setNetworkExpanded = useCallback((networkId: string, expanded: boolean) => {
    commitExpandedNetworks({ ...getExpandedNetworksSnapshot(), [networkId]: expanded })
  }, [])

  const setAllExpanded = useCallback((networkIds: string[], expanded: boolean) => {
    commitExpandedNetworks(Object.fromEntries(networkIds.map((id) => [id, expanded])))
  }, [])

  if (isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-12 w-full" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-full" />
      </div>
    )
  }
  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>Не удалось загрузить сети</AlertTitle>
        <AlertDescription>Обновите страницу или проверьте соединение с сервером.</AlertDescription>
      </Alert>
    )
  }

  const items = data?.items ?? []
  if (items.length === 0) {
    return (
      <Alert>
        <Network />
        <AlertTitle>Сети не настроены</AlertTitle>
        <AlertDescription>Добавьте блокчейн-сеть в каталог и примените миграцию тенанта.</AlertDescription>
      </Alert>
    )
  }

  const networkIds = items.map((state) => state.network.id)
  const allExpanded = items.every((state) =>
    isNetworkCardExpanded(state.network.id, expandedById, items.length)
  )
  const allCollapsed = items.every(
    (state) => !isNetworkCardExpanded(state.network.id, expandedById, items.length)
  )

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">
          Страница обновляет данные каждые 15 секунд. Воркер публикует состояние каждые 30 секунд. URL и секреты RPC не возвращаются API.
        </p>
        <div className="flex flex-wrap items-center gap-2">
          {items.length > 1 ? (
            <>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setAllExpanded(networkIds, true)}
                disabled={allExpanded}
              >
                Развернуть все
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setAllExpanded(networkIds, false)}
                disabled={allCollapsed}
              >
                Свернуть все
              </Button>
            </>
          ) : null}
          <Button variant="outline" size="sm" onClick={() => void mutate()} disabled={isValidating}>
            <RefreshCw data-icon="inline-start" className={isValidating ? "animate-spin" : undefined} />
            Обновить
          </Button>
        </div>
      </div>
      {items.map((state) => (
        <CryptoNetworkCard
          key={state.network.id}
          incoming={state}
          expanded={isNetworkCardExpanded(state.network.id, expandedById, items.length)}
          onExpandedChange={(expanded) => setNetworkExpanded(state.network.id, expanded)}
          onUpdated={mutate}
          onDirtyChange={handleDirtyChange}
        />
      ))}
    </div>
  )
}
