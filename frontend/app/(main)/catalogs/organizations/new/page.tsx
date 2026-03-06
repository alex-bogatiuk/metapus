"use client"

import { useState } from "react"
import { useRouter, usePathname } from "next/navigation"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { ReferenceField } from "@/components/shared/reference-field"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useFormDraft } from "@/hooks/useFormDraft"
import { api } from "@/lib/api"

interface OrganizationFormState {
  name: string
  code: string
  fullName: string
  inn: string
  kpp: string
  baseCurrencyId: string
  baseCurrencyName: string
  isDefault: boolean
}

const INITIAL_STATE: OrganizationFormState = {
  name: "",
  code: "",
  fullName: "",
  inn: "",
  kpp: "",
  baseCurrencyId: "",
  baseCurrencyName: "",
  isDefault: false,
}

export default function NewOrganizationPage() {
  const router = useRouter()
  const pathname = usePathname()
  const { markDirty, markClean } = useTabDirty()
  const { state: f, update, clear } = useFormDraft<OrganizationFormState>(pathname, INITIAL_STATE)

  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleChange = () => markDirty()

  const handleSave = async (andClose: boolean) => {
    if (!f.name) { setError("Укажите наименование"); return }
    if (!f.baseCurrencyId) { setError("Укажите базовую валюту"); return }
    setSaving(true)
    setError(null)
    try {
      const created = await api.organizations.create({
        name: f.name,
        code: f.code || undefined,
        fullName: f.fullName || undefined,
        inn: f.inn || undefined,
        kpp: f.kpp || undefined,
        baseCurrencyId: f.baseCurrencyId,
        isDefault: f.isDefault,
      })
      clear()
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
              <Input className="mt-1" value={f.name} onChange={(e) => { update({ name: e.target.value }); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Код</Label>
              <Input className="mt-1" value={f.code} onChange={(e) => { update({ code: e.target.value }); handleChange() }} placeholder="Авто" />
            </div>
            <div className="md:col-span-2">
              <Label className="text-xs text-muted-foreground">Полное наименование</Label>
              <Input className="mt-1" value={f.fullName} onChange={(e) => { update({ fullName: e.target.value }); handleChange() }} />
            </div>
          </div>

          <div className="grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-3">
            <div>
              <Label className="text-xs text-muted-foreground">ИНН</Label>
              <Input className="mt-1" value={f.inn} onChange={(e) => { update({ inn: e.target.value }); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">КПП</Label>
              <Input className="mt-1" value={f.kpp} onChange={(e) => { update({ kpp: e.target.value }); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Базовая валюта *</Label>
              <div className="mt-1">
                <ReferenceField
                  value={f.baseCurrencyId}
                  displayName={f.baseCurrencyName}
                  apiEndpoint="/catalog/currencies"
                  placeholder="Выберите валюту"
                  onChange={(id, name) => { update({ baseCurrencyId: id, baseCurrencyName: name }); handleChange() }}
                />
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <Switch checked={f.isDefault} onCheckedChange={(v) => { update({ isDefault: v }); handleChange() }} />
            <Label className="text-xs">Основная организация</Label>
          </div>
        </div>
      </div>
    </div>
  )
}
