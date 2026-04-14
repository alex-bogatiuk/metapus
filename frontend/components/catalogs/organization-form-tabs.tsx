"use client"

import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Separator } from "@/components/ui/separator"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { ReferenceField } from "@/components/shared/reference-field"
import { Building2, Upload } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  TAX_SYSTEM_LABELS,
  INVENTORY_METHOD_LABELS,
} from "@/types/catalog"
import type { TaxSystem, InventoryMethod } from "@/types/catalog"

// ── Shared form state ────────────────────────────────────────────────────

export interface OrganizationFormState {
  // Basic
  name: string
  code: string
  fullName: string
  // Requisites
  inn: string
  kpp: string
  ogrn: string
  // Addresses
  legalAddress: string
  actualAddress: string
  // Contacts
  phone: string
  email: string
  website: string
  // Currency
  baseCurrencyId: string
  baseCurrencyName: string
  isDefault: boolean
  // Responsible persons
  director: string
  accountant: string
  logoUrl: string
  // Accounting policy
  taxSystem: string
  vatPayer: boolean
  defaultVatRateId: string
  defaultVatRateName: string
  inventoryMethod: string
  fiscalYearStart: string
  // Meta
  version: number
}

export const INITIAL_ORG_STATE: OrganizationFormState = {
  name: "",
  code: "",
  fullName: "",
  inn: "",
  kpp: "",
  ogrn: "",
  legalAddress: "",
  actualAddress: "",
  phone: "",
  email: "",
  website: "",
  baseCurrencyId: "",
  baseCurrencyName: "",
  isDefault: false,
  director: "",
  accountant: "",
  logoUrl: "",
  taxSystem: "osno",
  vatPayer: false,
  defaultVatRateId: "",
  defaultVatRateName: "",
  inventoryMethod: "fifo",
  fiscalYearStart: "01-01",
  version: 0,
}

// ── Helper ───────────────────────────────────────────────────────────────

function Field({
  label,
  value,
  onChange,
  placeholder,
  colSpan,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  colSpan?: boolean
}) {
  return (
    <div className={colSpan ? "sm:col-span-2" : undefined}>
      <Label className="mb-1.5 text-xs text-muted-foreground">{label}</Label>
      <Input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="text-sm"
      />
    </div>
  )
}

// ── Tabs ──────────────────────────────────────────────────────────────────

interface OrganizationFormTabsProps {
  f: OrganizationFormState
  update: (patch: Partial<OrganizationFormState>) => void
  onChange: () => void
}

