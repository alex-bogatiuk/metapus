"use client"

import { CheckCircle2, Loader2, RefreshCw, XCircle } from "lucide-react"

import { formatDateTime } from "@/lib/format"
import type { TreasuryWithdrawal } from "@/types/treasury"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

import { AddressButton, StatusBadge } from "./withdrawal-components"

export type SigningJobStatus = { status: string; attempts: number; lastError: string }

export function WithdrawalsTable({
  items,
  loading,
  actionID,
  signingJobs,
  signingJobLoadingID,
  formatTokenAmount,
  onApprove,
  onReject,
  onLoadSigningJob,
  onRetrySigning,
  onCopy,
  approveLabel,
  rejectLabel,
}: {
  items: TreasuryWithdrawal[]
  loading: boolean
  actionID: string | null
  signingJobs: Record<string, SigningJobStatus>
  signingJobLoadingID: string | null
  formatTokenAmount: (minor: string, tokenId?: string) => string
  onApprove: (id: string) => void
  onReject: (id: string) => void
  onLoadSigningJob: (id: string) => void
  onRetrySigning: (id: string) => void
  onCopy: (address: string) => void
  approveLabel: string
  rejectLabel: string
}) {
  return (
    <Table className="min-w-[980px]">
      <TableHeader className="sticky top-0 z-10 bg-muted/80 backdrop-blur-sm">
        <TableRow>
          <TableHead className="h-9 px-3 py-2">Номер</TableHead>
          <TableHead className="h-9 px-3 py-2">Статус</TableHead>
          <TableHead className="h-9 px-3 py-2 text-right">Сумма</TableHead>
          <TableHead className="h-9 px-3 py-2">Адрес</TableHead>
          <TableHead className="h-9 px-3 py-2">TX</TableHead>
          <TableHead className="h-9 px-3 py-2">Дата</TableHead>
          <TableHead className="h-9 px-3 py-2 text-right">Действия</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          Array.from({ length: 6 }).map((_, i) => (
            <TableRow key={i}>
              {Array.from({ length: 7 }).map((__, j) => (
                <TableCell key={j} className="px-3 py-2">
                  <Skeleton className="h-4 w-full" />
                </TableCell>
              ))}
            </TableRow>
          ))
        ) : items.length === 0 ? (
          <TableRow>
            <TableCell colSpan={7} className="h-28 text-center text-muted-foreground">
              Нет документов
            </TableCell>
          </TableRow>
        ) : (
          items.map((item) => (
            <TableRow key={item.id}>
              <TableCell className="px-3 py-2 font-mono text-xs">{item.number}</TableCell>
              <TableCell className="px-3 py-2">
                <StatusBadge status={item.status} />
                {item.status === "broadcast" && (
                  <div className="mt-1 max-w-60 text-xs text-muted-foreground">Средства зарезервированы до подтверждения сети</div>
                )}
                {signingJobs[item.id]?.status === "needs_review" && (
                  <div className="mt-1 max-w-60 text-xs text-destructive" title={signingJobs[item.id].lastError}>
                    Подпись требует проверки после {signingJobs[item.id].attempts} попыток
                    {signingJobs[item.id].lastError && `: ${signingJobs[item.id].lastError}`}
                  </div>
                )}
              </TableCell>
              <TableCell className="px-3 py-2 text-right font-mono text-xs tabular-nums">
                {formatTokenAmount(item.amount, item.tokenId)}
              </TableCell>
              <TableCell className="px-3 py-2">
                <AddressButton address={item.destAddress} onCopy={onCopy} />
              </TableCell>
              <TableCell className="px-3 py-2">
                <AddressButton address={item.txHash} onCopy={onCopy} />
              </TableCell>
              <TableCell className="px-3 py-2 text-xs text-muted-foreground">{formatDateTime(item.date)}</TableCell>
              <TableCell className="px-3 py-2 text-right">
                {item.status === "pending_approval" || item.status === "pending_signature" ? (
                  <div className="flex justify-end gap-2">
                    <Button size="sm" variant="outline" onClick={() => onReject(item.id)} disabled={actionID === item.id}>
                      <XCircle />
                      {rejectLabel}
                    </Button>
                    {item.status === "pending_approval" && (
                      <Button size="sm" onClick={() => onApprove(item.id)} disabled={actionID === item.id}>
                        {actionID === item.id ? <Loader2 className="animate-spin" /> : <CheckCircle2 />}
                        {approveLabel}
                      </Button>
                    )}
                    {item.status === "pending_signature" && signingJobs[item.id]?.status !== "needs_review" && (
                      <Button size="sm" variant="outline" onClick={() => onLoadSigningJob(item.id)}
                        disabled={signingJobLoadingID === item.id || actionID === item.id}>
                        {signingJobLoadingID === item.id ? <Loader2 className="animate-spin" /> : <RefreshCw />}
                        Проверить подпись
                      </Button>
                    )}
                    {item.status === "pending_signature" && signingJobs[item.id]?.status === "needs_review" && (
                      <Button size="sm" onClick={() => onRetrySigning(item.id)} disabled={actionID === item.id}>
                        {actionID === item.id ? <Loader2 className="animate-spin" /> : <RefreshCw />}
                        Повторить подпись
                      </Button>
                    )}
                  </div>
                ) : item.approvedAt ? (
                  <span className="text-xs text-muted-foreground">{formatDateTime(item.approvedAt)}</span>
                ) : (
                  <span className="text-xs text-muted-foreground">—</span>
                )}
              </TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  )
}
