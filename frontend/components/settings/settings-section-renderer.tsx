"use client"

import type { SettingValue } from "@/lib/schemas/settings"

/**
 * SettingsSectionRenderer — generic metadata-driven renderer for settings sections.
 *
 * Renders groups of fields from a SettingSectionDef definition.
 * Reads values from useSettingsStore and writes via updateSection.
 * Includes a sticky save footer.
 *
 * Usage:
 *   <SettingsSectionRenderer section={sectionDef} />
 */

import { useCallback } from "react"
import { ReferenceField } from "@/components/shared/reference-field"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useSettingsStore } from "@/stores/useSettingsStore"
import { useTabDirty } from "@/hooks/useTabDirty"
import type { SettingSectionDef, SettingFieldDef } from "@/lib/settings-registry"

interface SettingsSectionRendererProps {
  section: SettingSectionDef
}

export function SettingsSectionRenderer({ section }: SettingsSectionRendererProps) {
  const settings = useSettingsStore((s) => s.settings)
  const updateSection = useSettingsStore((s) => s.updateSection)
  const saveSection = useSettingsStore((s) => s.saveSection)
  const isSaving = useSettingsStore((s) => s.isSaving)
  const { markDirty } = useTabDirty()

  // Get current values for this section.
  // Spread creates a plain object with an index signature, satisfying JsonObject.
  const sectionData: Record<string, SettingValue> = { ...settings[section.id] }

  const handleChange = useCallback(
    (key: string, value: SettingValue) => {
      updateSection(section.id, key, value)
      markDirty()
    },
    [section.id, updateSection, markDirty]
  )

  const handleSave = useCallback(() => {
    saveSection(section.id)
  }, [section.id, saveSection])

  return (
    <div className="space-y-6">
      {section.groups.map((group, gi) => (
        <div key={gi} className="space-y-4">
          <h3 className="text-sm font-semibold text-foreground">
            {group.label}
          </h3>
          <div className="space-y-3">
            {group.fields.map((field) => (
              <FieldRow
                key={field.key}
                field={field}
                value={sectionData[field.key]}
                onChange={handleChange}
              />
            ))}
          </div>
        </div>
      ))}

      {/* Spacer */}
      <div className="h-16" />

      {/* Sticky Save footer */}
      <div data-material="workspace" className="sticky bottom-0 -mx-6 flex flex-wrap items-center gap-x-3 gap-y-2 border-t px-6 py-3">
        <Button onClick={handleSave} disabled={isSaving}>
          {isSaving ? "Сохранение…" : "Сохранить"}
        </Button>
        {section.saveHint && (
          <p className="text-xs text-muted-foreground">
            {section.saveHint}
          </p>
        )}
      </div>
    </div>
  )
}

// ── Field renderers ─────────────────────────────────────────────────────

interface FieldRowProps {
  field: SettingFieldDef
  value: SettingValue | undefined
  onChange: (key: string, value: SettingValue) => void
}

function FieldRow({ field, value, onChange }: FieldRowProps) {
  switch (field.type) {
    case "reference":
      return <div className="grid grid-cols-1 items-center gap-3 sm:grid-cols-[minmax(0,1fr)_280px]"><FieldLabel field={field} /><ReferenceField value={typeof value === "string" ? value : ""} apiEndpoint={field.apiEndpoint!} onChange={id => onChange(field.key, id || null)} /></div>

    case "switch":
      return (
        <div className="grid grid-cols-1 items-center gap-3 sm:grid-cols-[minmax(0,1fr)_280px] sm:gap-4">
          <FieldLabel field={field} />
          <div className="flex justify-end">
            <Switch
              checked={Boolean(value)}
              onCheckedChange={(v) => onChange(field.key, v)}
            />
          </div>
        </div>
      )

    case "select":
      return (
        <div className="grid grid-cols-1 items-center gap-3 sm:grid-cols-[minmax(0,1fr)_280px] sm:gap-4">
          <FieldLabel field={field} />
          <Select
            value={String(value ?? "")}
            onValueChange={(v) => onChange(field.key, v)}
          >
            <SelectTrigger className="h-9 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {field.options?.map((opt) => (
                <SelectItem key={opt.value} value={opt.value}>
                  {opt.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )

    case "number":
      return (
        <div className="grid grid-cols-1 items-center gap-3 sm:grid-cols-[minmax(0,1fr)_280px] sm:gap-4">
          <FieldLabel field={field} />
          <div className="flex items-center gap-2">
            <Input
              type="number"
              value={String(value ?? "")}
              onChange={(e) => onChange(field.key, Number(e.target.value))}
              min={field.min}
              max={field.max}
              step={field.step}
              className="h-9 text-sm tabular-nums"
            />
            {field.suffix && (
              <span className="text-xs text-muted-foreground shrink-0">
                {field.suffix}
              </span>
            )}
          </div>
        </div>
      )

    case "text":
      return (
        <div className="grid grid-cols-1 items-start gap-3 sm:grid-cols-[minmax(0,1fr)_280px] sm:gap-4">
          <FieldLabel field={field} />
          <Input
            value={String(value ?? "")}
            onChange={(e) => onChange(field.key, e.target.value)}
            className="h-9 text-sm"
          />
        </div>
      )
  }
}

function FieldLabel({ field }: { field: SettingFieldDef }) {
  return (
    <div>
      <p className="text-sm font-medium text-foreground">{field.label}</p>
      <p className="text-xs text-muted-foreground">{field.description}</p>
    </div>
  )
}
