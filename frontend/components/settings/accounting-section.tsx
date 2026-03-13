"use client"

import { useCallback } from "react"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { Separator } from "@/components/ui/separator"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useSettingsStore } from "@/stores/useSettingsStore"
import { useTabDirty } from "@/hooks/useTabDirty"
import type { TaxSystem, InventoryMethod, VatRate } from "@/types/settings"

const TAX_SYSTEMS: { value: TaxSystem; label: string }[] = [
  { value: "osno", label: "ОСНО — Общая система" },
  { value: "usn_income", label: "УСН — Доходы (6%)" },
  { value: "usn_income_expense", label: "УСН — Доходы минус расходы (15%)" },
  { value: "envd", label: "ЕНВД" },
  { value: "patent", label: "Патент" },
]

const INVENTORY_METHODS: { value: InventoryMethod; label: string }[] = [
  { value: "fifo", label: "FIFO — первым пришёл, первым ушёл" },
  { value: "average", label: "По средней стоимости" },
  { value: "specific", label: "По стоимости единицы" },
]

const VAT_RATES: { value: VatRate; label: string }[] = [
  { value: "20", label: "20%" },
  { value: "10", label: "10%" },
  { value: "0", label: "0%" },
  { value: "none", label: "Без НДС" },
]

const CURRENCIES = [
  { value: "RUB", label: "₽ Российский рубль (RUB)" },
  { value: "USD", label: "$ Доллар США (USD)" },
  { value: "EUR", label: "€ Евро (EUR)" },
  { value: "KZT", label: "₸ Казахстанский тенге (KZT)" },
]

interface SelectFieldProps {
  label: string
  description?: string
  value: string
  onValueChange: (value: string) => void
  options: { value: string; label: string }[]
}

function SelectField({
  label,
  description,
  value,
  onValueChange,
  options,
}: SelectFieldProps) {
  return (
    <div className="grid grid-cols-[1fr_280px] items-start gap-4">
      <div>
        <p className="text-sm font-medium text-foreground">{label}</p>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
      <Select value={value} onValueChange={onValueChange}>
        <SelectTrigger className="h-9 text-sm">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {options.map((opt) => (
            <SelectItem key={opt.value} value={opt.value}>
              {opt.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

interface SwitchFieldProps {
  label: string
  description?: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
}

function SwitchField({
  label,
  description,
  checked,
  onCheckedChange,
}: SwitchFieldProps) {
  return (
    <div className="grid grid-cols-[1fr_280px] items-center gap-4">
      <div>
        <p className="text-sm font-medium text-foreground">{label}</p>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
      <div className="flex justify-end">
        <Switch checked={checked} onCheckedChange={onCheckedChange} />
      </div>
    </div>
  )
}

export function AccountingSection() {
  const { settings, updateAccounting, isSaving } = useSettingsStore()
  const { markDirty } = useTabDirty()
  const acc = settings.accounting

  const update = useCallback(
    (field: string, value: string | boolean) => {
      updateAccounting({ [field]: value })
      markDirty()
    },
    [updateAccounting, markDirty]
  )

  return (
    <div className="space-y-6">
      {/* Валюта и учёт */}
      <div className="space-y-4">
        <h3 className="text-sm font-semibold text-foreground">Основные параметры</h3>

        <SelectField
          label="Валюта по умолчанию"
          description="Основная валюта для всех операций"
          value={acc.defaultCurrency}
          onValueChange={(v) => update("defaultCurrency", v)}
          options={CURRENCIES}
        />

        <SelectField
          label="Система налогообложения"
          description="Влияет на формирование отчётности"
          value={acc.taxSystem}
          onValueChange={(v) => update("taxSystem", v)}
          options={TAX_SYSTEMS}
        />

        <SelectField
          label="Метод списания запасов"
          description="Определяет порядок оценки себестоимости"
          value={acc.inventoryMethod}
          onValueChange={(v) => update("inventoryMethod", v)}
          options={INVENTORY_METHODS}
        />
      </div>

      <Separator />

      {/* НДС */}
      <div className="space-y-4">
        <h3 className="text-sm font-semibold text-foreground">НДС</h3>

        <SwitchField
          label="Плательщик НДС"
          description="Включите, если организация является плательщиком НДС"
          checked={acc.vatPayer}
          onCheckedChange={(v) => update("vatPayer", v)}
        />

        {acc.vatPayer && (
          <SelectField
            label="Ставка НДС по умолчанию"
            description="Ставка, подставляемая в новые документы"
            value={acc.defaultVatRate}
            onValueChange={(v) => update("defaultVatRate", v)}
            options={VAT_RATES}
          />
        )}
      </div>

      <Separator />

      {/* Нумерация */}
      <div className="space-y-4">
        <h3 className="text-sm font-semibold text-foreground">Нумерация документов</h3>

        <SwitchField
          label="Автонумерация"
          description="Автоматически присваивать номера новым документам"
          checked={acc.autoNumbering}
          onCheckedChange={(v) => update("autoNumbering", v)}
        />

        {acc.autoNumbering && (
          <div className="grid grid-cols-[1fr_280px] items-start gap-4">
            <div>
              <p className="text-sm font-medium text-foreground">
                Префикс номера
              </p>
              <p className="text-xs text-muted-foreground">
                Добавляется перед порядковым номером (например, &laquo;ПТ-&raquo;)
              </p>
            </div>
            <Input
              value={acc.numberPrefix}
              onChange={(e) => update("numberPrefix", e.target.value)}
              placeholder="ПТ-"
              className="h-9 text-sm"
            />
          </div>
        )}
      </div>

      <Separator />

      {/* Финансовый год */}
      <div className="space-y-4">
        <h3 className="text-sm font-semibold text-foreground">Финансовый год</h3>

        <div className="grid grid-cols-[1fr_280px] items-start gap-4">
          <div>
            <p className="text-sm font-medium text-foreground">
              Начало финансового года
            </p>
            <p className="text-xs text-muted-foreground">
              Дата начала отчётного периода (ДД-ММ)
            </p>
          </div>
          <Input
            value={acc.fiscalYearStart}
            onChange={(e) => update("fiscalYearStart", e.target.value)}
            placeholder="01-01"
            className="h-9 text-sm"
          />
        </div>
      </div>

      {/* Spacer so content doesn't hide behind sticky footer */}
      <div className="h-16" />

      {/* Sticky Save footer */}
      <div className="sticky bottom-0 -mx-6 border-t bg-background px-6 py-3 flex items-center gap-3">
        <Button disabled={isSaving}>
          {isSaving ? "Сохранение..." : "Сохранить"}
        </Button>
        <p className="text-xs text-muted-foreground">
          Изменения применятся к новым документам
        </p>
      </div>
    </div>
  )
}
