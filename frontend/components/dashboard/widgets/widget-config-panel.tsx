"use client"
import type { JsonObject } from "@/lib/json-value"
import type { RegisteredWidgetDefinition } from "@/types/dashboard"
import { FieldGroup } from "@/components/ui/field"

/** Shows the widget's own config editor; a widget without one has no settings. */
export function WidgetConfigPanel({ definition, config, onConfigChange }: { definition: RegisteredWidgetDefinition; config: JsonObject; onConfigChange: (value: JsonObject) => void }) {
  const Editor = definition.configEditor
  return Editor ? <FieldGroup><Editor config={config} onConfigChange={onConfigChange} /></FieldGroup> : null
}
