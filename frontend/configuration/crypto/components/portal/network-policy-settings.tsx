"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { AlertTriangle, Loader2, Network, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { MerchantAPIKeyListItem } from "@/configuration/crypto/types/merchant-api"
import type {
  MerchantNetworkPolicy,
  MerchantNetworkPolicyMode,
  MerchantNetworkPolicyResponse,
  MerchantNetworkSurface,
} from "@/configuration/crypto/types/portal-api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"

type PolicyTarget = {
  surface: MerchantNetworkSurface
  keyId?: string
}

const BASELINE_KEY = "baseline"

const modeLabels: Record<MerchantNetworkPolicyMode, string> = {
  off: "Выключено",
  warn: "Только предупреждать",
  enforce: "Запрещать доступ",
}

function errorMessage<T>(error: T) {
  return error instanceof Error ? error.message : "Операция не выполнена"
}

export function NetworkPolicySettings({ merchantId }: { merchantId: string }) {
  const [surface, setSurface] = useState<MerchantNetworkSurface>("portal")
  const [selectedKeyId, setSelectedKeyId] = useState(BASELINE_KEY)
  const [apiKeys, setApiKeys] = useState<MerchantAPIKeyListItem[]>([])
  const [response, setResponse] = useState<MerchantNetworkPolicyResponse | null>(null)
  const [reason, setReason] = useState("")
  const [cidr, setCidr] = useState("")
  const [label, setLabel] = useState("")
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  const target = useMemo<PolicyTarget>(() => ({
    surface,
    keyId: surface === "api" && selectedKeyId !== BASELINE_KEY ? selectedKeyId : undefined,
  }), [selectedKeyId, surface])

  const loadPolicy = useCallback(async (nextTarget: PolicyTarget) => {
    setLoading(true)
    try {
      const next = nextTarget.keyId
        ? await api.portal.networkPolicies.getForKey(merchantId, nextTarget.keyId)
        : await api.portal.networkPolicies.get(merchantId, nextTarget.surface)
      setResponse(next)
    } catch (error) {
      setResponse(null)
      toast.error(errorMessage(error))
    } finally {
      setLoading(false)
    }
  }, [merchantId])

  useEffect(() => {
    void loadPolicy(target)
  }, [loadPolicy, target])

  useEffect(() => {
    let active = true
    api.portal.apiKeys.list(merchantId)
      .then((result) => {
        if (active) setApiKeys(result.items.filter((item) => item.isActive))
      })
      .catch(() => {
        if (active) setApiKeys([])
      })
    return () => {
      active = false
    }
  }, [merchantId])

  const updatePolicy = (policy: MerchantNetworkPolicy) => {
    setResponse((current) => current ? { ...current, policy } : null)
    setReason("")
  }

  const requireReason = () => {
    if (reason.trim()) return true
    toast.error("Укажите причину изменения политики")
    return false
  }

  const setMode = async (mode: MerchantNetworkPolicyMode) => {
    if (!requireReason()) return
    setSaving(true)
    try {
      const policy = target.keyId
        ? await api.portal.networkPolicies.setModeForKey(merchantId, target.keyId, mode, reason.trim())
        : await api.portal.networkPolicies.setMode(merchantId, target.surface, mode, reason.trim())
      updatePolicy(policy)
      await loadPolicy(target)
      toast.success("Режим сетевого доступа изменён")
    } catch (error) {
      toast.error(errorMessage(error))
    } finally {
      setSaving(false)
    }
  }

  const addNetwork = async () => {
    if (!cidr.trim() || !requireReason()) {
      if (!cidr.trim()) toast.error("Укажите сеть в формате CIDR")
      return
    }
    setSaving(true)
    try {
      const policy = target.keyId
        ? await api.portal.networkPolicies.addNetworkForKey(merchantId, target.keyId, cidr.trim(), label.trim(), reason.trim())
        : await api.portal.networkPolicies.addNetwork(merchantId, target.surface, cidr.trim(), label.trim(), reason.trim())
      updatePolicy(policy)
      setCidr("")
      setLabel("")
      toast.success("Разрешённая сеть добавлена")
    } catch (error) {
      toast.error(errorMessage(error))
    } finally {
      setSaving(false)
    }
  }

  const revokeNetwork = async (networkId: string) => {
    if (!requireReason()) return
    setSaving(true)
    try {
      const policy = target.keyId
        ? await api.portal.networkPolicies.revokeNetworkForKey(merchantId, target.keyId, networkId, reason.trim())
        : await api.portal.networkPolicies.revokeNetwork(merchantId, target.surface, networkId, reason.trim())
      updatePolicy(policy)
      toast.success("Сеть отозвана")
    } catch (error) {
      toast.error(errorMessage(error))
    } finally {
      setSaving(false)
    }
  }

  const policy = response?.policy
  const accessVariant = response?.networkAccess === "deny" ? "destructive" : "outline"

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <Network className="size-4 text-muted-foreground" />
          <CardTitle className="text-base">Сетевой доступ</CardTitle>
        </div>
        <CardDescription>
          Ограничение Portal и Merchant API по CIDR. Политика API-ключа пересекается с общей политикой API.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Tabs
          value={surface}
          onValueChange={(value) => {
            setSurface(value as MerchantNetworkSurface)
            setSelectedKeyId(BASELINE_KEY)
          }}
        >
          <TabsList variant="line">
            <TabsTrigger value="portal" variant="line">Portal</TabsTrigger>
            <TabsTrigger value="api" variant="line">Merchant API</TabsTrigger>
          </TabsList>

          <TabsContent value="portal">
            <PolicyEditor />
          </TabsContent>
          <TabsContent value="api">
            <Field className="mb-6">
              <FieldLabel htmlFor="network-policy-api-key">Область политики</FieldLabel>
              <Select value={selectedKeyId} onValueChange={setSelectedKeyId}>
                <SelectTrigger id="network-policy-api-key">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value={BASELINE_KEY}>Общая политика Merchant API</SelectItem>
                    {apiKeys.map((key) => (
                      <SelectItem key={key.id} value={key.id} description={key.keyPrefix}>
                        {key.name}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FieldDescription>
                Ограничение ключа не расширяет общую политику: запрос должен пройти обе проверки.
              </FieldDescription>
            </Field>
            <PolicyEditor />
          </TabsContent>
        </Tabs>
      </CardContent>
    </Card>
  )

  function PolicyEditor() {
    if (loading || !policy) {
      return (
        <div className="flex min-h-36 items-center justify-center text-muted-foreground">
          <Loader2 className="size-5 animate-spin" aria-label="Загрузка сетевой политики" />
        </div>
      )
    }

    return (
      <FieldGroup className="pt-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium">Текущий доступ</span>
            <Badge variant={accessVariant}>{response?.networkAccess ?? "allow"}</Badge>
          </div>
          <span className="text-sm text-muted-foreground">Определённый IP: {response?.detectedIp ?? "—"}</span>
        </div>

        {response?.networkAccess === "deny" && (
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>Текущий IP не входит в разрешённые сети</AlertTitle>
            <AlertDescription>
              Включение enforce заблокирует этот браузер. Добавьте текущий IP или CIDR с меткой recovery.
            </AlertDescription>
          </Alert>
        )}

        <Field>
          <FieldLabel htmlFor={`network-policy-mode-${surface}-${selectedKeyId}`}>Режим</FieldLabel>
          <Select value={policy.mode} onValueChange={(value) => void setMode(value as MerchantNetworkPolicyMode)} disabled={saving}>
            <SelectTrigger id={`network-policy-mode-${surface}-${selectedKeyId}`}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {(Object.keys(modeLabels) as MerchantNetworkPolicyMode[]).map((mode) => (
                  <SelectItem key={mode} value={mode}>{modeLabels[mode]}</SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <FieldDescription>Переход к enforce выполняется поэтапно: off → warn → enforce.</FieldDescription>
        </Field>

        <Field>
          <FieldLabel htmlFor={`network-policy-reason-${surface}-${selectedKeyId}`}>Причина изменения</FieldLabel>
          <Input
            id={`network-policy-reason-${surface}-${selectedKeyId}`}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            placeholder="Например: ограничение доступа офиса и VPN"
            disabled={saving}
          />
          <FieldDescription>Причина попадёт в неизменяемый security audit.</FieldDescription>
        </Field>

        <Field>
          <FieldLabel>Разрешённые сети</FieldLabel>
          {policy.allowedNetworks.length === 0 ? (
            <p className="text-sm text-muted-foreground">Активных CIDR пока нет.</p>
          ) : (
            <div className="grid gap-2">
              {policy.allowedNetworks.map((network) => (
                <div key={network.id} className="flex items-center justify-between gap-3 rounded-md border p-3">
                  <div className="min-w-0">
                    <p className="truncate font-mono text-sm">{network.network}</p>
                    {network.label && <p className="truncate text-sm text-muted-foreground">{network.label}</p>}
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={`Отозвать сеть ${network.network}`}
                    onClick={() => void revokeNetwork(network.id)}
                    disabled={saving}
                  >
                    <Trash2 data-icon="inline-start" />
                  </Button>
                </div>
              ))}
            </div>
          )}
        </Field>

        <div className="grid gap-4 md:grid-cols-2">
          <Field>
            <FieldLabel htmlFor={`network-policy-cidr-${surface}-${selectedKeyId}`}>CIDR</FieldLabel>
            <Input
              id={`network-policy-cidr-${surface}-${selectedKeyId}`}
              value={cidr}
              onChange={(event) => setCidr(event.target.value)}
              placeholder="203.0.113.24/32 или 2001:db8::/48"
              disabled={saving}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor={`network-policy-label-${surface}-${selectedKeyId}`}>Метка</FieldLabel>
            <Input
              id={`network-policy-label-${surface}-${selectedKeyId}`}
              value={label}
              onChange={(event) => setLabel(event.target.value)}
              placeholder="Офис, VPN или recovery"
              disabled={saving}
            />
          </Field>
        </div>
        <Button type="button" variant="outline" onClick={() => void addNetwork()} disabled={saving || !cidr.trim()}>
          {saving ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Plus data-icon="inline-start" />}
          Добавить сеть
        </Button>
      </FieldGroup>
    )
  }
}
