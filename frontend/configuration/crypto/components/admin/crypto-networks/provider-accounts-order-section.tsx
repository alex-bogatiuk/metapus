"use client"

import { useMemo, useState } from "react"
import useSWR from "swr"
import {
  ArrowDown,
  ArrowUp,
  ExternalLink,
  KeyRound,
  Plus,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"

import type { CryptoProviderAccount } from "@/configuration/crypto/types/crypto-network"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { FieldLabel } from "@/components/ui/field"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

import { FieldHelp } from "./types-and-helpers"
import { ProviderAccountCredentialsDialog } from "./provider-account-credentials-dialog"

import { cryptoApi } from "@/configuration/crypto/lib/api"
export interface ProviderAccountsOrderSectionProps {
  networkId: string
  accountIds: string[]
  onChange: (accountIds: string[]) => void
  error?: string
}

export function ProviderAccountsOrderSection({
  networkId,
  accountIds,
  onChange,
  error,
}: ProviderAccountsOrderSectionProps) {
  const { data, mutate } = useSWR(
    `/catalog/crypto-provider-accounts?networkId=${networkId}`,
    () => cryptoApi.cryptoProviderAccounts.list({ limit: 50 })
  )

  const [dialogAccount, setDialogAccount] = useState<CryptoProviderAccount | null | undefined>(undefined)

  const accountsMap = useMemo(() => {
    const map = new Map<string, CryptoProviderAccount>()
    for (const item of data?.items ?? []) {
      if (item.networkId === networkId) {
        map.set(item.id, item)
      }
    }
    return map
  }, [data?.items, networkId])

  const moveUp = (index: number) => {
    if (index <= 0) return
    const next = [...accountIds]
    const temp = next[index - 1]
    next[index - 1] = next[index]
    next[index] = temp
    onChange(next)
  }

  const moveDown = (index: number) => {
    if (index >= accountIds.length - 1) return
    const next = [...accountIds]
    const temp = next[index + 1]
    next[index + 1] = next[index]
    next[index] = temp
    onChange(next)
  }

  const remove = (idToRemove: string) => {
    onChange(accountIds.filter((id) => id !== idToRemove))
  }

  const availableToAdd = useMemo(() => {
    return (data?.items ?? []).filter(
      (acc) => acc.networkId === networkId && !accountIds.includes(acc.id) && acc.isActive
    )
  }, [data?.items, networkId, accountIds])

  const handleAccountSaved = (saved: CryptoProviderAccount, isNewAcc: boolean) => {
    mutate()
    if (isNewAcc) {
      onChange([...accountIds, saved.id])
    }
    setDialogAccount(undefined)
    toast.success(isNewAcc ? "Аккаунт создан и добавлен в цепочку" : "Учетные данные обновлены")
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-1.5">
          <FieldLabel>Порядок аккаунтов провайдеров</FieldLabel>
          <FieldHelp title="Приоритет аккаунтов крипто-провайдеров">
            <p>
              Новый заказ аренды энергии направляется к первому доступному аккаунту (#1).
            </p>
            <p className="mt-1">
              При сбое или исчерпании лимита circuit breaker автоматически переключает аренду на следующий аккаунт в списке.
            </p>
          </FieldHelp>
        </div>
        <a
          href="/catalogs/crypto-provider-accounts"
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground hover:underline"
        >
          <ExternalLink className="size-3" />
          Справочник аккаунтов
        </a>
      </div>

      {accountIds.length === 0 ? (
        <div className="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
          Аккаунты не добавлены в цепочку. Создайте аккаунт или выберите существующий ниже.
        </div>
      ) : (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-28">Приоритет</TableHead>
                <TableHead>Аккаунт</TableHead>
                <TableHead>Провайдер</TableHead>
                <TableHead>Ключи API</TableHead>
                <TableHead className="text-right">Действия</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {accountIds.map((id, index) => {
                const acc = accountsMap.get(id)
                return (
                  <TableRow key={id}>
                    <TableCell>
                      <Badge variant={index === 0 ? "default" : "secondary"}>
                        #{index + 1} {index === 0 ? "Основной" : "Резерв"}
                      </Badge>
                    </TableCell>
                    <TableCell className="font-medium">
                      {acc ? acc.name : `${id.slice(0, 8)}…`}
                      {acc?.externalAccountRef ? (
                        <span className="ml-1.5 text-xs text-muted-foreground">({acc.externalAccountRef})</span>
                      ) : null}
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline">{acc ? acc.provider.toUpperCase() : "..."}</Badge>
                    </TableCell>
                    <TableCell>
                      <button
                        type="button"
                        onClick={() => setDialogAccount(acc)}
                        className="inline-flex items-center gap-1.5 hover:opacity-80 transition-opacity"
                        title="Настроить ключи API"
                      >
                        {acc?.hasCredentials ? (
                          <Badge variant="outline" className="text-emerald-600 dark:text-emerald-400 cursor-pointer gap-1">
                            <KeyRound className="size-3" />
                            {acc.maskedApiKey || "Настроены"}
                          </Badge>
                        ) : (
                          <Badge variant="destructive" className="cursor-pointer gap-1">
                            <KeyRound className="size-3" />
                            Нет ключей
                          </Badge>
                        )}
                      </button>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="size-7 text-muted-foreground hover:text-foreground"
                          onClick={() => setDialogAccount(acc)}
                          title="Настроить ключи API"
                        >
                          <KeyRound className="size-3.5" />
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="size-7"
                          onClick={() => moveUp(index)}
                          disabled={index === 0}
                          title="Поднять приоритет"
                        >
                          <ArrowUp className="size-3.5" />
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="size-7"
                          onClick={() => moveDown(index)}
                          disabled={index === accountIds.length - 1}
                          title="Опустить приоритет"
                        >
                          <ArrowDown className="size-3.5" />
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="size-7 text-destructive hover:bg-destructive/10"
                          onClick={() => remove(id)}
                          title="Удалить из цепочки"
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2">
        {availableToAdd.length > 0 ? (
          <Select
            value=""
            onValueChange={(selectedId) => {
              if (selectedId && !accountIds.includes(selectedId)) {
                onChange([...accountIds, selectedId])
              }
            }}
          >
            <SelectTrigger className="w-full sm:w-80">
              <SelectValue placeholder="+ Добавить существующий в цепочку..." />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {availableToAdd.map((acc) => (
                  <SelectItem key={acc.id} value={acc.id}>
                    {acc.name} ({acc.provider.toUpperCase()})
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        ) : null}

        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => setDialogAccount(null)}
          className="gap-1.5 text-xs h-9"
        >
          <Plus className="size-3.5" />
          Создать аккаунт провайдера
        </Button>
      </div>

      <ProviderAccountCredentialsDialog
        open={dialogAccount !== undefined}
        onOpenChange={(isOpen) => {
          if (!isOpen) setDialogAccount(undefined)
        }}
        networkId={networkId}
        account={dialogAccount}
        onSaved={handleAccountSaved}
      />

      {error ? <p className="text-xs font-medium text-destructive">{error}</p> : null}
    </div>
  )
}
