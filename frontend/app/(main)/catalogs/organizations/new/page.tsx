"use client"

import { useState } from "react"
import { useRouter } from "next/navigation"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useTabDirty } from "@/hooks/useTabDirty"
import { api } from "@/lib/api"

export default function NewOrganizationPage() {
  const router = useRouter()
  const { markDirty, markClean } = useTabDirty()

  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [name, setName] = useState("")
  const [code, setCode] = useState("")
  const [fullName, setFullName] = useState("")
  const [inn, setInn] = useState("")
  const [kpp, setKpp] = useState("")
  const [baseCurrencyId, setBaseCurrencyId] = useState("")
  const [isDefault, setIsDefault] = useState(false)

  const handleChange = () => markDirty()

  const handleSave = async (andClose: boolean) => {
    if (!name) { setError("Укажите наименование"); return }
    if (!baseCurrencyId) { setError("Укажите базовую валюту"); return }
    setSaving(true)
    setError(null)
    try {
      const created = await api.organizations.create({
        name,
        code: code || undefined,
        fullName: fullName || undefined,
        inn: inn || undefined,
        kpp: kpp || undefined,
        baseCurrencyId,
        isDefault,
      })
      markClean()
      if (andClose) {
        router.push("/catalogs/organizations")
      } else {
        router.replace(`/catalogs/organizations/${created.id}`)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка сохранения")
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex h-full flex-col">
      <FormToolbar
        title="Организация (создание)"
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
              <Input className="mt-1" value={code} onChange={(e) => { setCode(e.target.value); handleChange() }} placeholder="Авто" />
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
              <Label className="text-xs text-muted-foreground">Базовая валюта (ID) *</Label>
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
