"use client"

import { cn } from "@/lib/utils"
import { getProfilePresets, type ProfilePreset } from "@/lib/security-registry"

export type { ProfilePreset } from "@/lib/security-registry"

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
        {getProfilePresets().map((preset) => {
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
