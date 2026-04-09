"use client"

/**
 * CompoundReferenceField — compound reference field for TypedRef.
 *
 * Two-step picker:
 * 1. Type selector (dropdown): choose entity type from allowedRefTypes or all registered entities
 * 2. Entity picker (ReferenceField): standard combobox for the selected entity type
 *
 * Analogous to:
 * - 1C: «Поле ввода» with «ОписаниеТипов» — кнопка «T» для выбора типа
 * - ERPNext: Dynamic Link — link_doctype select + link_name search
 * - Odoo: fields.Reference — model dropdown + record picker
 */

import { useState, useCallback, useMemo, useEffect } from "react"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { ReferenceField } from "@/components/shared/reference-field"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { useResolvedRef } from "@/lib/ref-resolver"
import { cn } from "@/lib/utils"
import type { TypedRef } from "@/types/common"
import type { EntityMeta } from "@/types/metadata"

export interface CompoundReferenceFieldProps {
    /** Current TypedRef value */
    value: TypedRef
    /** Called when user selects type + entity */
    onChange: (ref: TypedRef, presentation: string) => void
    /** Allowed entity types (from metadata field.allowedRefTypes). Empty/undefined = any type. */
    allowedRefTypes?: string[]
    /** Compact mode for table cells */
    compact?: boolean
    /** Disabled state */
    disabled?: boolean
    /** Placeholder for the type selector */
    placeholder?: string
}

/**
 * Derive API endpoint from EntityMeta.
 * Catalog: /catalog/{routePrefix}  |  Document: /document/{routePrefix}
 */
function deriveApiEndpoint(entity: EntityMeta): string {
    const prefix = entity.routePrefix ?? entity.key
    return entity.type === "catalog"
        ? `/catalog/${prefix}`
        : `/document/${prefix}`
}

export function CompoundReferenceField({
    value,
    onChange,
    allowedRefTypes,
    compact = false,
    disabled = false,
    placeholder = "Выберите тип…",
}: CompoundReferenceFieldProps) {
    const entities = useMetadataStore((s) => s.entities)
    const getEntityByName = useMetadataStore((s) => s.getEntityByName)

    // ── Available entity types ──────────────────────────────────────────
    const availableTypes: EntityMeta[] = useMemo(() => {
        if (!allowedRefTypes || allowedRefTypes.length === 0) {
            // Universal mode: show ALL registered entities
            return entities
        }
        // Restricted mode: only allowed types
        return allowedRefTypes
            .map((name) => getEntityByName(name))
            .filter((e): e is EntityMeta => !!e)
    }, [entities, allowedRefTypes, getEntityByName])

    // ── Selected type entity ────────────────────────────────────────────
    const selectedEntity = useMemo(
        () => (value.refType ? getEntityByName(value.refType) : undefined),
        [value.refType, getEntityByName],
    )

    // ── Resolve current presentation ────────────────────────────────────
    const { presentation } = useResolvedRef(
        value.refType && value.refId ? value : null,
    )

    // ── API endpoint for the ReferenceField ─────────────────────────────
    const apiEndpoint = useMemo(
        () => (selectedEntity ? deriveApiEndpoint(selectedEntity) : ""),
        [selectedEntity],
    )

    // ── Handlers ────────────────────────────────────────────────────────
    const handleTypeChange = useCallback(
        (newType: string) => {
            // Reset refId when type changes
            onChange({ refType: newType, refId: "" }, "")
        },
        [onChange],
    )

    const handleEntityChange = useCallback(
        (id: string, display: string) => {
            onChange({ refType: value.refType, refId: id }, display)
        },
        [value.refType, onChange],
    )

    // ── Layout ──────────────────────────────────────────────────────────
    const height = compact ? "h-7" : "h-9"
    const textSize = compact ? "text-xs" : "text-sm"

    return (
        <div className={cn("flex gap-1 items-center w-full", compact && "gap-0.5")}>
            {/* Step 1: Type selector */}
            <Select
                value={value.refType || undefined}
                onValueChange={handleTypeChange}
                disabled={disabled}
            >
                <SelectTrigger
                    className={cn(
                        "shrink-0",
                        compact ? "w-[120px]" : "w-[160px]",
                        height,
                        textSize,
                    )}
                >
                    <SelectValue placeholder={placeholder} />
                </SelectTrigger>
                <SelectContent>
                    {availableTypes.map((entity) => (
                        <SelectItem key={entity.name} value={entity.name} className="text-xs">
                            {entity.presentation.singular}
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>

            {/* Step 2: Entity picker (only when type is selected) */}
            {value.refType && apiEndpoint ? (
                <div className="flex-1 min-w-0">
                    <ReferenceField
                        compact={compact}
                        value={value.refId}
                        displayName={presentation || undefined}
                        onChange={handleEntityChange}
                        apiEndpoint={apiEndpoint}
                        placeholder="Выберите…"
                        disabled={disabled}
                    />
                </div>
            ) : (
                <div
                    className={cn(
                        "flex-1 min-w-0 border rounded-md flex items-center px-3 text-muted-foreground",
                        height,
                        textSize,
                    )}
                >
                    Сначала выберите тип
                </div>
            )}
        </div>
    )
}
