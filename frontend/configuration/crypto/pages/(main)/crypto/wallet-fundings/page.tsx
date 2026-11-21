"use client"

import { useState } from "react"
import Link from "next/link"
import useSWR from "swr"

import { formatDateTime, formatMinorUnits } from "@/lib/format"
import { usePermissions } from "@/hooks/use-permissions"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

import { cryptoApi } from "@/configuration/crypto/lib/api"
const pageSize = 50

function short(value: string) {
  return value.length > 14 ? `${value.slice(0, 8)}…${value.slice(-4)}` : value
}

/**
 * Recorded external fundings of platform wallets. Each one added custody
 * directly; a funding recorded after the intake grace is an unidentified
 * receipt taken as platform funding instead.
 */
export default function WalletFundingsPage() {
  const permissions = usePermissions()
  const canRead = permissions.has("crypto_receipt:read")
  const [offset, setOffset] = useState(0)
  const { data, error, isLoading, mutate } = useSWR(canRead ? ["wallet-fundings", offset] : null, () => cryptoApi.unidentifiedReceipts.walletFundings(offset))

  if (!canRead) return <p className="p-6 text-muted-foreground">Нет доступа к пополнениям кошельков.</p>
  const items = data?.items ?? []
  return <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto p-6 [&>*]:shrink-0">
    <div className="flex flex-col gap-1">
      <h1 className="text-2xl font-semibold">Пополнения кошельков</h1>
      <p className="text-sm text-muted-foreground">
        Внешние переводы на кошельки платформы, записанные командой учёта пополнений. Каждое даёт приход в регистр остатков
        на кошельке и не проходит через невыясненные. Пополнение, записанное позже 30 минут, оформляется в журнале
        невыясненных как <Link className="text-primary hover:underline" href="/documents/crypto-platform-funding">пополнение платформы</Link>.
      </p>
    </div>
    <Card>
      <CardHeader>
        <CardTitle>Журнал пополнений</CardTitle>
        <CardDescription>Новые сверху. Движение одно: приход на кошелёк в регистре остатков.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex justify-end"><Button variant="outline" onClick={() => mutate()}>Обновить</Button></div>
        {error && <p role="alert" className="text-sm text-destructive">Не удалось загрузить пополнения.</p>}
        {isLoading ? <p className="text-sm text-muted-foreground">Загрузка…</p> : <Table>
          <TableHeader><TableRow>
            <TableHead>Записано</TableHead><TableHead>Сеть / кошелёк</TableHead><TableHead>Отправитель / транзакция</TableHead>
            <TableHead>Сумма</TableHead><TableHead className="text-right">Блок</TableHead>
          </TableRow></TableHeader>
          <TableBody>
            {items.map((item) => <TableRow key={item.id}>
              <TableCell>
                <div>{formatDateTime(item.recordedAt)}</div>
                <div className="text-xs text-muted-foreground">в сети {formatDateTime(item.blockTimestamp)}</div>
              </TableCell>
              <TableCell>
                <div>{item.networkName}</div>
                <Link className="text-xs text-primary hover:underline" href={`/catalogs/wallets/${item.walletId}`}>{item.walletCode}</Link>
              </TableCell>
              <TableCell>
                <div className="font-mono text-xs" title={item.fromAddress}>{short(item.fromAddress)}</div>
                <div className="font-mono text-xs text-muted-foreground" title={item.txHash}>{short(item.txHash)}</div>
              </TableCell>
              <TableCell className="font-mono">{formatMinorUnits(item.amount, item.decimalPlaces)} {item.tokenSymbol}</TableCell>
              <TableCell className="text-right tabular-nums">
                <div>{item.blockNumber.toLocaleString("ru-RU")}</div>
                <div className="text-xs text-muted-foreground">{item.confirmations} подтв.</div>
              </TableCell>
            </TableRow>)}
            {items.length === 0 && <TableRow><TableCell colSpan={5} className="text-center text-muted-foreground">Пополнений нет</TableCell></TableRow>}
          </TableBody>
        </Table>}
        <div className="flex items-center justify-end gap-2 text-sm text-muted-foreground">
          <span>{data?.total ?? 0} всего</span>
          <Button variant="outline" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - pageSize))}>Назад</Button>
          <Button variant="outline" size="sm" disabled={offset + pageSize >= (data?.total ?? 0)} onClick={() => setOffset(offset + pageSize)}>Далее</Button>
        </div>
      </CardContent>
    </Card>
  </div>
}
