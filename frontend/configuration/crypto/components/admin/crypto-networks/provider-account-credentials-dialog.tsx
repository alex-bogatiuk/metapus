"use client"

import { useEffect, useState } from "react"
import useSWR from "swr"
import { Eye, EyeOff, KeyRound, Lock, ShieldCheck } from "lucide-react"
import { toast } from "sonner"

import { api, ApiError } from "@/lib/api"
import type { CryptoProviderAccount } from "@/configuration/crypto/types/crypto-network"
import type {
  CreateCryptoProviderAccountRequest,
  UpdateCryptoProviderAccountRequest,
} from "@/types/generated/http-contracts"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { ReferenceField } from "@/components/shared/reference-field"

import { cryptoApi } from "@/configuration/crypto/lib/api"
export interface ProviderAccountCredentialsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  networkId: string
  account?: CryptoProviderAccount | null
  onSaved: (account: CryptoProviderAccount, isNew: boolean) => void
}

export function ProviderAccountCredentialsDialog({
  open,
  onOpenChange,
  networkId,
  account,
  onSaved,
}: ProviderAccountCredentialsDialogProps) {
  const isNew = !account
  const [name, setName] = useState("")
  const [code, setCode] = useState("")
  const [provider, setProvider] = useState("itrx")
  const [balanceTokenId, setBalanceTokenId] = useState("")
  const [balanceTokenDisplay, setBalanceTokenDisplay] = useState("")
  const [externalAccountRef, setExternalAccountRef] = useState("")
  const [isActive, setIsActive] = useState(true)
  const [baseUrl, setBaseUrl] = useState("")
  const [apiKey, setApiKey] = useState("")
  const [apiSecret, setApiSecret] = useState("")
  const [callbackUrl, setCallbackUrl] = useState("")
  const [showSecret, setShowSecret] = useState(false)
  const [saving, setSaving] = useState(false)

  const { data: tokensData } = useSWR(
    open ? `/catalog/tokens?networkId=${networkId}` : null,
    () => cryptoApi.tokens.list({ limit: 50 })
  )

  useEffect(() => {
    if (!open) return
    if (account) {
      setName(account.name)
      setCode(account.code || "")
      setProvider(account.provider || "itrx")
      setBalanceTokenId(account.balanceTokenId)
      setExternalAccountRef(account.externalAccountRef || "")
      setIsActive(account.isActive)
      setBaseUrl("")
      setApiKey("")
      setApiSecret("")
      setCallbackUrl("")
    } else {
      setName("")
      setCode("")
      setProvider("itrx")
      const firstToken = (tokensData?.items ?? []).find(
        (t) => !t.networkId || t.networkId === networkId
      )
      setBalanceTokenId(firstToken?.id ?? "")
      setBalanceTokenDisplay(firstToken?.name ?? "")
      setExternalAccountRef("")
      setIsActive(true)
      setBaseUrl("https://api.itrx.io")
      setApiKey("")
      setApiSecret("")
      setCallbackUrl("")
    }
  }, [open, account, networkId, tokensData?.items])

  const handleSave = async () => {
    if (!name.trim()) {
      toast.error("Укажите наименование аккаунта")
      return
    }
    if (!balanceTokenId) {
      toast.error("Выберите токен депозита")
      return
    }

    try {
      setSaving(true)
      if (isNew) {
        const payload: CreateCryptoProviderAccountRequest = {
          name: name.trim(),
          provider: provider.trim(),
          networkId,
          balanceTokenId,
          isActive,
        }
        if (code.trim()) payload.code = code.trim()
        if (externalAccountRef.trim()) payload.externalAccountRef = externalAccountRef.trim()
        if (baseUrl.trim()) payload.baseUrl = baseUrl.trim()
        if (apiKey.trim()) payload.apiKey = apiKey.trim()
        if (apiSecret.trim()) payload.apiSecret = apiSecret.trim()
        if (callbackUrl.trim()) payload.callbackUrl = callbackUrl.trim()

        const created = await cryptoApi.cryptoProviderAccounts.create(payload)
        onSaved(created, true)
        onOpenChange(false)
      } else {
        const payload: UpdateCryptoProviderAccountRequest = {
          name: name.trim(),
          provider: provider.trim(),
          networkId,
          balanceTokenId,
          isActive,
          version: account.version,
        }
        if (code.trim()) payload.code = code.trim()
        if (externalAccountRef.trim()) payload.externalAccountRef = externalAccountRef.trim()
        if (baseUrl.trim()) payload.baseUrl = baseUrl.trim()
        if (apiKey.trim()) payload.apiKey = apiKey.trim()
        if (apiSecret.trim()) payload.apiSecret = apiSecret.trim()
        if (callbackUrl.trim()) payload.callbackUrl = callbackUrl.trim()

        const updated = await cryptoApi.cryptoProviderAccounts.update(account.id, payload)
        onSaved(updated, false)
        onOpenChange(false)
      }
    } catch (err) {
      if (err instanceof ApiError) {
        toast.error(err.parsedBody?.message ?? "Не удалось сохранить учетные данные")
      } else {
        toast.error("Не удалось сохранить учетные данные")
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <div className="flex items-center justify-between">
            <DialogTitle className="flex items-center gap-2 text-base">
              <KeyRound className="size-4 text-primary" />
              {isNew ? "Новый аккаунт провайдера" : `Настройка ключей: ${account?.name}`}
            </DialogTitle>
            <Badge variant="outline" className="gap-1 font-mono text-[10px]">
              <Lock className="size-3 text-emerald-500" />
              AES-256-GCM
            </Badge>
          </div>
          <DialogDescription className="text-xs">
            {isNew
              ? "Создайте аккаунт и укажите API-ключи для автоматической аренды энергии."
              : "Учетные данные шифруются в БД. Оставьте поля пустыми, если не хотите изменять текущие ключи."}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3.5 py-1">
          {account?.hasCredentials ? (
            <div className="rounded-md border border-emerald-500/20 bg-emerald-500/10 p-2.5 flex items-center justify-between text-xs">
              <div className="flex items-center gap-2 text-emerald-900 dark:text-emerald-200">
                <ShieldCheck className="size-4 text-emerald-600 dark:text-emerald-400 shrink-0" />
                <span>Ключи настроены: <strong className="font-mono">{account.maskedApiKey || "Заданы"}</strong></span>
              </div>
              <span className="text-muted-foreground font-mono text-[10px]">v{account.credentialsVersion}</span>
            </div>
          ) : null}

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <Label htmlFor="dlg-name" className="text-xs">Наименование <span className="text-destructive">*</span></Label>
              <Input
                id="dlg-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="ITRX Основной"
                className="h-8 text-xs"
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="dlg-provider" className="text-xs">Провайдер</Label>
              <Select value={provider} onValueChange={setProvider}>
                <SelectTrigger id="dlg-provider" className="h-8 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="itrx">ITRX (Tron Energy)</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <Label className="text-xs">Токен баланса <span className="text-destructive">*</span></Label>
              <ReferenceField
                apiEndpoint="/catalog/tokens"
                value={balanceTokenId}
                displayName={balanceTokenDisplay}
                onChange={(val, display) => {
                  setBalanceTokenId(val)
                  setBalanceTokenDisplay(display)
                }}
                placeholder="Токен (TRX)"
                compact
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="dlg-ref" className="text-xs">Внешний ID / Референс</Label>
              <Input
                id="dlg-ref"
                value={externalAccountRef}
                onChange={(e) => setExternalAccountRef(e.target.value)}
                placeholder="Субаккаунт или логин"
                className="h-8 text-xs font-mono"
              />
            </div>
          </div>

          <div className="rounded-md border p-3 space-y-3 bg-muted/20">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium flex items-center gap-1.5">
                <Lock className="size-3 text-muted-foreground" />
                Секреты API (Write-Only)
              </span>
              <span className="text-[10px] text-muted-foreground font-mono">Защищено</span>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <Label htmlFor="dlg-base-url" className="text-xs">Base URL</Label>
                <Input
                  id="dlg-base-url"
                  value={baseUrl}
                  onChange={(e) => setBaseUrl(e.target.value)}
                  placeholder="https://api.itrx.io"
                  className="h-8 text-xs font-mono"
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="dlg-callback" className="text-xs">Callback URL</Label>
                <Input
                  id="dlg-callback"
                  value={callbackUrl}
                  onChange={(e) => setCallbackUrl(e.target.value)}
                  placeholder="https://... (опционально)"
                  className="h-8 text-xs font-mono"
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <Label htmlFor="dlg-api-key" className="text-xs">
                  API Key {account?.hasCredentials ? "(для замены)" : ""}
                </Label>
                <Input
                  id="dlg-api-key"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  placeholder={account?.hasCredentials ? "••••••••••••••••" : "Ключ API"}
                  className="h-8 text-xs font-mono"
                  autoComplete="off"
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="dlg-api-secret" className="text-xs">
                  API Secret {account?.hasCredentials ? "(для замены)" : ""}
                </Label>
                <div className="relative">
                  <Input
                    id="dlg-api-secret"
                    type={showSecret ? "text" : "password"}
                    value={apiSecret}
                    onChange={(e) => setApiSecret(e.target.value)}
                    placeholder={account?.hasCredentials ? "••••••••••••••••" : "Секрет API"}
                    className="h-8 text-xs font-mono pr-8"
                    autoComplete="new-password"
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="absolute right-0 top-0 h-8 w-8 text-muted-foreground hover:text-foreground"
                    onClick={() => setShowSecret((v) => !v)}
                  >
                    {showSecret ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                  </Button>
                </div>
              </div>
            </div>
          </div>

          <div className="flex items-center justify-between pt-1">
            <div className="space-y-0.5">
              <Label htmlFor="dlg-active" className="text-xs cursor-pointer">Активен для аренды</Label>
              <p className="text-[10px] text-muted-foreground">Использовать в маршрутизации заказов</p>
            </div>
            <Switch
              id="dlg-active"
              checked={isActive}
              onCheckedChange={setIsActive}
            />
          </div>
        </div>

        <DialogFooter className="gap-2 sm:gap-0">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => onOpenChange(false)}
            disabled={saving}
          >
            Отмена
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={handleSave}
            disabled={saving}
          >
            {saving ? "Сохранение..." : isNew ? "Создать аккаунт" : "Сохранить"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
