"use client"

import { useCallback, useEffect, useState } from "react"
import { usePathname, useRouter } from "next/navigation"
import { toast } from "sonner"
import {
  ShieldCheck,
  ShieldAlert,
  KeyRound,
  Eye,
  EyeOff,
  Lock,
  Server,
  Network,
  Coins,
  FileCode,
} from "lucide-react"

import { api, ApiError } from "@/lib/api"
import type { EntityFormProps } from "@/lib/entity-registry"
import type { CryptoProviderAccount } from "@/configuration/crypto/types/crypto-network"
import type { CreateCryptoProviderAccountRequest, UpdateCryptoProviderAccountRequest } from "@/configuration/crypto/types/generated/http-contracts"
import { useTabTitle } from "@/hooks/useTabTitle"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useCloseTab } from "@/hooks/useCloseTab"
import { useTabsStore } from "@/stores/useTabsStore"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { ReferenceField } from "@/components/shared/reference-field"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { ScrollArea } from "@/components/ui/scroll-area"

import { cryptoApi } from "@/configuration/crypto/lib/api"
export default function CryptoProviderAccountForm({
  id,
  entityName,
}: EntityFormProps) {
  const isNew = !id || id === "new"
  const router = useRouter()
  const pathname = usePathname()
  const { closeOne, closeAndShowInList } = useCloseTab()
  const { markDirty, markClean } = useTabDirty()

  const [loading, setLoading] = useState(!isNew)
  const [saving, setSaving] = useState(false)
  const [showSecret, setShowSecret] = useState(false)

  // Entity fields
  const [code, setCode] = useState("")
  const [name, setName] = useState("")
  const [provider, setProvider] = useState("itrx")
  const [networkId, setNetworkId] = useState("")
  const [networkDisplay, setNetworkDisplay] = useState("")
  const [balanceTokenId, setBalanceTokenId] = useState("")
  const [balanceTokenDisplay, setBalanceTokenDisplay] = useState("")
  const [externalAccountRef, setExternalAccountRef] = useState("")
  const [isActive, setIsActive] = useState(true)
  const [version, setVersion] = useState(1)

  // Credentials fields (Write-Only)
  const [baseUrl, setBaseUrl] = useState("")
  const [apiKey, setApiKey] = useState("")
  const [apiSecret, setApiSecret] = useState("")
  const [callbackUrl, setCallbackUrl] = useState("")

  // Existing credentials status (Read-Only from server)
  const [hasCredentials, setHasCredentials] = useState(false)
  const [maskedApiKey, setMaskedApiKey] = useState<string | undefined>()
  const [credentialsVersion, setCredentialsVersion] = useState(0)
  const [credentialsUpdatedAt, setCredentialsUpdatedAt] = useState<string | undefined>()

  useTabTitle(isNew ? "Новый аккаунт" : name || "Аккаунт провайдера", "Аккаунт провайдера")

  const loadData = useCallback(async () => {
    if (isNew) {
      setBaseUrl("https://api.itrx.io")
      setLoading(false)
      return
    }

    try {
      setLoading(true)
      const res = await cryptoApi.cryptoProviderAccounts.get(id!)
      setCode(res.code || "")
      setName(res.name || "")
      setProvider(res.provider || "itrx")
      setNetworkId(res.networkId || "")
      setBalanceTokenId(res.balanceTokenId || "")
      setExternalAccountRef(res.externalAccountRef || "")
      setIsActive(res.isActive)
      setVersion(res.version)
      setHasCredentials(res.hasCredentials)
      setMaskedApiKey(res.maskedApiKey)
      setCredentialsVersion(res.credentialsVersion)
      setCredentialsUpdatedAt(res.credentialsUpdatedAt)
      setBaseUrl("")
      setApiKey("")
      setApiSecret("")
      setCallbackUrl("")
    } catch (err) {
      if (err instanceof ApiError) {
        toast.error(err.parsedBody?.message ?? "Не удалось загрузить данные аккаунта")
      } else {
        toast.error("Не удалось загрузить данные аккаунта")
      }
    } finally {
      setLoading(false)
    }
  }, [id, isNew])

  useEffect(() => {
    loadData()
  }, [loadData])

  const handleClose = () => {
    if (!isNew && id) {
      closeAndShowInList(pathname, "/catalogs/crypto-provider-accounts", id)
      return
    }
    closeOne(pathname)
    router.push("/catalogs/crypto-provider-accounts")
  }

  const handleSave = async (andClose: boolean) => {
    if (!name.trim()) {
      toast.error("Укажите наименование аккаунта")
      return
    }
    if (!provider.trim()) {
      toast.error("Выберите провайдера")
      return
    }
    if (!networkId) {
      toast.error("Выберите блокчейн-сеть")
      return
    }
    if (!balanceTokenId) {
      toast.error("Выберите токен баланса")
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
        markClean()
        toast.success("Аккаунт успешно создан")

        const editUrl = `/catalogs/crypto-provider-accounts/${created.id}`
        if (andClose) {
          closeOne(pathname)
          router.push("/catalogs/crypto-provider-accounts")
        } else {
          useTabsStore.getState().openTab({
            id: editUrl,
            url: editUrl,
            title: created.name,
            isDirty: false,
          })
          closeOne(pathname)
          router.push(editUrl)
        }
      } else {
        const payload: UpdateCryptoProviderAccountRequest = {
          name: name.trim(),
          provider: provider.trim(),
          networkId,
          balanceTokenId,
          isActive,
          version,
        }
        if (code.trim()) payload.code = code.trim()
        if (externalAccountRef.trim()) payload.externalAccountRef = externalAccountRef.trim()
        if (baseUrl.trim()) payload.baseUrl = baseUrl.trim()
        if (apiKey.trim()) payload.apiKey = apiKey.trim()
        if (apiSecret.trim()) payload.apiSecret = apiSecret.trim()
        if (callbackUrl.trim()) payload.callbackUrl = callbackUrl.trim()

        const updated = await cryptoApi.cryptoProviderAccounts.update(id!, payload)
        setVersion(updated.version)
        setHasCredentials(updated.hasCredentials)
        setMaskedApiKey(updated.maskedApiKey)
        setCredentialsVersion(updated.credentialsVersion)
        setCredentialsUpdatedAt(updated.credentialsUpdatedAt)
        setApiKey("")
        setApiSecret("")
        markClean()
        toast.success("Данные аккаунта сохранены")

        if (andClose) {
          closeOne(pathname)
          router.push("/catalogs/crypto-provider-accounts")
        }
      }
    } catch (err) {
      if (err instanceof ApiError) {
        toast.error(err.parsedBody?.message ?? "Ошибка при сохранении")
      } else {
        toast.error("Ошибка при сохранении")
      }
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <div className="relative flex h-full flex-col overflow-hidden bg-workspace-surface">
        <div className="flex flex-col gap-6 p-6 max-w-5xl mx-auto w-full">
          <Skeleton className="h-10 w-64" />
          <div className="grid gap-6 md:grid-cols-2">
            <Skeleton className="h-64 rounded-xl" />
            <Skeleton className="h-64 rounded-xl" />
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="relative flex h-full flex-col overflow-hidden bg-workspace-surface">
      <FormToolbar
        title={isNew ? "Новый аккаунт крипто-провайдера" : name || "Аккаунт крипто-провайдера"}
        status={{
          label: isActive ? "Активен" : "Неактивен",
          variant: isActive ? "success" : "secondary",
        }}
        primaryAction={{
          label: "Записать и закрыть",
          onClick: () => handleSave(true),
          disabled: saving,
        }}
        secondaryActions={[
          {
            label: "Записать",
            onClick: () => handleSave(false),
            disabled: saving,
          },
        ]}
        backHref="/catalogs/crypto-provider-accounts"
        onClose={handleClose}
      />

      <ScrollArea className="flex-1">
        <main className="p-6 max-w-5xl w-full mx-auto space-y-6">
        {/* Card 1: Main configuration */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base flex items-center gap-2">
              <Server className="size-4 text-primary" />
              Основные параметры аккаунта
            </CardTitle>
            <CardDescription>
              Настройка провайдера аренды вычислительных ресурсов (Tron Energy / Bandwidth)
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="account-name">
                  Наименование <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="account-name"
                  value={name}
                  onChange={(e) => {
                    setName(e.target.value)
                    markDirty()
                  }}
                  placeholder="Например: ITRX Основной пул Nile"
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="account-code">Код сущности</Label>
                <Input
                  id="account-code"
                  value={code}
                  onChange={(e) => {
                    setCode(e.target.value)
                    markDirty()
                  }}
                  placeholder="Авто или произвольный код"
                  className="font-mono text-sm"
                />
              </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="account-provider">
                  Провайдер <span className="text-destructive">*</span>
                </Label>
                <Select
                  value={provider}
                  onValueChange={(val) => {
                    setProvider(val)
                    markDirty()
                  }}
                >
                  <SelectTrigger id="account-provider">
                    <SelectValue placeholder="Выберите провайдера" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="itrx">ITRX (Аренда энергии Tron)</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-1.5">
                <Label>
                  Блокчейн-сеть <span className="text-destructive">*</span>
                </Label>
                <ReferenceField
                  apiEndpoint="/catalog/blockchain-networks"
                  value={networkId}
                  displayName={networkDisplay}
                  onChange={(val, display) => {
                    setNetworkId(val)
                    setNetworkDisplay(display)
                    markDirty()
                  }}
                  placeholder="Выберите сеть (например, Tron Nile)"
                />
              </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label>
                  Токен баланса депозита <span className="text-destructive">*</span>
                </Label>
                <ReferenceField
                  apiEndpoint="/catalog/tokens"
                  value={balanceTokenId}
                  displayName={balanceTokenDisplay}
                  onChange={(val, display) => {
                    setBalanceTokenId(val)
                    setBalanceTokenDisplay(display)
                    markDirty()
                  }}
                  placeholder="Выберите токен (например, TRX)"
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="external-account-ref">Внешний ID / Референс аккаунта</Label>
                <Input
                  id="external-account-ref"
                  value={externalAccountRef}
                  onChange={(e) => {
                    setExternalAccountRef(e.target.value)
                    markDirty()
                  }}
                  placeholder="Логин или ID субаккаунта на платформе"
                  className="font-mono text-sm"
                />
              </div>
            </div>

            <div className="flex items-center justify-between pt-2 border-t">
              <div className="space-y-0.5">
                <Label htmlFor="is-active" className="cursor-pointer">Активен для аренды</Label>
                <p className="text-xs text-muted-foreground">
                  Отключите, если хотите временно исключить аккаунт из автоматической ротации
                </p>
              </div>
              <Switch
                id="is-active"
                checked={isActive}
                onCheckedChange={(val) => {
                  setIsActive(val)
                  markDirty()
                }}
              />
            </div>
          </CardContent>
        </Card>

        {/* Card 2: Security & Credentials */}
        <Card className="border-primary/20">
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="text-base flex items-center gap-2">
                <KeyRound className="size-4 text-primary" />
                Учетные данные API провайдера
              </CardTitle>
              <Badge variant="outline" className="gap-1 font-mono text-xs">
                <Lock className="size-3 text-emerald-500" />
                AES-256-GCM
              </Badge>
            </div>
            <CardDescription>
              Ключи и секреты сохраняются в зашифрованном виде. Режим Write-Only: в браузере ключи никогда не отображаются открытым текстом.
            </CardDescription>
          </CardHeader>

          <CardContent className="space-y-4">
            {hasCredentials ? (
              <div className="rounded-lg border border-emerald-500/20 bg-emerald-500/10 p-3.5 flex items-start gap-3">
                <ShieldCheck className="size-5 text-emerald-600 dark:text-emerald-400 shrink-0 mt-0.5" />
                <div className="text-sm space-y-1">
                  <div className="font-semibold text-emerald-900 dark:text-emerald-200 flex items-center gap-2">
                    Учетные данные настроены и зашифрованы
                    {maskedApiKey ? (
                      <span className="font-mono font-normal text-xs px-2 py-0.5 rounded bg-emerald-500/20 text-emerald-800 dark:text-emerald-300">
                        {maskedApiKey}
                      </span>
                    ) : null}
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Версия ключей #{credentialsVersion}
                    {credentialsUpdatedAt
                      ? ` · Обновлены ${new Date(credentialsUpdatedAt).toLocaleString()}`
                      : ""}
                    . Оставьте поля ключей пустыми, чтобы сохранить текущие настройки.
                  </p>
                </div>
              </div>
            ) : (
              <div className="rounded-lg border border-amber-500/20 bg-amber-500/10 p-3.5 flex items-start gap-3">
                <ShieldAlert className="size-5 text-amber-600 dark:text-amber-400 shrink-0 mt-0.5" />
                <div className="text-sm space-y-1">
                  <div className="font-semibold text-amber-900 dark:text-amber-200">
                    Учетные данные не заданы
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Воркер платформы не сможет выполнять автоматическую аренду ресурсов для этого аккаунта, пока не будут указаны API-ключи.
                  </p>
                </div>
              </div>
            )}

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="api-base-url">Базовый URL API</Label>
                <Input
                  id="api-base-url"
                  value={baseUrl}
                  onChange={(e) => {
                    setBaseUrl(e.target.value)
                    markDirty()
                  }}
                  placeholder="https://api.itrx.io"
                  className="font-mono text-sm"
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="api-callback-url">Callback URL</Label>
                <Input
                  id="api-callback-url"
                  value={callbackUrl}
                  onChange={(e) => {
                    setCallbackUrl(e.target.value)
                    markDirty()
                  }}
                  placeholder="https://... (опционально)"
                  className="font-mono text-sm"
                />
              </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="api-key">
                  API Key {hasCredentials ? "(для замены)" : ""}
                </Label>
                <Input
                  id="api-key"
                  value={apiKey}
                  onChange={(e) => {
                    setApiKey(e.target.value)
                    markDirty()
                  }}
                  placeholder={hasCredentials ? "••••••••••••••••" : "Введите API-ключ"}
                  className="font-mono text-sm"
                  autoComplete="off"
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="api-secret">
                  API Secret {hasCredentials ? "(для замены)" : ""}
                </Label>
                <div className="relative">
                  <Input
                    id="api-secret"
                    type={showSecret ? "text" : "password"}
                    value={apiSecret}
                    onChange={(e) => {
                      setApiSecret(e.target.value)
                      markDirty()
                    }}
                    placeholder={hasCredentials ? "••••••••••••••••" : "Введите секрет или пароль API"}
                    className="font-mono text-sm pr-9"
                    autoComplete="new-password"
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="absolute right-0 top-0 h-full w-9 text-muted-foreground hover:text-foreground"
                    onClick={() => setShowSecret((v) => !v)}
                  >
                    {showSecret ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                  </Button>
                </div>
              </div>
            </div>

            <div className="flex items-center gap-2 text-xs text-muted-foreground pt-1">
              <Lock className="size-3 text-muted-foreground/80 shrink-0" />
              <span>
                Ключи шифруются симметричным ключом до записи в БД и никогда не передаются в сторонние логи.
              </span>
            </div>
          </CardContent>
        </Card>
        </main>
      </ScrollArea>
    </div>
  )
}
