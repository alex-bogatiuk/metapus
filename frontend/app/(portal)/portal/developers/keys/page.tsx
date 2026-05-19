"use client"

import { useCallback, useEffect, useState } from "react"
import { usePortalStore } from "@/stores/usePortalStore"
import { api } from "@/lib/api"
import type { MerchantAPIKeyListItem, MerchantAPIKeyResponse, APIKeyScope } from "@/types/merchant-api"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Skeleton } from "@/components/ui/skeleton"
import { Plus, Copy, Check, Trash2, KeyRound, ShieldAlert } from "lucide-react"
import { toast } from "sonner"

const AVAILABLE_SCOPES: { value: APIKeyScope; label: string }[] = [
  { value: "invoice:create", label: "Создание инвойсов" },
  { value: "invoice:read", label: "Чтение инвойсов" },
  { value: "address:create", label: "Создание кошельков" },
]

export default function APIKeysPage() {
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)
  const [keys, setKeys] = useState<MerchantAPIKeyListItem[]>([])
  const [loading, setLoading] = useState(true)
  const [createOpen, setCreateOpen] = useState(false)
  const [newKeyName, setNewKeyName] = useState("")
  const [newKeyScopes, setNewKeyScopes] = useState<APIKeyScope[]>([])
  const [creating, setCreating] = useState(false)
  const [plaintext, setPlaintext] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const fetchKeys = useCallback(async () => {
    if (!activeMerchantId) return
    setLoading(true)
    try {
      const data = await api.portal.apiKeys.list(activeMerchantId)
      setKeys(data.items)
    } catch {
      toast.error("Не удалось загрузить API-ключи")
    } finally {
      setLoading(false)
    }
  }, [activeMerchantId])

  useEffect(() => { fetchKeys() }, [fetchKeys])

  const handleCreate = async () => {
    if (!activeMerchantId || !newKeyName.trim()) return
    setCreating(true)
    try {
      const resp = await api.portal.apiKeys.create(activeMerchantId, {
        name: newKeyName.trim(),
        scopes: newKeyScopes.length > 0 ? newKeyScopes : undefined,
      })
      setPlaintext((resp as MerchantAPIKeyResponse).plaintext ?? null)
      setCreateOpen(false)
      setNewKeyName("")
      setNewKeyScopes([])
      await fetchKeys()
      toast.success("API-ключ создан")
    } catch {
      toast.error("Ошибка при создании ключа")
    } finally {
      setCreating(false)
    }
  }

  const handleRevoke = async (keyId: string) => {
    if (!activeMerchantId) return
    try {
      await api.portal.apiKeys.revoke(activeMerchantId, keyId)
      await fetchKeys()
      toast.success("Ключ отозван")
    } catch {
      toast.error("Ошибка при отзыве ключа")
    }
  }

  const handleCopy = async (text: string) => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  if (!activeMerchantId) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
        <KeyRound className="size-10 mb-3 opacity-40" />
        <p>Выберите мерчанта для управления API-ключами</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">API-ключи</h1>
          <p className="text-sm text-muted-foreground">
            Управляйте ключами доступа к Merchant API
          </p>
        </div>
        <Dialog open={createOpen} onOpenChange={setCreateOpen}>
          <DialogTrigger asChild>
            <Button size="sm">
              <Plus className="size-4 mr-1.5" />
              Создать ключ
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Новый API-ключ</DialogTitle>
              <DialogDescription>
                Ключ будет показан только один раз. Сохраните его в безопасном месте.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-2">
              <div className="space-y-2">
                <Label htmlFor="key-name">Название</Label>
                <Input
                  id="key-name"
                  placeholder="Production API"
                  value={newKeyName}
                  onChange={(e) => setNewKeyName(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label>Разрешения (scopes)</Label>
                {AVAILABLE_SCOPES.map((scope) => (
                  <div key={scope.value} className="flex items-center gap-2">
                    <Checkbox
                      id={`scope-${scope.value}`}
                      checked={newKeyScopes.includes(scope.value)}
                      onCheckedChange={(checked) => {
                        setNewKeyScopes((prev) =>
                          checked
                            ? [...prev, scope.value]
                            : prev.filter((s) => s !== scope.value)
                        )
                      }}
                    />
                    <Label htmlFor={`scope-${scope.value}`} className="text-sm font-normal cursor-pointer">
                      {scope.label}
                    </Label>
                  </div>
                ))}
              </div>
            </div>
            <DialogFooter>
              <Button onClick={handleCreate} disabled={creating || !newKeyName.trim()}>
                {creating ? "Создание..." : "Создать"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {/* Plaintext key reveal dialog */}
      {plaintext && (
        <AlertDialog open={!!plaintext} onOpenChange={() => setPlaintext(null)}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle className="flex items-center gap-2">
                <ShieldAlert className="size-5 text-amber-500" />
                Сохраните ваш API-ключ
              </AlertDialogTitle>
              <AlertDialogDescription>
                Это единственный раз, когда ключ показан в открытом виде. Скопируйте его сейчас.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <div className="bg-muted rounded-md p-3 font-mono text-sm break-all select-all">
              {plaintext}
            </div>
            <AlertDialogFooter>
              <Button
                variant="outline"
                onClick={() => handleCopy(plaintext)}
                className="gap-1.5"
              >
                {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
                {copied ? "Скопировано" : "Копировать"}
              </Button>
              <AlertDialogAction>Готово</AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Активные ключи</CardTitle>
          <CardDescription>
            Ключи используются для аутентификации запросов к Merchant API
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="space-y-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          ) : keys.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              <KeyRound className="size-8 mx-auto mb-2 opacity-30" />
              <p className="text-sm">Нет созданных API-ключей</p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Название</TableHead>
                  <TableHead>Префикс</TableHead>
                  <TableHead>Scopes</TableHead>
                  <TableHead>Создан</TableHead>
                  <TableHead className="w-[60px]" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {keys.map((key) => (
                  <TableRow key={key.id}>
                    <TableCell className="font-medium">{key.name}</TableCell>
                    <TableCell>
                      <code className="text-xs bg-muted px-1.5 py-0.5 rounded">
                        {key.keyPrefix}...
                      </code>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {key.scopes?.map((s) => (
                          <Badge key={s} variant="secondary" className="text-[10px]">
                            {s}
                          </Badge>
                        )) ?? (
                          <span className="text-xs text-muted-foreground">all</span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {new Date(key.createdAt).toLocaleDateString("ru-RU")}
                    </TableCell>
                    <TableCell>
                      <AlertDialog>
                        <AlertDialogTrigger asChild>
                          <Button variant="ghost" size="icon" className="size-7 text-muted-foreground hover:text-destructive">
                            <Trash2 className="size-3.5" />
                          </Button>
                        </AlertDialogTrigger>
                        <AlertDialogContent>
                          <AlertDialogHeader>
                            <AlertDialogTitle>Отозвать ключ?</AlertDialogTitle>
                            <AlertDialogDescription>
                              Ключ &quot;{key.name}&quot; ({key.keyPrefix}...) будет деактивирован.
                              Все запросы с этим ключом перестанут работать.
                            </AlertDialogDescription>
                          </AlertDialogHeader>
                          <AlertDialogFooter>
                            <AlertDialogCancel>Отмена</AlertDialogCancel>
                            <AlertDialogAction
                              onClick={() => handleRevoke(key.id)}
                              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                            >
                              Отозвать
                            </AlertDialogAction>
                          </AlertDialogFooter>
                        </AlertDialogContent>
                      </AlertDialog>
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
