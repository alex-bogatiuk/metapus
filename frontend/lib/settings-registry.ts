/**
 * Settings section definitions — metadata-driven registry.
 *
 * Each section describes a JSONB column in sys_settings:
 * its UI representation (title, icon, groups of fields)
 * and the mapping to the store section key.
 *
 * Adding a new settings section = adding one entry here + Go model + migration column.
 * No new .tsx files needed.
 */

import type { LucideIcon } from "lucide-react"
import type { SettingsSection } from "@/stores/useSettingsStore"
import {
  Hash,
  Gauge,
  Coins,
} from "lucide-react"

// ── Types ───────────────────────────────────────────────────────────────

export type FieldType = "switch" | "select" | "number" | "text" | "reference"

export interface SelectOption {
  value: string
  label: string
}

/** Definition of a single setting field. */
export interface SettingFieldDef {
  /** JSON key in the section object (e.g. "autoNumbering"). */
  key: string
  /** Display label (noun, no verbs per UX-editor). */
  label: string
  /** Short explanation of what this setting does. */
  description: string
  /** UI control type. */
  type: FieldType
  /** Options for "select" type. */
  options?: SelectOption[]
  apiEndpoint?: string
  /** Constraints for "number" type. */
  min?: number
  max?: number
  step?: number
  /** Hint suffix for number fields (e.g. "дн."). */
  suffix?: string
}

/** A logical group of fields within a section. */
export interface SettingGroupDef {
  /** Group title (e.g. "Документы"). */
  label: string
  /** Fields in this group. */
  fields: SettingFieldDef[]
}

/** A full settings section (maps to one JSONB column). */
export interface SettingSectionDef {
  /** Store section key (e.g. "numbering"). */
  id: SettingsSection
  /** Display title (noun, именительный падеж). */
  title: string
  /** Short description. */
  description: string
  /** Lucide icon. */
  icon: LucideIcon
  /** Semantic category for sidebar grouping. */
  category: "general" | "module"
  /** Groups of fields. */
  groups: SettingGroupDef[]
  /** Save hint shown in sticky footer. */
  saveHint?: string
}

// ── Registry ────────────────────────────────────────────────────────────

export const settingsSections: SettingSectionDef[] = [
  // ── General ──
  {
    id: "numbering",
    title: "Нумерация",
    description: "Автонумерация документов",
    icon: Hash,
    category: "general",
    groups: [
      {
        label: "Документы",
        fields: [
          {
            key: "autoNumbering",
            label: "Автонумерация",
            description: "Номера присваиваются автоматически при записи",
            type: "switch",
          },
          {
            key: "numberPrefix",
            label: "Префикс номера",
            description: "Добавляется перед порядковым номером (например, «ИНВ-»)",
            type: "text",
          },
        ],
      },
    ],
    saveHint: "Изменения применятся к новым документам",
  },
  {
    id: "performance",
    title: "Производительность",
    description: "Параллелизм и лимиты",
    icon: Gauge,
    category: "general",
    groups: [
      {
        label: "Пакетные операции",
        fields: [
          {
            key: "batchConcurrency",
            label: "Параллелизм",
            description: "Количество документов, обрабатываемых одновременно при массовых операциях",
            type: "number",
            min: 1,
            max: 10,
            step: 1,
          },
        ],
      },
    ],
    saveHint: "Изменения применятся к следующим пакетным операциям",
  },

  // ── Module-scoped ──
  {
    id: "exchangeRates",
    title: "Курсы валют",
    description: "Источник оценки балансов в USD",
    icon: Coins,
    category: "module",
    groups: [
      {
        label: "Оценка в USD",
        fields: [
          {
            key: "sourceId",
            label: "Источник CoinGecko",
            description: "Получение каждые 5 минут, актуальность 15 минут. Ключ задаётся в карточке источника курсов.",
            type: "reference",
            apiEndpoint: "/catalog/rate-sources",
          },
        ],
      },
    ],
  },
]
