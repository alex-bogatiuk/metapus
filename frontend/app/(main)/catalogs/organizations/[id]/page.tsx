"use client"

import { useState, useEffect } from "react"
import { useRouter, useParams } from "next/navigation"
import { Loader2 } from "lucide-react"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useTabDirty } from "@/hooks/useTabDirty"
import { api } from "@/lib/api"
import type { OrganizationResponse } from "@/types/catalog"

export default function EditOrganizationPage() {
  const router = useRouter()
  const params = useParams<{ id: string }>()
  const { markDirty, markClean } = useTabDirty()

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [doc, setDoc] = useState<OrganizationResponse | null>(null)

  const [name, setName] = useState("")
  const [code, setCode] = useState("")
  const [fullName, setFullName] = useState("")
  const [inn, setInn] = useState("")
  const [kpp, setKpp] = useState("")
  const [baseCurrencyId, setBaseCurrencyId] = useState("")
  const [isDefault, setIsDefault] = useState(false)
  const [version, setVersion] = useState(0)

  useEffect(() => {
    if (!params.id) return
    setLoading(true)
    api.organizations.get(params.id).then((d) => {
      setDoc(d)
      setName(d.name)
      setCode(d.code)
      setFullName(d.fullName || "")
      setInn(d.inn || "")
      setKpp(d.kpp || "")
      setBaseCurrencyId(d.baseCurrencyId)
      setIsDefault(d.isDefault)
      setVersion(d.version)
    }).catch((err) => {
      setError(err instanceof Error ? err.message : "Ошибка загрузки")
    }).finally(() => setLoading(false))
  }, [params.id])

  const handleChange = () => markDirty()

  const handleSave = async (andClose: boolean) => {
    if (!name) { setError("Укажите наименование"); return }
    setSaving(true)
    setError(null)
    try {
      const updated = await api.organizations.update(params.id, {
        id: params.id,
        name,
        code,
        fullName: fullName || undefined,
        inn: inn || undefined,
        kpp: kpp || undefined,
        baseCurrencyId,
        isDefault,
        version,
      })
      setDoc(updated)
      setVersion(updated.version)
      markClean()
      if (andClose) router.push("/catalogs/organizations")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка сохранения")
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center text-muted-foreground">
        <Loader2 className="mr-2 h-5 w-5 animate-spin" />Загрузка…
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col">
      <FormToolbar
        title={`Организация: ${doc?.name || ""}`}
        status={
          doc?.deletionMark
            ? { label: "Помечен на удаление", variant: "destructive" as const }
            : undefined
        }
        primaryAction={{
          label: saving ? "Сохранение…" : "Записать и закрыть",
          variant: "default",
          onClick: () => handleSave(true),
        }}
        secondaryActions={[
          { label: "Записать", onClick: () => handleSave(false) },
        ]}
        backHref="/catalogs/organizations"
        onClose={() => router.push("/catalogs/organizations")}
      />

      {error && (
        <div className="bg-destructive/10 border-b border-destructive/20 px-4 py-2 text-sm text-destructive">{error}</div>
      )}

      <div className="flex-1 overflow-auto p-6">
        <div className="max-w-3xl space-y-6">
          <div className="grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-2">
            <div>
              <Label className="text-xs text-muted-foreground">Наименование *</Label>
              <Input className="mt-1" value={name} onChange={(e) => { setName(e.target.value); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Код</Label>
              <Input className="mt-1" value={code} onChange={(e) => { setCode(e.target.value); handleChange() }} />
            </div>
            <div className="md:col-span-2">
              <Label className="text-xs text-muted-foreground">Полное наименование</Label>
              <Input className="mt-1" value={fullName} onChange={(e) => { setFullName(e.target.value); handleChange() }} />
            </div>
          </div>

          <div className="grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-3">
            <div>
              <Label className="text-xs text-muted-foreground">ИНН</Label>
              <Input className="mt-1" value={inn} onChange={(e) => { setInn(e.target.value); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">КПП</Label>
              <Input className="mt-1" value={kpp} onChange={(e) => { setKpp(e.target.value); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Базовая валюта (ID)</Label>
              <Input className="mt-1" value={baseCurrencyId} onChange={(e) => { setBaseCurrencyId(e.target.value); handleChange() }} />
            </div>
          </div>

          <div className="flex items-center gap-2">
            <Switch checked={isDefault} onCheckedChange={(v) => { setIsDefault(v); handleChange() }} />
            <Label className="text-xs">Основная организация</Label>
          </div>
        </div>
      </div>
    </div>
  )
}
