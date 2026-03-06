"use client"

import { useRouter } from "next/navigation"
import { Loader2 } from "lucide-react"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useCatalogForm } from "@/hooks/useCatalogForm"
import { api } from "@/lib/api"
import type { CounterpartyResponse, CounterpartyType, LegalForm } from "@/types/catalog"
import { COUNTERPARTY_TYPE_LABELS, LEGAL_FORM_LABELS } from "@/types/catalog"

interface CounterpartyEditState {
  name: string
  code: string
  type: CounterpartyType
  legalForm: LegalForm
  fullName: string
  inn: string
  kpp: string
  ogrn: string
  legalAddress: string
  actualAddress: string
  phone: string
  email: string
  contactPerson: string
  comment: string
  version: number
  [key: string]: unknown
}

const INITIAL_STATE: CounterpartyEditState = {
  name: "",
  code: "",
  type: "supplier",
  legalForm: "company",
  fullName: "",
  inn: "",
  kpp: "",
  ogrn: "",
  legalAddress: "",
  actualAddress: "",
  phone: "",
  email: "",
  contactPerson: "",
  comment: "",
  version: 0,
}

export default function EditCounterpartyPage() {
  const router = useRouter()
  const { f, update, handleChange, handleSave, saving, error, loading, deletionMark } = useCatalogForm({
    entityName: "Контрагент",
    initialState: INITIAL_STATE,
    api: {
      get: api.counterparties.get,
      update: api.counterparties.update,
    },
    listPath: "/catalogs/counterparties",
    validate: (s) => !s.name ? "Укажите наименование" : null,
    titleField: (s) => s.name || undefined,
    mapFromResponse: (d) => ({
      name: d.name,
      code: d.code,
      type: d.type,
      legalForm: d.legalForm,
      fullName: d.fullName || "",
      inn: d.inn || "",
      kpp: d.kpp || "",
      ogrn: d.ogrn || "",
      legalAddress: d.legalAddress || "",
      actualAddress: d.actualAddress || "",
      phone: d.phone || "",
      email: d.email || "",
      contactPerson: d.contactPerson || "",
      comment: d.comment || "",
      version: d.version,
    }),
    mapToUpdate: (s) => ({
      name: s.name,
      code: s.code,
      type: s.type,
      legalForm: s.legalForm,
      fullName: s.fullName || null,
      inn: s.inn || null,
      kpp: s.kpp || null,
      ogrn: s.ogrn || null,
      legalAddress: s.legalAddress || null,
      actualAddress: s.actualAddress || null,
      phone: s.phone || null,
      email: s.email || null,
      contactPerson: s.contactPerson || null,
      comment: s.comment || null,
      version: s.version,
    }),
    getVersion: (d) => d.version,
    getDeletionMark: (d) => d.deletionMark,
  })

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
        title={`Контрагент: ${f.name || ""}`}
        status={
          deletionMark
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
        backHref="/catalogs/counterparties"
        onClose={() => router.push("/catalogs/counterparties")}
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
              <Input className="mt-1" value={f.code} onChange={(e) => { update({ code: e.target.value }); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Тип *</Label>
              <Select value={f.type} onValueChange={(v) => { update({ type: v as CounterpartyType }); handleChange() }}>
                <SelectTrigger className="mt-1"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {Object.entries(COUNTERPARTY_TYPE_LABELS).map(([k, label]) => (
                    <SelectItem key={k} value={k}>{label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Правовая форма *</Label>
              <Select value={f.legalForm} onValueChange={(v) => { update({ legalForm: v as LegalForm }); handleChange() }}>
                <SelectTrigger className="mt-1"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {Object.entries(LEGAL_FORM_LABELS).map(([k, label]) => (
                    <SelectItem key={k} value={k}>{label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
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
              <Label className="text-xs text-muted-foreground">ОГРН</Label>
              <Input className="mt-1" value={f.ogrn} onChange={(e) => { update({ ogrn: e.target.value }); handleChange() }} />
            </div>
          </div>

          <div className="grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-2">
            <div>
              <Label className="text-xs text-muted-foreground">Юридический адрес</Label>
              <Input className="mt-1" value={f.legalAddress} onChange={(e) => { update({ legalAddress: e.target.value }); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Фактический адрес</Label>
              <Input className="mt-1" value={f.actualAddress} onChange={(e) => { update({ actualAddress: e.target.value }); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Телефон</Label>
              <Input className="mt-1" value={f.phone} onChange={(e) => { update({ phone: e.target.value }); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Email</Label>
              <Input className="mt-1" type="email" value={f.email} onChange={(e) => { update({ email: e.target.value }); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Контактное лицо</Label>
              <Input className="mt-1" value={f.contactPerson} onChange={(e) => { update({ contactPerson: e.target.value }); handleChange() }} />
            </div>
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Комментарий</Label>
            <Textarea rows={3} className="mt-1" value={f.comment} onChange={(e) => { update({ comment: e.target.value }); handleChange() }} />
          </div>
        </div>
      </div>
    </div>
  )
}
