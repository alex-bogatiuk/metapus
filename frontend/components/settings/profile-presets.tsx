"use client"

import { ShieldCheck, Eye, Briefcase, Calculator, Store } from "lucide-react"
import { cn } from "@/lib/utils"

// ── Preset definitions ───────────────────────────────────────────────

export interface ProfilePreset {
  code: string
  name: string
  description: string
  icon: React.ElementType
  dimensions: Record<string, string[]>
  entityDimensions?: Record<string, Record<string, string[]>>
  fieldPolicies: {
    entityName: string
    action: "read" | "write"
    allowedFields: string[]
    tableParts?: Record<string, string[]>
  }[]
}

export const PROFILE_PRESETS: ProfilePreset[] = [
  {
    code: "viewer",
    name: "Только просмотр",
    description: "Чтение без финансовых полей (суммы, комиссии)",
    icon: Eye,
    dimensions: {},
    fieldPolicies: [
      {
        entityName: "CryptoInvoice",
        action: "read",
        allowedFields: ["*", "-expected_amount", "-received_amount"],
      },
      {
        entityName: "CryptoPayment",
        action: "read",
        allowedFields: ["*", "-amount", "-fee_amount"],
      },
    ],
  },
  {
    code: "manager_limited",
    name: "Менеджер",
    description: "CRUD без проведения, ограниченные организации",
    icon: Briefcase,
    dimensions: { organization: [] },
    fieldPolicies: [],
  },
  {
    code: "merchant_operator",
    name: "Оператор мерчанта",
    description: "Полный доступ к операциям, ограниченные мерчанты",
    icon: Store,
    dimensions: { merchant: [] },
    fieldPolicies: [],
  },
  {
    code: "accountant",
    name: "Бухгалтер",
    description: "Полный доступ к документам и отчётам",
    icon: Calculator,
    dimensions: {},
    fieldPolicies: [],
  },
]

// ── Component ────────────────────────────────────────────────────────

interface ProfilePresetPickerProps {
  onSelect: (preset: ProfilePreset) => void
  onSkip: () => void
}

export function ProfilePresetPicker({ onSelect, onSkip }: ProfilePresetPickerProps) {
  return (
    <div className="space-y-4">
      <div>
        <h3 className="text-sm font-medium">Выберите шаблон</h3>
        <p className="text-xs text-muted-foreground mt-0.5">
          Начните с готового шаблона или создайте профиль с нуля
        </p>
      </div>

      <div className="grid grid-cols-2 gap-3">
        {PROFILE_PRESETS.map((preset) => {
          const Icon = preset.icon
          return (
            <button
              key={preset.code}
              onClick={() => onSelect(preset)}
              className="group rounded-lg border bg-card p-3 text-left transition-colors hover:border-primary/30 hover:bg-muted/20"
            >
              <div className="flex items-center gap-2 mb-1.5">
                <Icon className="h-4 w-4 text-muted-foreground group-hover:text-primary transition-colors" />
                <span className="text-sm font-medium">{preset.name}</span>
              </div>
              <p className="text-[11px] text-muted-foreground leading-relaxed">
                {preset.description}
              </p>
            </button>
          )
        })}
      </div>

      <button
        onClick={onSkip}
        className="w-full text-center text-xs text-muted-foreground hover:text-foreground transition-colors py-2"
      >
        Создать с нуля →
      </button>
    </div>
  )
}
