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
  Warehouse,
  ShoppingCart,
  PackageCheck,
} from "lucide-react"

// ── Types ───────────────────────────────────────────────────────────────

export type FieldType = "switch" | "select" | "number" | "text"

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
  /** Store section key (e.g. "warehouse"). */
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
            description: "Добавляется перед порядковым номером (например, «ПТ-»)",
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
    id: "warehouse",
    title: "Склад",
    description: "Учёт запасов и складские операции",
    icon: Warehouse,
    category: "module",
    groups: [
      {
        label: "Учёт запасов",
        fields: [
          {
            key: "inventoryMethod",
            label: "Метод списания",
            description: "Определяет порядок расчёта себестоимости при отгрузке",
            type: "select",
            options: [
              { value: "fifo", label: "FIFO (первый вошёл — первый вышел)" },
              { value: "weighted_average", label: "Средневзвешенная стоимость" },
            ],
          },
          {
            key: "negativeStockControl",
            label: "Контроль отрицательных остатков",
            description: "Запрещает проведение документов при нехватке на складе",
            type: "switch",
          },
        ],
      },
      {
        label: "Приёмка",
        fields: [
          {
            key: "autoPostReceipts",
            label: "Автоматическое проведение приходов",
            description: "Документы приёмки проводятся автоматически при записи",
            type: "switch",
          },
        ],
      },
    ],
    saveHint: "Изменения повлияют на новые складские операции",
  },
  {
    id: "sales",
    title: "Продажи",
    description: "Коммерческая политика и отгрузка",
    icon: ShoppingCart,
    category: "module",
    groups: [
      {
        label: "Условия оплаты",
        fields: [
          {
            key: "defaultPaymentTermDays",
            label: "Срок оплаты по умолчанию",
            description: "Количество дней отсрочки для новых счетов",
            type: "number",
            min: 0,
            max: 365,
            step: 1,
            suffix: "дн.",
          },
        ],
      },
      {
        label: "Резервирование",
        fields: [
          {
            key: "autoReserveStock",
            label: "Автоматическое резервирование",
            description: "Остатки резервируются при подтверждении заказа покупателя",
            type: "switch",
          },
        ],
      },
    ],
    saveHint: "Изменения повлияют на новые заказы и счета",
  },
  {
    id: "purchasing",
    title: "Закупки",
    description: "Условия оплаты и согласование",
    icon: PackageCheck,
    category: "module",
    groups: [
      {
        label: "Условия оплаты",
        fields: [
          {
            key: "defaultPaymentTermDays",
            label: "Срок оплаты по умолчанию",
            description: "Количество дней отсрочки для новых заказов поставщику",
            type: "number",
            min: 0,
            max: 365,
            step: 1,
            suffix: "дн.",
          },
        ],
      },
      {
        label: "Согласование",
        fields: [
          {
            key: "requireApproval",
            label: "Согласование заказов",
            description: "Заказы поставщику требуют подтверждения руководителя",
            type: "switch",
          },
        ],
      },
    ],
    saveHint: "Изменения повлияют на новые заказы поставщику",
  },
]
