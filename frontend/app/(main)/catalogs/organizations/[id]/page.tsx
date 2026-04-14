"use client"

import { useState, useEffect } from "react"
import { useRouter, useParams, usePathname } from "next/navigation"
import { Loader2 } from "lucide-react"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { ScrollArea } from "@/components/ui/scroll-area"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useTabTitle } from "@/hooks/useTabTitle"
import { useFormDraft } from "@/hooks/useFormDraft"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { api } from "@/lib/api"
import type { OrganizationResponse } from "@/types/catalog"
import {
  OrganizationFormTabs,
  INITIAL_ORG_STATE,
} from "@/components/catalogs/organization-form-tabs"
import type { OrganizationFormState } from "@/components/catalogs/organization-form-tabs"

function mapFromResponse(d: OrganizationResponse): OrganizationFormState {
  return {
    name: d.name,
    code: d.code,
    fullName: d.fullName || "",
    inn: d.inn || "",
    kpp: d.kpp || "",
    ogrn: d.ogrn || "",
    legalAddress: d.legalAddress || "",
    actualAddress: d.actualAddress || "",
    phone: d.phone || "",
    email: d.email || "",
    website: d.website || "",
    baseCurrencyId: d.baseCurrencyId,
    baseCurrencyName: "",
    isDefault: d.isDefault,
    director: d.director || "",
    accountant: d.accountant || "",
    logoUrl: d.logoUrl || "",
    taxSystem: d.taxSystem || "osno",
    vatPayer: d.vatPayer,
    defaultVatRateId: d.defaultVatRateId || "",
    defaultVatRateName: "",
    inventoryMethod: d.inventoryMethod || "fifo",
    fiscalYearStart: d.fiscalYearStart || "01-01",
    version: d.version,
  }
}

export default function EditOrganizationPage() {
  const router = useRouter()
  const params = useParams<{ id: string }>()
  const pathname = usePathname()
  const { markDirty, markClean } = useTabDirty()
  const { state: f, update, replace, clear, hasDraft } = useFormDraft<OrganizationFormState>(pathname, INITIAL_ORG_STATE)

  const [loading, setLoading] = useState(!hasDraft)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [doc, setDoc] = useState<OrganizationResponse | null>(null)
  const orgLabel = useMetadataStore((s) => s.getLabel("organization", "singular"))
  useTabTitle(f.name || undefined, orgLabel)

  useEffect(() => {
    if (!params.id || hasDraft) return
    setLoading(true)
    api.organizations.get(params.id).then((d) => {
      setDoc(d)
      replace(mapFromResponse(d))
    }).catch((err) => {
      setError(err instanceof Error ? err.message : "Ошибка загрузки")
    }).finally(() => setLoading(false))
  }, [params.id, hasDraft, replace])

  const handleChange = () => markDirty()

  const handleSave = async (andClose: boolean) => {
    if (!f.name) { setError("Укажите наименование"); return }
    setSaving(true)
    setError(null)
    try {
      const updated = await api.organizations.update(params.id, {
        id: params.id,
        name: f.name,
        code: f.code,
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
        version: f.version,
      })
      setDoc(updated)
      update({ version: updated.version })
      clear()
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
        title={`${orgLabel}: ${doc?.name || ""}`}
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
        backTargetId={params.id}
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
