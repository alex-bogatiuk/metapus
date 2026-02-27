"use client"

import { useState, useEffect } from "react"
import { useRouter, useParams } from "next/navigation"
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
import { useTabDirty } from "@/hooks/useTabDirty"
import { useTabTitle } from "@/hooks/useTabTitle"
import { api } from "@/lib/api"
import type { CounterpartyResponse, CounterpartyType, LegalForm } from "@/types/catalog"
import { COUNTERPARTY_TYPE_LABELS, LEGAL_FORM_LABELS } from "@/types/catalog"

export default function EditCounterpartyPage() {
  const router = useRouter()
  const params = useParams<{ id: string }>()
  const { markDirty, markClean } = useTabDirty()

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [doc, setDoc] = useState<CounterpartyResponse | null>(null)

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
  const [version, setVersion] = useState(0)
  useTabTitle(name || undefined, "Контрагент")

  useEffect(() => {
    if (!params.id) return
    setLoading(true)
    api.counterparties.get(params.id).then((d) => {
      setDoc(d)
      setName(d.name)
      setCode(d.code)
      setType(d.type)
      setLegalForm(d.legalForm)
      setFullName(d.fullName || "")
      setInn(d.inn || "")
      setKpp(d.kpp || "")
      setOgrn(d.ogrn || "")
      setLegalAddress(d.legalAddress || "")
      setActualAddress(d.actualAddress || "")
      setPhone(d.phone || "")
      setEmail(d.email || "")
      setContactPerson(d.contactPerson || "")
      setComment(d.comment || "")
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
      const updated = await api.counterparties.update(params.id, {
        name,
        code,
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
        version,
      })
      setDoc(updated)
      setVersion(updated.version)
      markClean()
      if (andClose) router.push("/catalogs/counterparties")
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
        title={`Контрагент: ${doc?.name || ""}`}
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
              <Input className="mt-1" value={code} onChange={(e) => { setCode(e.target.value); handleChange() }} />
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
