"use client"

import { useCallback } from "react"
import { Monitor, Moon, Sun } from "lucide-react"
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
import { cn } from "@/lib/utils"
import { useSettingsStore } from "@/stores/useSettingsStore"
import { useTabDirty } from "@/hooks/useTabDirty"
import type { ThemeMode, DateFormat, NumberFormat } from "@/types/settings"

// ── Options ─────────────────────────────────────────────────────────────

const THEMES: { value: ThemeMode; label: string; icon: React.ElementType }[] = [
  { value: "light", label: "Светлая", icon: Sun },
  { value: "dark", label: "Тёмная", icon: Moon },
  { value: "system", label: "Системная", icon: Monitor },
]

const DATE_FORMATS: { value: DateFormat; label: string; example: string }[] = [
  { value: "dd.MM.yyyy", label: "ДД.ММ.ГГГГ", example: "25.02.2026" },
  { value: "yyyy-MM-dd", label: "ГГГГ-ММ-ДД", example: "2026-02-25" },
  { value: "MM/dd/yyyy", label: "ММ/ДД/ГГГГ", example: "02/25/2026" },
]

const NUMBER_FORMATS: {
  value: NumberFormat
  label: string
  example: string
}[] = [
  { value: "space", label: "Пробел", example: "1 234 567,89" },
  { value: "comma", label: "Запятая", example: "1,234,567.89" },
  { value: "none", label: "Без разделителя", example: "1234567,89" },
]

const PAGE_SIZES = [10, 25, 50, 100]

const LANGUAGES = [
  { value: "ru", label: "Русский" },
  { value: "en", label: "English" },
  { value: "kk", label: "Қазақша" },
]

// ── Field components ────────────────────────────────────────────────────

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

// ── Main Component ──────────────────────────────────────────────────────

export function InterfaceSection() {
  const { settings, updateInterface, isSaving } = useSettingsStore()
  const { markDirty } = useTabDirty()
  const ui = settings.interface

  const update = useCallback(
    (field: string, value: string | boolean | number) => {
      updateInterface({ [field]: value })
      markDirty()
    },
    [updateInterface, markDirty]
  )

  return (
    <div className="space-y-6">
      {/* Тема */}
      <div className="space-y-4">
        <h3 className="text-sm font-semibold text-foreground">Тема оформления</h3>
        <div className="grid grid-cols-3 gap-3">
          {THEMES.map((theme) => {
            const Icon = theme.icon
            const isActive = ui.theme === theme.value
            return (
              <button
                key={theme.value}
                onClick={() => update("theme", theme.value)}
                className={cn(
                  "flex flex-col items-center gap-2 rounded-lg border p-4 text-sm transition-colors",
                  isActive
                    ? "border-primary bg-primary/5 text-foreground"
                    : "border-border bg-card text-muted-foreground hover:border-primary/30 hover:text-foreground"
                )}
              >
                <Icon
                  className={cn(
                    "h-5 w-5",
                    isActive ? "text-primary" : "text-muted-foreground"
                  )}
                />
                <span className="font-medium">{theme.label}</span>
              </button>
            )
          })}
        </div>
      </div>

      <Separator />

      {/* Язык и форматы */}
      <div className="space-y-4">
        <h3 className="text-sm font-semibold text-foreground">Язык и форматы</h3>

        <SelectField
          label="Язык интерфейса"
          value={ui.language}
          onValueChange={(v) => update("language", v)}
          options={LANGUAGES}
        />

        <div className="grid grid-cols-[1fr_280px] items-start gap-4">
          <div>
            <p className="text-sm font-medium text-foreground">Формат даты</p>
            <p className="text-xs text-muted-foreground">
              Пример:{" "}
              {DATE_FORMATS.find((f) => f.value === ui.dateFormat)?.example}
            </p>
          </div>
          <Select
            value={ui.dateFormat}
            onValueChange={(v) => update("dateFormat", v)}
          >
            <SelectTrigger className="h-9 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {DATE_FORMATS.map((f) => (
                <SelectItem key={f.value} value={f.value}>
                  {f.label}
                  <span className="ml-2 text-muted-foreground">
                    ({f.example})
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="grid grid-cols-[1fr_280px] items-start gap-4">
          <div>
            <p className="text-sm font-medium text-foreground">
              Формат чисел
            </p>
            <p className="text-xs text-muted-foreground">
              Пример:{" "}
              {
                NUMBER_FORMATS.find((f) => f.value === ui.numberFormat)
                  ?.example
              }
            </p>
          </div>
          <Select
            value={ui.numberFormat}
            onValueChange={(v) => update("numberFormat", v)}
          >
            <SelectTrigger className="h-9 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {NUMBER_FORMATS.map((f) => (
                <SelectItem key={f.value} value={f.value}>
                  {f.label}
                  <span className="ml-2 text-muted-foreground">
                    ({f.example})
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      <Separator />

      {/* Таблицы и отображение */}
      <div className="space-y-4">
        <h3 className="text-sm font-semibold text-foreground">
          Таблицы и отображение
        </h3>

        <SelectField
          label="Строк на странице"
          description="Количество записей в таблицах по умолчанию"
          value={String(ui.pageSize)}
          onValueChange={(v) => update("pageSize", Number(v))}
          options={PAGE_SIZES.map((n) => ({
            value: String(n),
            label: String(n),
          }))}
        />

        <SwitchField
          label="Компактный режим"
          description="Уменьшенные отступы и размер шрифта в таблицах"
          checked={ui.compactMode}
          onCheckedChange={(v) => update("compactMode", v)}
        />

        <SwitchField
          label="Всплывающие подсказки"
          description="Показывать tooltip при наведении на элементы"
          checked={ui.showTooltips}
          onCheckedChange={(v) => update("showTooltips", v)}
        />

        <SwitchField
          label="Боковая панель свёрнута"
          description="Сворачивать меню навигации по умолчанию"
          checked={ui.sidebarCollapsed}
          onCheckedChange={(v) => update("sidebarCollapsed", v)}
        />
      </div>

      {/* Save button */}
      <div className="flex items-center gap-3 pt-2">
        <Button disabled={isSaving}>
          {isSaving ? "Сохранение..." : "Сохранить"}
        </Button>
        <p className="text-xs text-muted-foreground">
          Настройки применяются только к вашей учётной записи
        </p>
      </div>
    </div>
  )
}
