"use client"

import { useState } from "react"
import { useRouter, usePathname } from "next/navigation"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { ScrollArea } from "@/components/ui/scroll-area"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useFormDraft } from "@/hooks/useFormDraft"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { api } from "@/lib/api"
import {
  OrganizationFormTabs,
  INITIAL_ORG_STATE,
} from "@/components/catalogs/organization-form-tabs"
import type { OrganizationFormState } from "@/components/catalogs/organization-form-tabs"

export default function NewOrganizationPage() {
  const router = useRouter()
  const pathname = usePathname()
  const { markDirty, markClean } = useTabDirty()
  const { state: f, update, clear } = useFormDraft<OrganizationFormState>(pathname, INITIAL_ORG_STATE)

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
        ogrn: f.ogrn || undefined,
        legalAddress: f.legalAddress || undefined,
        actualAddress: f.actualAddress || undefined,
        phone: f.phone || undefined,
        email: f.email || undefined,
        website: f.website || undefined,
        baseCurrencyId: f.baseCurrencyId,
        isDefault: f.isDefault,
        director: f.director || undefined,
        accountant: f.accountant || undefined,
        logoUrl: f.logoUrl || undefined,
        taxSystem: f.taxSystem || undefined,
        vatPayer: f.vatPayer,
        defaultVatRateId: f.defaultVatRateId || undefined,
        inventoryMethod: f.inventoryMethod || undefined,
        fiscalYearStart: f.fiscalYearStart || undefined,
      })
      clear()
      markClean()
      if (andClose) {
        router.push("/catalogs/organizations")
      } else {
        router.replace(`/catalogs/organizations/${created.id}`)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Не удалось сохранить данные")
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex h-full flex-col">
      <FormToolbar
        title={`${useMetadataStore.getState().getLabel("organization", "singular")} (создание)`}
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

      <ScrollArea className="flex-1">
        <div className="p-6">
          <div className="max-w-3xl">
            <OrganizationFormTabs f={f} update={update} onChange={handleChange} />
          </div>
        </div>
      </ScrollArea>
    </div>
  )
}
