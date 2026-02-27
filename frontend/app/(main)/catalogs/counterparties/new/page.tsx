"use client"

import { useState } from "react"
import { useRouter } from "next/navigation"
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
import { useTabDirty } from "@/hooks/useTabDirty"
import { api } from "@/lib/api"
import type { CounterpartyType, LegalForm } from "@/types/catalog"
import { COUNTERPARTY_TYPE_LABELS, LEGAL_FORM_LABELS } from "@/types/catalog"

export default function NewCounterpartyPage() {
  const router = useRouter()
  const { markDirty, markClean } = useTabDirty()

  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [name, setName] = useState("")
  const [code, setCode] = useState("")
  const [type, setType] = useState<CounterpartyType>("supplier")
  const [legalForm, setLegalForm] = useState<LegalForm>("company")
  const [fullName, setFullName] = useState("")
  const [inn, setInn] = useState("")
  const [kpp, setKpp] = useState("")
  const [ogrn, setOgrn] = useState("")
  const [legalAddress, setLegalAddress] = useState("")
  const [actualAddress, setActualAddress] = useState("")
  const [phone, setPhone] = useState("")
  const [email, setEmail] = useState("")
  const [contactPerson, setContactPerson] = useState("")
  const [comment, setComment] = useState("")

  const handleChange = () => markDirty()

  const handleSave = async (andClose: boolean) => {
    if (!name) { setError("Укажите наименование"); return }
    setSaving(true)
    setError(null)
    try {
      const created = await api.counterparties.create({
        name,
        code: code || undefined,
        type,
        legalForm,
        fullName: fullName || null,
        inn: inn || null,
        kpp: kpp || null,
        ogrn: ogrn || null,
        legalAddress: legalAddress || null,
        actualAddress: actualAddress || null,
        phone: phone || null,
        email: email || null,
        contactPerson: contactPerson || null,
        comment: comment || null,
      })
      markClean()
      if (andClose) {
        router.push("/catalogs/counterparties")
      } else {
        router.replace(`/catalogs/counterparties/${created.id}`)
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
        title="Контрагент (создание)"
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
              <Input className="mt-1" value={name} onChange={(e) => { setName(e.target.value); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Код</Label>
              <Input className="mt-1" value={code} onChange={(e) => { setCode(e.target.value); handleChange() }} placeholder="Авто" />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Тип *</Label>
              <Select value={type} onValueChange={(v) => { setType(v as CounterpartyType); handleChange() }}>
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
              <Select value={legalForm} onValueChange={(v) => { setLegalForm(v as LegalForm); handleChange() }}>
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
              <Label className="text-xs text-muted-foreground">ОГРН</Label>
              <Input className="mt-1" value={ogrn} onChange={(e) => { setOgrn(e.target.value); handleChange() }} />
            </div>
          </div>

          <div className="grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-2">
            <div>
              <Label className="text-xs text-muted-foreground">Юридический адрес</Label>
              <Input className="mt-1" value={legalAddress} onChange={(e) => { setLegalAddress(e.target.value); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Фактический адрес</Label>
              <Input className="mt-1" value={actualAddress} onChange={(e) => { setActualAddress(e.target.value); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Телефон</Label>
              <Input className="mt-1" value={phone} onChange={(e) => { setPhone(e.target.value); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Email</Label>
              <Input className="mt-1" type="email" value={email} onChange={(e) => { setEmail(e.target.value); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Контактное лицо</Label>
              <Input className="mt-1" value={contactPerson} onChange={(e) => { setContactPerson(e.target.value); handleChange() }} />
            </div>
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Комментарий</Label>
            <Textarea rows={3} className="mt-1" value={comment} onChange={(e) => { setComment(e.target.value); handleChange() }} />
          </div>
        </div>
      </div>
    </div>
  )
}
