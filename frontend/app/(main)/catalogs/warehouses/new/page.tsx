"use client"

import { useState } from "react"
import { useRouter } from "next/navigation"
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
import { api } from "@/lib/api"
import type { WarehouseType } from "@/types/catalog"
import { WAREHOUSE_TYPE_LABELS } from "@/types/catalog"

export default function NewWarehousePage() {
  const router = useRouter()
  const { markDirty, markClean } = useTabDirty()

  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [name, setName] = useState("")
  const [code, setCode] = useState("")
  const [type, setType] = useState<WarehouseType>("main")
  const [address, setAddress] = useState("")
  const [isActive, setIsActive] = useState(true)
  const [allowNegativeStock, setAllowNegativeStock] = useState(false)
  const [isDefault, setIsDefault] = useState(false)
  const [organizationId, setOrganizationId] = useState("")
  const [description, setDescription] = useState("")

  const handleChange = () => markDirty()

  const handleSave = async (andClose: boolean) => {
    if (!name) { setError("Укажите наименование"); return }
    setSaving(true)
    setError(null)
    try {
      const created = await api.warehouses.create({
        name,
        code: code || undefined,
        type,
        address: address || null,
        isActive,
        allowNegativeStock,
        isDefault,
        organizationId: organizationId || undefined,
        description: description || null,
      })
      markClean()
      if (andClose) {
        router.push("/catalogs/warehouses")
      } else {
        router.replace(`/catalogs/warehouses/${created.id}`)
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
        title="Склад (создание)"
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
              <Input className="mt-1" value={code} onChange={(e) => { setCode(e.target.value); handleChange() }} placeholder="Авто" />
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
