"use client"

import type { TreasuryAddress } from "@/types/treasury"
import { Badge } from "@/components/ui/badge"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

import { AddressButton } from "./withdrawal-components"

export function AllowlistTable({
  items,
  onCopy,
}: {
  items: TreasuryAddress[]
  onCopy: (address: string) => void
}) {
  return (
    <Table className="min-w-[720px]">
      <TableHeader className="sticky top-0 z-10 bg-muted/80 backdrop-blur-sm">
        <TableRow>
          <TableHead className="h-9 px-3 py-2">Метка</TableHead>
          <TableHead className="h-9 px-3 py-2">Адрес</TableHead>
          <TableHead className="h-9 px-3 py-2">Сеть</TableHead>
          <TableHead className="h-9 px-3 py-2">Статус</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.length === 0 ? (
          <TableRow>
            <TableCell colSpan={4} className="h-28 text-center text-muted-foreground">
              Нет разрешённых адресов
            </TableCell>
          </TableRow>
        ) : (
          items.map((address) => (
            <TableRow key={address.id}>
              <TableCell className="px-3 py-2 text-sm">{address.label || "—"}</TableCell>
              <TableCell className="px-3 py-2">
                <AddressButton address={address.address} onCopy={onCopy} />
              </TableCell>
              <TableCell className="px-3 py-2 font-mono text-xs text-muted-foreground">{address.networkId}</TableCell>
              <TableCell className="px-3 py-2">
                <Badge variant={address.isActive ? "secondary" : "outline"}>
                  {address.isActive ? "Активен" : "Отключён"}
                </Badge>
              </TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  )
}
