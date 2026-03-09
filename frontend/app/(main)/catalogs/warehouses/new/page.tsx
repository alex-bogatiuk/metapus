"use client"

import { useRouter } from "next/navigation"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { ReferenceField } from "@/components/shared/reference-field"
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
import { useCatalogForm } from "@/hooks/useCatalogForm"
import { api } from "@/lib/api"
import type { WarehouseType } from "@/types/catalog"
import { WAREHOUSE_TYPE_LABELS } from "@/types/catalog"

interface WarehouseFormState {
  name: string
  code: string
  type: WarehouseType
  address: string
  isActive: boolean
  allowNegativeStock: boolean
  isDefault: boolean
  organizationId: string
  organizationName: string
  description: string
  [key: string]: unknown
}

const INITIAL_STATE: WarehouseFormState = {
  name: "",
  code: "",
  type: "main",
  address: "",
  isActive: true,
  allowNegativeStock: false,
  isDefault: false,
  organizationId: "",
  organizationName: "",
  description: "",
}

export default function NewWarehousePage() {
  const router = useRouter()
  const { f, update, handleChange, handleSave, saving, error } = useCatalogForm({
    entityName: "Склад",
    initialState: INITIAL_STATE,
    api: { create: api.warehouses.create },
    listPath: "/catalogs/warehouses",
    validate: (s) => !s.name ? "Укажите наименование" : null,
    mapToCreate: (s) => ({
      name: s.name,
      code: s.code || undefined,
      type: s.type,
      address: s.address || null,
      isActive: s.isActive,
      allowNegativeStock: s.allowNegativeStock,
      isDefault: s.isDefault,
      organizationId: s.organizationId || undefined,
      description: s.description || null,
    }),
  })

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
              <Input className="mt-1" value={f.name} onChange={(e) => { update({ name: e.target.value }); handleChange() }} />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Код</Label>
              <Input className="mt-1" value={f.code} onChange={(e) => { update({ code: e.target.value }); handleChange() }} placeholder="Авто" />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Тип *</Label>
              <Select value={f.type} onValueChange={(v) => { update({ type: v as WarehouseType }); handleChange() }}>
                <SelectTrigger className="mt-1"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {Object.entries(WAREHOUSE_TYPE_LABELS).map(([k, label]) => (
                    <SelectItem key={k} value={k}>{label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Организация</Label>
              <div className="mt-1">
                <ReferenceField
                  value={f.organizationId}
                  displayName={f.organizationName}
                  apiEndpoint="/catalog/organizations"
                  placeholder="Выберите организацию"
                  onChange={(id, name) => { update({ organizationId: id, organizationName: name }); handleChange() }}
                />
              </div>
            </div>
            <div className="md:col-span-2">
              <Label className="text-xs text-muted-foreground">Адрес</Label>
              <Input className="mt-1" value={f.address} onChange={(e) => { update({ address: e.target.value }); handleChange() }} />
            </div>
          </div>

          <div className="flex flex-wrap gap-6">
            <div className="flex items-center gap-2">
              <Switch checked={f.isActive} onCheckedChange={(v) => { update({ isActive: v }); handleChange() }} />
              <Label className="text-xs">Активен</Label>
            </div>
            <div className="flex items-center gap-2">
              <Switch checked={f.allowNegativeStock} onCheckedChange={(v) => { update({ allowNegativeStock: v }); handleChange() }} />
              <Label className="text-xs">Отрицательные остатки</Label>
            </div>
            <div className="flex items-center gap-2">
              <Switch checked={f.isDefault} onCheckedChange={(v) => { update({ isDefault: v }); handleChange() }} />
              <Label className="text-xs">По умолчанию</Label>
            </div>
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Описание</Label>
            <Textarea rows={3} className="mt-1" value={f.description} onChange={(e) => { update({ description: e.target.value }); handleChange() }} />
          </div>
        </div>
      </div>
    </div>
  )
}
