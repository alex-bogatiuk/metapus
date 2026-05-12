"use client"

import { useState, useEffect, useCallback } from "react"
import { api } from "@/lib/api"
import type {
  MerchantAPIKeyListItem,
  MerchantAPIKeyResponse,
  CreateMerchantAPIKeyRequest,
  APIKeyScope,
  API_KEY_SCOPES,
} from "@/types/merchant-api"
// Re-import constant (value, not just type)
import { API_KEY_SCOPES as SCOPE_OPTIONS } from "@/types/merchant-api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
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
} from "@/components/ui/alert-dialog"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Key,
  Plus,
  Trash2,
  Copy,
  Check,
  RefreshCw,
  ShieldOff,
  Clock,
} from "lucide-react"
import { toast } from "sonner"
import { format } from "date-fns"
import { ru } from "date-fns/locale"

interface MerchantAPIKeysTabProps {
  merchantId: string
  /** true when the parent entity hasn't been saved yet (hide until saved) */
  isNew?: boolean
}

export function MerchantAPIKeysTab({ merchantId, isNew }: MerchantAPIKeysTabProps) {
  const [keys, setKeys] = useState<MerchantAPIKeyListItem[]>([])
  const [loading, setLoading] = useState(false)
  const [showCreate, setShowCreate] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<MerchantAPIKeyListItem | null>(null)
  const [newKey, setNewKey] = useState<MerchantAPIKeyResponse | null>(null)
  const [copied, setCopied] = useState(false)

  const loadKeys = useCallback(async () => {
    if (!merchantId || isNew) return
    setLoading(true)
    try {
      const items = await api.merchantApiKeys.list(merchantId)
      setKeys(items)
    } catch {
      toast.error("Не удалось загрузить API ключи")
    } finally {
      setLoading(false)
    }
  }, [merchantId, isNew])

  useEffect(() => {
    loadKeys()
  }, [loadKeys])

  const handleRevoke = async () => {
    if (!revokeTarget) return
    try {
      await api.merchantApiKeys.revoke(merchantId, revokeTarget.id)
      toast.success(`Ключ «${revokeTarget.name}» отозван`)
      setRevokeTarget(null)
      await loadKeys()
    } catch {
      toast.error("Не удалось отозвать ключ")
    }
  }

  const handleCopy = async (text: string) => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  if (isNew) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-center text-muted-foreground gap-3">
        <Key className="h-8 w-8 opacity-40" />
        <p className="text-sm">Сохраните мерчанта, чтобы управлять API-ключами</p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm font-medium">API-ключи мерчанта</p>
          <p className="text-xs text-muted-foreground mt-0.5">
            Ключи используются для создания инвойсов через Merchant API
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" onClick={loadKeys} disabled={loading}>
            <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
          </Button>
          <Button size="sm" onClick={() => setShowCreate(true)}>
            <Plus className="mr-1.5 h-3.5 w-3.5" />
            Создать ключ
          </Button>
        </div>
      </div>

      <Separator />

      {/* Key List */}
      {keys.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-10 text-center text-muted-foreground gap-3">
          <Key className="h-7 w-7 opacity-40" />
          <p className="text-sm">Нет активных API-ключей</p>
        </div>
      ) : (
        <div className="space-y-2">
          {keys.map((key) => (
            <APIKeyCard
              key={key.id}
              apiKey={key}
              onRevoke={() => setRevokeTarget(key)}
            />
          ))}
        </div>
      )}

      {/* Create Key Dialog */}
      <CreateAPIKeyDialog
        open={showCreate}
        merchantId={merchantId}
        onClose={() => setShowCreate(false)}
        onCreated={(created) => {
          setNewKey(created)
          setShowCreate(false)
          loadKeys()
        }}
      />

      {/* Show Plaintext Dialog */}
      {newKey?.plaintext && (
        <Dialog open onOpenChange={() => setNewKey(null)}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <Key className="h-4 w-4 text-primary" />
                Ключ создан
              </DialogTitle>
              <DialogDescription>
                Скопируйте ключ сейчас — он больше не будет показан.
              </DialogDescription>
            </DialogHeader>
            <div className="rounded-md bg-muted px-4 py-3 font-mono text-sm break-all select-all">
              {newKey.plaintext}
            </div>
            <DialogFooter className="gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleCopy(newKey.plaintext!)}
              >
                {copied ? (
                  <Check className="mr-1.5 h-3.5 w-3.5 text-green-500" />
                ) : (
                  <Copy className="mr-1.5 h-3.5 w-3.5" />
                )}
                {copied ? "Скопировано" : "Копировать"}
              </Button>
              <Button onClick={() => setNewKey(null)}>Закрыть</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}

      {/* Revoke Confirm Dialog */}
      <AlertDialog open={!!revokeTarget} onOpenChange={() => setRevokeTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Отозвать ключ?</AlertDialogTitle>
            <AlertDialogDescription>
              Ключ «{revokeTarget?.name}» будет немедленно деактивирован.
              Все запросы с этим ключом начнут получать 401. Это действие необратимо.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Отмена</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={handleRevoke}
            >
              <ShieldOff className="mr-1.5 h-3.5 w-3.5" />
              Отозвать
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

// ─── API Key Card ──────────────────────────────────────────────────────────

function APIKeyCard({
  apiKey,
  onRevoke,
}: {
  apiKey: MerchantAPIKeyListItem
  onRevoke: () => void
}) {
  const isExpired = apiKey.expiresAt
    ? new Date(apiKey.expiresAt) < new Date()
    : false

  return (
    <div className="flex items-center justify-between rounded-lg border bg-card px-4 py-3">
      <div className="flex items-start gap-3 min-w-0">
        <Key className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0 space-y-1">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-sm font-medium">{apiKey.name}</span>
            <span className="font-mono text-xs text-muted-foreground bg-muted rounded px-1.5 py-0.5">
              {apiKey.keyPrefix}…
            </span>
            {!apiKey.isActive && (
              <Badge variant="destructive" className="text-[10px]">Отозван</Badge>
            )}
            {isExpired && (
              <Badge variant="secondary" className="text-[10px]">Истёк</Badge>
            )}
          </div>
          <div className="flex items-center gap-2 flex-wrap">
            {apiKey.scopes.map((s) => (
              <Badge key={s} variant="outline" className="text-[10px]">
                {s}
              </Badge>
            ))}
          </div>
          <div className="flex items-center gap-3 text-xs text-muted-foreground flex-wrap">
            <span>Создан {fmtDate(apiKey.createdAt)}</span>
            {apiKey.lastUsedAt && (
              <span className="flex items-center gap-1">
                <Clock className="h-3 w-3" />
                Последнее использование: {fmtDate(apiKey.lastUsedAt)}
              </span>
            )}
            {apiKey.expiresAt && (
              <span>Истекает: {fmtDate(apiKey.expiresAt)}</span>
            )}
          </div>
        </div>
      </div>
      {apiKey.isActive && !isExpired && (
        <Button
          variant="ghost"
          size="icon"
          className="shrink-0 text-muted-foreground hover:text-destructive"
          onClick={onRevoke}
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      )}
    </div>
  )
}

// ─── Create Dialog ─────────────────────────────────────────────────────────

function CreateAPIKeyDialog({
  open,
  merchantId,
  onClose,
  onCreated,
}: {
  open: boolean
  merchantId: string
  onClose: () => void
  onCreated: (key: MerchantAPIKeyResponse) => void
}) {
  const [name, setName] = useState("")
  const [scopes, setScopes] = useState<APIKeyScope[]>(["invoice:create", "invoice:read"])
  const [submitting, setSubmitting] = useState(false)

  const toggleScope = (scope: APIKeyScope) => {
    setScopes((prev) =>
      prev.includes(scope) ? prev.filter((s) => s !== scope) : [...prev, scope]
    )
  }

  const handleSubmit = async () => {
    if (!name.trim()) {
      toast.error("Введите название ключа")
      return
    }
    if (scopes.length === 0) {
      toast.error("Выберите хотя бы один scope")
      return
    }
    setSubmitting(true)
    try {
      const body: CreateMerchantAPIKeyRequest = { name: name.trim(), scopes }
      const created = await api.merchantApiKeys.create(merchantId, body)
      onCreated(created)
      toast.success("API-ключ создан")
      setName("")
      setScopes(["invoice:create", "invoice:read"])
    } catch {
      toast.error("Не удалось создать ключ")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Создать API-ключ</DialogTitle>
          <DialogDescription>
            Ключ будет показан только один раз — сохраните его в надёжном месте.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div>
            <Label className="text-xs text-muted-foreground mb-1.5 block">
              Название <span className="text-destructive">*</span>
            </Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Например: Production Key"
              onKeyDown={(e) => e.key === "Enter" && handleSubmit()}
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground mb-2 block">Разрешения</Label>
            <div className="space-y-2">
              {SCOPE_OPTIONS.map(({ value, label }) => (
                <label
                  key={value}
                  className="flex items-center gap-2.5 cursor-pointer select-none"
                >
                  <Checkbox
                    checked={scopes.includes(value)}
                    onCheckedChange={() => toggleScope(value)}
                  />
                  <span className="text-sm">{label}</span>
                  <span className="font-mono text-xs text-muted-foreground">
                    ({value})
                  </span>
                </label>
              ))}
            </div>
          </div>
        </div>

        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={onClose} disabled={submitting}>
            Отмена
          </Button>
          <Button onClick={handleSubmit} disabled={submitting || !name.trim()}>
            {submitting ? (
              <RefreshCw className="mr-1.5 h-3.5 w-3.5 animate-spin" />
            ) : (
              <Plus className="mr-1.5 h-3.5 w-3.5" />
            )}
            Создать
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ─── Helpers ───────────────────────────────────────────────────────────────

function fmtDate(iso: string): string {
  try {
    return format(new Date(iso), "dd MMM yyyy", { locale: ru })
  } catch {
    return iso
  }
}
