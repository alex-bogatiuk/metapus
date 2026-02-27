"use client"

import { useState, useEffect } from "react"
import { useRouter, useParams } from "next/navigation"
import { Loader2 } from "lucide-react"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Switch } from "@/components/ui/switch"
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
import type { WarehouseResponse, WarehouseType } from "@/types/catalog"
import { WAREHOUSE_TYPE_LABELS } from "@/types/catalog"

export default function EditWarehousePage() {
  const router = useRouter()
  const params = useParams<{ id: string }>()
  const { markDirty, markClean } = useTabDirty()

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [doc, setDoc] = useState<WarehouseResponse | null>(null)

  const [name, setName] = useState("")
  const [code, setCode] = useState("")
  const [type, setType] = useState<WarehouseType>("main")
  const [address, setAddress] = useState("")
  const [isActive, setIsActive] = useState(true)
  const [allowNegativeStock, setAllowNegativeStock] = useState(false)
  const [isDefault, setIsDefault] = useState(false)
  const [organizationId, setOrganizationId] = useState("")
  const [description, setDescription] = useState("")
  const [version, setVersion] = useState(0)
  useTabTitle(name || undefined, "Склад")

  useEffect(() => {
    if (!params.id) return
    setLoading(true)
    api.warehouses.get(params.id).then((d) => {
      setDoc(d)
      setName(d.name)
      setCode(d.code)
      setType(d.type)
      setAddress(d.address || "")
      setIsActive(d.isActive)
      setAllowNegativeStock(d.allowNegativeStock)
      setIsDefault(d.isDefault)
      setOrganizationId(d.organizationId || "")
      setDescription(d.description || "")
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
      const updated = await api.warehouses.update(params.id, {
        name,
        code,
        type,
        address: address || null,
        isActive,
        allowNegativeStock,
        isDefault,
        organizationId: organizationId || undefined,
        description: description || null,
        version,
      })
      setDoc(updated)
      setVersion(updated.version)
      markClean()
      if (andClose) router.push("/catalogs/warehouses")
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
        title={`Склад: ${doc?.name || ""}`}
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
        backHref="/catalogs/warehouses"
        onClose={() => router.push("/catalogs/warehouses")}
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
              <Select value={type} onValueChange={(v) => { setType(v as WarehouseType); handleChange() }}>
                <SelectTrigger className="mt-1"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {Object.entries(WAREHOUSE_TYPE_LABELS).map(([k, label]) => (
                    <SelectItem key={k} value={k}>{label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Организация (ID)</Label>
              <Input className="mt-1" value={organizationId} onChange={(e) => { setOrganizationId(e.target.value); handleChange() }} />
            </div>
            <div className="md:col-span-2">
              <Label className="text-xs text-muted-foreground">Адрес</Label>
              <Input className="mt-1" value={address} onChange={(e) => { setAddress(e.target.value); handleChange() }} />
            </div>
          </div>

          <div className="flex flex-wrap gap-6">
            <div className="flex items-center gap-2">
              <Switch checked={isActive} onCheckedChange={(v) => { setIsActive(v); handleChange() }} />
              <Label className="text-xs">Активен</Label>
            </div>
            <div className="flex items-center gap-2">
              <Switch checked={allowNegativeStock} onCheckedChange={(v) => { setAllowNegativeStock(v); handleChange() }} />
              <Label className="text-xs">Отрицательные остатки</Label>
            </div>
            <div className="flex items-center gap-2">
              <Switch checked={isDefault} onCheckedChange={(v) => { setIsDefault(v); handleChange() }} />
              <Label className="text-xs">По умолчанию</Label>
            </div>
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Описание</Label>
            <Textarea rows={3} className="mt-1" value={description} onChange={(e) => { setDescription(e.target.value); handleChange() }} />
          </div>
        </div>
      </div>
    </div>
  )
}