export function OrganizationFormTabs({ f, update, onChange }: OrganizationFormTabsProps) {
  const set = (patch: Partial<OrganizationFormState>) => {
    update(patch)
    onChange()
  }

  return (
    <Tabs defaultValue="main" className="w-full">
      <TabsList>
        <TabsTrigger value="main">Основные</TabsTrigger>
        <TabsTrigger value="accounting">Учётная политика</TabsTrigger>
        <TabsTrigger value="contacts">Контакты</TabsTrigger>
        <TabsTrigger value="responsible">Ответственные</TabsTrigger>
      </TabsList>

      {/* ── Tab: Основные ──────────────────────────────────────── */}
      <TabsContent value="main" className="mt-4 space-y-6">
        {/* Name / Code / FullName */}
        <div className="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2">
          <Field label="Наименование *" value={f.name} onChange={(v) => set({ name: v })} />
          <Field label="Код" value={f.code} onChange={(v) => set({ code: v })} placeholder="Авто" />
          <Field label="Полное наименование" value={f.fullName} onChange={(v) => set({ fullName: v })} colSpan />
        </div>

        <Separator />

        {/* Requisites */}
        <div className="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-4">
          <Field label="ИНН" value={f.inn} onChange={(v) => set({ inn: v })} />
          <Field label="КПП" value={f.kpp} onChange={(v) => set({ kpp: v })} />
          <Field label="ОГРН" value={f.ogrn} onChange={(v) => set({ ogrn: v })} />
          <div>
            <Label className="mb-1.5 text-xs text-muted-foreground">Базовая валюта *</Label>
            <ReferenceField
              value={f.baseCurrencyId}
              displayName={f.baseCurrencyName}
              apiEndpoint="/catalog/currencies"
              placeholder="Выберите валюту"
              onChange={(id, name) => set({ baseCurrencyId: id, baseCurrencyName: name })}
            />
          </div>
        </div>

        <Separator />

        {/* Addresses */}
        <div className="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2">
          <Field label="Юридический адрес" value={f.legalAddress} onChange={(v) => set({ legalAddress: v })} colSpan />
          <Field label="Фактический адрес" value={f.actualAddress} onChange={(v) => set({ actualAddress: v })} colSpan />
        </div>

        <Separator />

        <div className="flex items-center gap-2">
          <Switch checked={f.isDefault} onCheckedChange={(v) => set({ isDefault: v })} />
          <Label className="text-xs">Основная организация</Label>
        </div>
      </TabsContent>

      {/* ── Tab: Учётная политика ──────────────────────────────── */}
      <TabsContent value="accounting" className="mt-4 space-y-6">
        <div className="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2">
          {/* Tax system */}
          <div>
            <Label className="mb-1.5 text-xs text-muted-foreground">Система налогообложения</Label>
            <Select value={f.taxSystem} onValueChange={(v) => set({ taxSystem: v })}>
              <SelectTrigger className="text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {Object.entries(TAX_SYSTEM_LABELS).map(([value, label]) => (
                  <SelectItem key={value} value={value}>{label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Inventory method */}
          <div>
            <Label className="mb-1.5 text-xs text-muted-foreground">Метод учёта запасов</Label>
            <Select value={f.inventoryMethod} onValueChange={(v) => set({ inventoryMethod: v as InventoryMethod })}>
              <SelectTrigger className="text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {Object.entries(INVENTORY_METHOD_LABELS).map(([value, label]) => (
                  <SelectItem key={value} value={value}>{label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        <Separator />

        {/* VAT */}
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <Switch checked={f.vatPayer} onCheckedChange={(v) => set({ vatPayer: v })} />
            <Label className="text-xs">Плательщик НДС</Label>
          </div>

          {f.vatPayer && (
            <div className="max-w-xs">
              <Label className="mb-1.5 text-xs text-muted-foreground">Ставка НДС по умолчанию</Label>
              <ReferenceField
                value={f.defaultVatRateId}
                displayName={f.defaultVatRateName}
                apiEndpoint="/catalog/vat-rates"
                placeholder="Выберите ставку"
                onChange={(id, name) => set({ defaultVatRateId: id, defaultVatRateName: name })}
              />
            </div>
          )}
        </div>

        <Separator />

        <div className="max-w-xs">
          <Field label="Начало фискального года" value={f.fiscalYearStart} onChange={(v) => set({ fiscalYearStart: v })} placeholder="01-01" />
        </div>
      </TabsContent>

      {/* ── Tab: Контакты ──────────────────────────────────────── */}
      <TabsContent value="contacts" className="mt-4 space-y-6">
        <div className="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2">
          <Field label="Телефон" value={f.phone} onChange={(v) => set({ phone: v })} placeholder="+7 (999) 123-45-67" />
          <Field label="Email" value={f.email} onChange={(v) => set({ email: v })} placeholder="info@company.ru" />
          <Field label="Веб-сайт" value={f.website} onChange={(v) => set({ website: v })} placeholder="https://company.ru" />
        </div>
      </TabsContent>

      {/* ── Tab: Ответственные ─────────────────────────────────── */}
      <TabsContent value="responsible" className="mt-4 space-y-6">
        {/* Logo */}
        <div className="flex items-start gap-4">
          <div className="flex h-20 w-20 shrink-0 items-center justify-center rounded-lg border-2 border-dashed border-border bg-muted/40">
            {f.logoUrl ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img src={f.logoUrl} alt="Логотип" className="h-full w-full rounded-lg object-contain" />
            ) : (
              <Building2 className="h-8 w-8 text-muted-foreground/50" />
            )}
          </div>
          <div className="space-y-1.5">
            <p className="text-sm font-medium text-foreground">Логотип компании</p>
            <p className="text-xs text-muted-foreground">PNG, JPG или SVG, не более 2 МБ</p>
            <Button variant="outline" size="sm">
              <Upload className="mr-1.5 h-3.5 w-3.5" />
              Загрузить
            </Button>
          </div>
        </div>

        <Separator />

        <div className="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2">
          <Field label="Руководитель" value={f.director} onChange={(v) => set({ director: v })} placeholder="Иванов Иван Иванович" />
          <Field label="Главный бухгалтер" value={f.accountant} onChange={(v) => set({ accountant: v })} placeholder="Петрова Мария Сергеевна" />
        </div>
      </TabsContent>
    </Tabs>
  )
}
