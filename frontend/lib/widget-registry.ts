/**
 * Widget Registry — реестр виджетов дашборда.
 *
 * The platform defines the dashboard; configurations register its widgets.
 * Adding a widget:
 *  1. Define its Zod config schema next to the widget.
 *  2. Create a renderer that accepts WidgetRenderProps<YourConfig> and uses
 *     useWidgetData() for data fetching; delegate drawing to a presenter.
 *  3. Optionally add a config editor (WidgetConfigEditorProps<YourConfig>).
 *  4. Call registerWidget() from the configuration's registration.
 */

import { createElement } from "react"
import type { RegisteredWidgetDefinition, WidgetConfigEditorProps, WidgetDefinition, WidgetRuntimeProps, WidgetType } from "@/types/dashboard"
import { jsonObjectSnapshot, type JsonObject } from "@/lib/json-value"

// ── defineWidget — type-safe factory with runtime validation ────────

export function defineWidget<C>(def: WidgetDefinition<C>): RegisteredWidgetDefinition {
    if (def.allowedSizes.length === 0) {
        throw new Error(`Widget "${def.type}": allowedSizes must not be empty`)
    }
    if (!def.allowedSizes.includes(def.defaultSize)) {
        throw new Error(`Widget "${def.type}": defaultSize must be in allowedSizes`)
    }
    const schema = def.configSchema
    const validatedDefault = schema.parse(def.defaultConfig)
    const parse = (config: JsonObject): C => {
        const parsed = schema.safeParse(config)
        return parsed.success ? parsed.data : validatedDefault
    }
    function RegisteredWidget(props: WidgetRuntimeProps) {
        return createElement(def.component, {
            ...props,
            config: parse(props.config),
            onConfigChange: (next: C) => props.onConfigChange(jsonObjectSnapshot(schema.parse(next))),
        })
    }
    const Editor = def.configEditor
    const configEditor = Editor && function RegisteredConfigEditor(props: WidgetConfigEditorProps) {
        return createElement(Editor, {
            config: parse(props.config),
            onConfigChange: (next: C) => props.onConfigChange(jsonObjectSnapshot(schema.parse(next))),
        })
    }
    return {
        type: def.type, label: def.label, description: def.description, icon: def.icon,
        allowedSizes: def.allowedSizes, defaultSize: def.defaultSize, category: def.category,
        requiredPermission: def.requiredPermission, adminOnly: def.adminOnly,
        defaultConfig: jsonObjectSnapshot(validatedDefault),
        parseConfig: (config: JsonObject) => {
            const parsed = schema.safeParse(config)
            return parsed.success ? jsonObjectSnapshot(parsed.data) : null
        },
        component: RegisteredWidget,
        configEditor,
    }
}

// ── Registry ────────────────────────────────────────────────────────

const WIDGET_DEFINITIONS: RegisteredWidgetDefinition[] = []

export const widgetRegistry = new Map<WidgetType, RegisteredWidgetDefinition>()

/** All definitions as array (for Widget Gallery), in registration order. */
export const allWidgetDefinitions = WIDGET_DEFINITIONS

/**
 * Registers a dashboard widget. Configurations call this from their
 * registration; registering a type again replaces its definition.
 */
export function registerWidget<C>(def: WidgetDefinition<C>): void {
    const registered = defineWidget(def)
    const index = WIDGET_DEFINITIONS.findIndex(d => d.type === def.type)
    if (index >= 0) WIDGET_DEFINITIONS[index] = registered
    else WIDGET_DEFINITIONS.push(registered)
    widgetRegistry.set(def.type, registered)
}
