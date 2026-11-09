"use client"

import { useCallback, useEffect, useState } from "react"
import Link from "next/link"
import { usePortalStore } from "@/stores/usePortalStore"
import { api } from "@/lib/api"
import type { PortalWithdrawalAddress, PortalCurrencyItem } from "@/types/portal-api"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { ArrowLeft, Loader2, Plus, Shield, Trash2 } from "lucide-react"
import { toast } from "sonner"

export default function WithdrawalAddressesPage() {
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)
  const [addresses, setAddresses] = useState<PortalWithdrawalAddress[]>([])
  const [tokens, setTokens] = useState<PortalCurrencyItem[]>([])
  const [loading, setLoading] = useState(true)
  const [dialogOpen, setDialogOpen] = useState(false)

  // Add form — now network-based
  const [newNetworkId, setNewNetworkId] = useState("")
  const [newAddress, setNewAddress] = useState("")
  const [newLabel, setNewLabel] = useState("")
  const [adding, setAdding] = useState(false)

  // Derive unique networks from currencies data
  const uniqueNetworks = tokens.reduce<{ networkId: string; network: string }[]>((acc, t) => {
    if (!acc.some((n) => n.networkId === t.networkId)) {
      acc.push({ networkId: t.networkId, network: t.network })
    }
    return acc
  }, [])

  const fetchData = useCallback(async () => {
    if (!activeMerchantId) return
    setLoading(true)
    try {
      const [addrRes, tokenRes] = await Promise.all([
        api.portal.withdrawalAddresses(activeMerchantId),
        api.portal.currencies(activeMerchantId),
      ])
      setAddresses(addrRes.items)
      setTokens(tokenRes.items)
      // Pre-select first network if none selected
      if (tokenRes.items.length > 0 && !newNetworkId) {
        setNewNetworkId(tokenRes.items[0].networkId)
      }
    } catch {
      toast.error("Не удалось загрузить данные")
    } finally {
      setLoading(false)
    }
  }, [activeMerchantId, newNetworkId])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  const handleAdd = async () => {
    if (!activeMerchantId || !newNetworkId || !newAddress) return
    setAdding(true)
    try {
      await api.portal.addWithdrawalAddress(activeMerchantId, {
        networkId: newNetworkId,
        address: newAddress.trim(),
        label: newLabel.trim() || undefined,
      })
      toast.success("Адрес добавлен")
      setNewAddress("")
      setNewLabel("")
      setDialogOpen(false)
      fetchData()
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Ошибка добавления адреса"
      toast.error(msg)
    } finally {
      setAdding(false)
    }
  }

  const handleRemove = async (addressId: string) => {
    if (!activeMerchantId) return
    try {
      await api.portal.removeWithdrawalAddress(activeMerchantId, addressId)
      toast.success("Адрес удалён")
      setAddresses((prev) => prev.filter((a) => a.id !== addressId))
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Ошибка удаления адреса"
      toast.error(msg)
    }
  }

  if (!activeMerchantId) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
        <Shield className="size-10 mb-3 opacity-40" />
        <p>Выберите мерчанта</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Button variant="ghost" size="icon" asChild>
            <Link href="/portal/withdrawals">
              <ArrowLeft className="size-4" />
            </Link>
          </Button>
          <div>
            <h1 className="text-2xl font-bold tracking-tight">Адреса вывода</h1>
            <p className="text-sm text-muted-foreground">
              Управление белым списком адресов для вывода средств
            </p>
          </div>
        </div>

        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger asChild>
            <Button size="sm">
              <Plus className="mr-2 size-4" />
              Добавить адрес
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Новый адрес вывода</DialogTitle>
              <DialogDescription>
                Добавьте адрес в белый список для вывода средств. Только владелец (Owner) может добавлять адреса.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label>Сеть</Label>
                <Select value={newNetworkId} onValueChange={setNewNetworkId}>
                  <SelectTrigger>
                    <SelectValue placeholder="Выберите сеть" />
                  </SelectTrigger>
                  <SelectContent>
                    {uniqueNetworks.map((n) => (
                      <SelectItem key={n.networkId} value={n.networkId}>
                        {n.network}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>Адрес</Label>
                <Input
                  placeholder="T..."
                  value={newAddress}
                  onChange={(e) => setNewAddress(e.target.value)}
                  className="font-mono text-sm"
                />
              </div>
              <div className="space-y-2">
                <Label>
                  Метка <span className="text-muted-foreground">(необязательно)</span>
                </Label>
                <Input
                  placeholder="Hot wallet, Treasury..."
                  value={newLabel}
                  onChange={(e) => setNewLabel(e.target.value)}
                />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setDialogOpen(false)}>
                Отмена
              </Button>
              <Button onClick={handleAdd} disabled={adding || !newAddress.trim()}>
                {adding && <Loader2 className="mr-2 size-4 animate-spin" />}
                Добавить
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {/* Table */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base font-medium flex items-center gap-2">
            <Shield className="size-4" />
            Белый список
          </CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="space-y-2">
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : addresses.length === 0 ? (
            <div className="text-center py-8 text-sm text-muted-foreground">
              Нет адресов. Добавьте первый адрес для вывода средств.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Адрес</TableHead>
                  <TableHead>Сеть</TableHead>
                  <TableHead>Метка</TableHead>
                  <TableHead>Добавлен</TableHead>
                  <TableHead className="w-[80px]"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {addresses.map((addr) => (
                  <TableRow key={addr.id}>
                    <TableCell className="font-mono text-xs">
                      {addr.address.slice(0, 10)}...{addr.address.slice(-8)}
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline" className="text-xs">
                        {addr.network}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {addr.label || "—"}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground tabular-nums">
                      {new Date(addr.createdAt).toLocaleDateString("ru-RU")}
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-7 text-destructive hover:text-destructive"
                        onClick={() => handleRemove(addr.id)}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
