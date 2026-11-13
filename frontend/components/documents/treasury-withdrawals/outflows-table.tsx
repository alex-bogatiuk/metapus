"use client"

import { formatDateTime } from "@/lib/format"
import type { ExternalOutflow } from "@/types/treasury"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

import { AddressButton } from "./withdrawal-components"

export function OutflowsTable({
  items,
  tokenId,
  formatTokenAmount,
  onCopy,
  onSelect,
}: {
  items: ExternalOutflow[]
  tokenId: string
  formatTokenAmount: (minor: string, tokenId?: string) => string
  onCopy: (address: string) => void
  onSelect: (outflow: ExternalOutflow) => void
}) {
  return (
    <Table className="min-w-[980px]">
      <TableHeader className="sticky top-0 z-10">
        <TableRow>
          <TableHead className="h-9 px-3 py-2">Статус</TableHead>
          <TableHead className="h-9 px-3 py-2 text-right">Сумма</TableHead>
          <TableHead className="h-9 px-3 py-2">From</TableHead>
          <TableHead className="h-9 px-3 py-2">To</TableHead>
          <TableHead className="h-9 px-3 py-2">TX</TableHead>
          <TableHead className="h-9 px-3 py-2">Обнаружен</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.length === 0 ? (
          <TableRow>
            <TableCell colSpan={6} className="h-28 text-center text-muted-foreground">
              Внешние списания не найдены
            </TableCell>
          </TableRow>
        ) : (
          items.map((item) => (
            <TableRow key={item.id} className="cursor-pointer" onClick={() => onSelect(item)}>
              <TableCell className="px-3 py-2">
                <Button variant="link" className="h-auto p-0" onClick={() => onSelect(item)}>
                  <Badge variant="outline">{item.status}</Badge>
                </Button>
              </TableCell>
              <TableCell className="px-3 py-2 text-right font-mono text-xs tabular-nums">
                {formatTokenAmount(item.amount, item.tokenId || tokenId)}
              </TableCell>
              <TableCell className="px-3 py-2" onClick={(event) => event.stopPropagation()}><AddressButton address={item.fromAddress} onCopy={onCopy} /></TableCell>
              <TableCell className="px-3 py-2" onClick={(event) => event.stopPropagation()}><AddressButton address={item.toAddress} onCopy={onCopy} /></TableCell>
              <TableCell className="px-3 py-2" onClick={(event) => event.stopPropagation()}><AddressButton address={item.txHash} onCopy={onCopy} /></TableCell>
              <TableCell className="px-3 py-2 text-xs text-muted-foreground">{formatDateTime(item.detectedAt)}</TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  )
}
