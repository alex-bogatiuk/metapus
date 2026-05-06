"use client"

/**
 * AutoForm — metadata-driven form generator.
 *
 * Generates a form from the /api/v1/meta/:name endpoint.
 * Used as fallback when an entity has no custom formComponent registered
 * in the UIRegistry. Analogous to ERPNext's auto-rendered DocType forms.
 *
 * Field type → Component mapping:
 *   string     → <Input />
 *   boolean    → <Switch />
 *   reference  → <ReferenceField /> (combobox with search)
 *   date       → <Input type="date" />
 *   money      → <Input type="number" />
 *   integer    → <Input type="number" />
 *   number     → <Input type="number" />
 */

import { useEffect, useState, useCallback } from "react"
import { useRouter } from "next/navigation"
import { api, apiFetch, ApiError } from "@/lib/api"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Button } from "@/components/ui/button"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { ScrollArea } from "@/components/ui/scroll-area"
import { ReferenceField } from "@/components/shared/reference-field"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { Save, ArrowLeft, ArrowRightLeft, Network } from "lucide-react"
import { FormSkeleton } from "@/components/shared/form-skeleton"
import { toast } from "sonner"
import { useTabTitle } from "@/hooks/useTabTitle"
import { useTabDirty } from "@/hooks/useTabDirty"

interface FieldDef {
    name: string
    label?: string
    type: string
    required?: boolean
    readOnly?: boolean
    referenceType?: string
    scale?: number
    enumValues?: { value: string; label: string }[]
}

interface TablePartDef {
    name: string
    label?: string
    columns: FieldDef[]
}

interface EntityMetaDef {
    name: string
    type: string
    fields: FieldDef[]
    tableParts?: TablePartDef[]
    presentation?: {
        singular?: string
        plural?: string
        new?: string
    }
}

interface AutoFormProps {
    /** Entity name for metadata lookup (e.g. "Vehicle", "Counterparty") */
    entityName: string
    /** Entity ID — undefined means "create new" */
    id?: string
    /** Entity type for API path construction */
    entityType: "catalog" | "document"
    /** Route prefix for API path (e.g. "vehicles", "counterparties") */
    routePrefix: string
}

/** System/auto-managed fields that should not appear in the form */
const HIDDEN_FIELDS = new Set([
    "id", "version", "deletionMark", "attributes",
    "createdAt", "updatedAt", "createdBy", "updatedBy",
    "posted", "postedVersion", "txid", "deletedAt",
    "basisType", "basisId",
])

/**
 * Resolve a referenceType to an API endpoint path.
 * Uses metadata store (byKey) to find entity type + routePrefix.
 */
function resolveRefEndpoint(referenceType: string): string | null {
    const entity = useMetadataStore.getState().byKey[referenceType]
    if (!entity?.routePrefix) return null
    const prefix = entity.type === "document" ? "/document" : "/catalog"
    return `${prefix}/${entity.routePrefix}`
}

function fieldToInput(
    field: FieldDef,
    value: unknown,
    onChange: (name: string, val: unknown) => void,
    disabled: boolean,
    isFirstEditable = false,
) {
    const v = value ?? ""
    const handleChange = (val: unknown) => onChange(field.name, val)

    switch (field.type) {
        case "boolean":
            return (
                <Switch
                    checked={!!v}
                    onCheckedChange={handleChange}
                    disabled={disabled || field.readOnly}
                />
            )
        case "date":
        case "datetime":
            return (
                <Input
                    type="datetime-local"
                    value={typeof v === "string" ? v.slice(0, 16) : ""}
                    onChange={(e) => handleChange(e.target.value ? new Date(e.target.value).toISOString() : "")}
                    disabled={disabled || field.readOnly}
                    autoFocus={isFirstEditable}
                />
            )
        case "integer":
        case "number":
        case "money":
        case "decimal":
            return (
                <Input
                    type="number"
                    value={String(v)}
                    onChange={(e) => handleChange(e.target.value === "" ? 0 : Number(e.target.value))}
                    disabled={disabled || field.readOnly}
                    step={field.type === "integer" ? 1 : "any"}
                    autoFocus={isFirstEditable}
                />
            )
        case "reference": {
            if (field.referenceType && field.referenceType !== "parent") {
                const endpoint = resolveRefEndpoint(field.referenceType)
                if (endpoint) {
                    return (
                        <ReferenceField
                            value={typeof v === "string" ? v : ""}
                            onChange={(id) => handleChange(id)}
                            apiEndpoint={endpoint}
                            placeholder="Выберите…"
                            disabled={disabled || field.readOnly}
                        />
                    )
                }
            }
            // Fallback: unresolvable ref or parent — show text input
            return (
                <Input
                    type="text"
                    value={String(v)}
                    onChange={(e) => handleChange(e.target.value)}
                    disabled={disabled || field.readOnly}
                    autoFocus={isFirstEditable}
                />
            )
        }
        default:
            // Enum fields: render as dropdown when enumValues are available
            if (field.enumValues && field.enumValues.length > 0) {
                return (
                    <Select
                        value={String(v)}
                        onValueChange={handleChange}
                        disabled={disabled || field.readOnly}
                    >
                        <SelectTrigger>
                            <SelectValue placeholder="Выберите…" />
                        </SelectTrigger>
                        <SelectContent>
                            {field.enumValues.map((ev) => (
                                <SelectItem key={ev.value} value={ev.value}>
                                    {ev.label}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                )
            }
            return (
                <Input
                    type="text"
                    value={String(v)}
                    onChange={(e) => handleChange(e.target.value)}
                    disabled={disabled || field.readOnly}
                    autoFocus={isFirstEditable}
                />
            )
    }
}

export default function AutoForm({ entityName, id, entityType, routePrefix }: AutoFormProps) {
    const router = useRouter()
    const [meta, setMeta] = useState<EntityMetaDef | null>(null)
    const [formData, setFormData] = useState<Record<string, unknown>>({})
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [error, setError] = useState<string | null>(null)
    const isNew = !id || id === "new"
    const getLabel = useMetadataStore((s) => s.getLabel)

    const basePath = entityType === "catalog" ? `/catalog/${routePrefix}` : `/document/${routePrefix}`

    // Load metadata + entity data
    useEffect(() => {
        let cancelled = false
        async function load() {
            setLoading(true)
            setError(null)
            try {
                const metaData = await api.meta.getEntity(entityName)
                if (cancelled) return
                setMeta(metaData as EntityMetaDef)

                if (!isNew && id) {
                    const entity = await apiFetch<Record<string, unknown>>(`${basePath}/${id}`)
                    if (cancelled) return
                    setFormData(entity)
                }
            } catch (err) {
                if (!cancelled) {
                    setError(err instanceof Error ? err.message : "Failed to load")
                }
            } finally {
                if (!cancelled) setLoading(false)
            }
        }
        load()
        return () => { cancelled = true }
    }, [entityName, id, isNew, basePath])

    // Update tab title when entity data loads (e.g. "CP-001 (Крипто-платежи)")
    // entityName may be PascalCase (from registry) or snake_case (from metadata key) —
    // resolve via byName first, then byKey, then fall back to meta.presentation.
    const getEntityByName = useMetadataStore((s) => s.getEntityByName)
    const entityMeta = getEntityByName(entityName) ?? useMetadataStore.getState().byKey[entityName]
    const entityLabel = entityMeta?.presentation?.singular
        ?? meta?.presentation?.singular
        ?? getLabel(entityName, "singular")
    const displayNumber = isNew ? undefined : (formData.number as string | undefined) || (formData.name as string | undefined)
    useTabTitle(displayNumber, entityLabel)

    const isDocument = entityType === "document"
    const { markDirty, markClean } = useTabDirty()

    const handleFieldChange = useCallback((name: string, value: unknown) => {
        setFormData((prev) => ({ ...prev, [name]: value }))
        if (isDocument) markDirty()
    }, [isDocument, markDirty])

    // ── Refetch helper (document mode) ───────────────────────────────────
    const refetchDoc = useCallback(async () => {
        if (!id || isNew) return
        const entity = await apiFetch<Record<string, unknown>>(`${basePath}/${id}`)
        setFormData(entity)
        markClean()
    }, [id, isNew, basePath, markClean])

    const handleSave = async (andClose: boolean = false) => {
        setSaving(true)
        try {
            if (isNew) {
                const result = await apiFetch<Record<string, unknown>>(basePath, {
                    method: "POST",
                    body: JSON.stringify(formData),
                })
                toast.success("Записано")
                markClean()
                if (result?.id) {
                    const listPath = entityType === "catalog"
                        ? `/catalogs/${routePrefix}/${result.id}`
                        : `/documents/${routePrefix}/${result.id}`
                    router.push(listPath)
                }
            } else {
                // For posted documents: save = update + repost
                if (isDocument && formData.posted) {
                    const updated = await apiFetch<Record<string, unknown>>(`${basePath}/${id}/repost`, {
                        method: "PUT",
                        body: JSON.stringify(formData),
                    })
                    setFormData(updated)
                    markClean()
                    toast.success("Записано и перепроведено")
                } else {
                    const updated = await apiFetch<Record<string, unknown>>(`${basePath}/${id}`, {
                        method: "PUT",
                        body: JSON.stringify(formData),
                    })
                    setFormData(updated)
                    markClean()
                    toast.success("Записано")
                }
                if (andClose) {
                    const listPath = `/${entityType}s/${routePrefix}`
                    router.push(listPath)
                    return
                }
            }
        } catch (err) {
            if (err instanceof ApiError) {
                toast.error(err.parsedBody?.message ?? `Не удалось сохранить данные (код ${err.status})`)
            } else {
                toast.error("Не удалось сохранить данные")
            }
        } finally {
            setSaving(false)
        }
    }

    // ── Document-mode: Post and Close ────────────────────────────────────
    const handlePostAndClose = async () => {
        if (!isDocument || isNew) return
        setSaving(true)
        try {
            await apiFetch(`${basePath}/${id}/repost`, {
                method: "PUT",
                body: JSON.stringify(formData),
            })
            markClean()
            router.push(`/${entityType}s/${routePrefix}`)
        } catch (err) {
            if (err instanceof ApiError) {
                toast.error(err.parsedBody?.message ?? "Не удалось провести документ")
            } else {
                toast.error("Не удалось провести документ")
            }
        } finally {
            setSaving(false)
        }
    }

    // ── Document-mode: Post (without closing) ───────────────────────────
    const handlePost = async () => {
        if (!isDocument || isNew) return
        setSaving(true)
        try {
            await apiFetch(`${basePath}/${id}/post`, { method: "POST" })
            await refetchDoc()
            toast.success("Проведено")
        } catch (err) {
            if (err instanceof ApiError) {
                toast.error(err.parsedBody?.message ?? "Не удалось провести документ")
            } else {
                toast.error("Не удалось провести документ")
            }
        } finally {
            setSaving(false)
        }
    }

    // ── Document-mode: Unpost ────────────────────────────────────────────
    const handleUnpost = async () => {
        if (!isDocument || isNew) return
        setSaving(true)
        try {
            await apiFetch(`${basePath}/${id}/unpost`, { method: "POST" })
            await refetchDoc()
            toast.success("Проведение отменено")
        } catch (err) {
            if (err instanceof ApiError) {
                toast.error(err.parsedBody?.message ?? "Не удалось отменить проведение")
            } else {
                toast.error("Не удалось отменить проведение")
            }
        } finally {
            setSaving(false)
        }
    }

    // ── Document-mode: Toggle Deletion Mark ──────────────────────────────
    const handleToggleDeletionMark = async () => {
        if (!isDocument || isNew) return
        setSaving(true)
        try {
            const marked = !formData.deletionMark
            await apiFetch(`${basePath}/${id}/deletion-mark`, {
                method: "POST",
                body: JSON.stringify({ marked }),
            })
            await refetchDoc()
            toast.success(marked ? "Помечен на удаление" : "Пометка снята")
        } catch (err) {
            if (err instanceof ApiError) {
                toast.error(err.parsedBody?.message ?? "Не удалось изменить пометку")
            } else {
                toast.error("Не удалось изменить пометку")
            }
        } finally {
            setSaving(false)
        }
    }

    if (loading) {
        return <FormSkeleton variant={entityType} />
    }

    if (error || !meta) {
        return (
            <div className="p-6 text-center text-destructive">
                {error || "Метаданные не найдены"}
            </div>
        )
    }

    const editableFields = meta.fields.filter((f) => !HIDDEN_FIELDS.has(f.name))
    const title = isNew
        ? (meta.presentation?.new ?? `Новый: ${meta.presentation?.singular ?? entityName}`)
        : (meta.presentation?.singular ?? entityName)

    // ── Build document status badge ──────────────────────────────────────
    const docStatus = isDocument && !isNew
        ? formData.posted
            ? { label: "Проведён", variant: "success" as const }
            : formData.deletionMark
                ? { label: "Помечен на удаление", variant: "destructive" as const }
                : { label: "Черновик", variant: "outline" as const }
        : undefined

    // ── Build toolbar props based on entity type ─────────────────────────
    const toolbarPrimaryAction = isDocument && !isNew
        ? {
            label: saving ? "Сохранение…" : "Провести и закрыть",
            variant: "default" as const,
            onClick: handlePostAndClose,
        }
        : {
            label: saving ? "Сохранение…" : "Записать и закрыть",
            onClick: () => handleSave(true),
        }

    const toolbarSecondaryActions = isDocument && !isNew
        ? [
            { label: "Записать", onClick: () => handleSave(false) },
            ...(formData.posted
                ? [{ label: "Отменить проведение", onClick: handleUnpost }]
                : [{ label: "Провести", onClick: handlePost }]),
        ]
        : [
            { label: "Записать", onClick: () => handleSave(false) },
        ]

    const toolbarExtraMenuItems = isDocument && !isNew
        ? [
            {
                label: formData.deletionMark ? "Снять пометку удаления" : "Пометить на удаление",
                onClick: handleToggleDeletionMark,
                destructive: !formData.deletionMark,
            },
        ]
        : undefined

    return (
        <div className="flex h-full flex-col animate-skeleton-fade-in">
            <FormToolbar
                title={title}
                status={docStatus}
                primaryAction={toolbarPrimaryAction}
                secondaryActions={toolbarSecondaryActions}
                extraMenuItems={toolbarExtraMenuItems}
                toolbarIcons={isDocument && !isNew ? [
                    ...(formData.posted ? [{
                        icon: <ArrowRightLeft className="h-3.5 w-3.5" />,
                        title: "Движения документа",
                        onClick: () => router.push(`/documents/${routePrefix}/${id}/movements`),
                    }] : []),
                    {
                        icon: <Network className="h-3.5 w-3.5" />,
                        title: "Связанные документы",
                        onClick: () => router.push(`/documents/${routePrefix}/${id}/related`),
                    },
                ] : undefined}
                backHref={`/${entityType}s/${routePrefix}`}
                backTargetId={id === "new" ? undefined : id}
                onClose={() => router.push(`/${entityType}s/${routePrefix}`)}
                favoriteTarget={!isNew && id ? {
                    entityType: entityName,
                    entityId: id,
                    title: `${entityLabel} ${displayNumber || ""}`,
                    url: `/${entityType}s/${routePrefix}/${id}`,
                } : undefined}
            />

            <ScrollArea className="flex-1">
                <div className="p-6">
                    <div className="max-w-3xl space-y-6">
                        {/* Auto-generated fields */}
                        <div className="grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-2">
                            {editableFields.map((field, idx) => {
                                // M7: autoFocus on first editable non-boolean field for new entities
                                const isFirstEditable = isNew && idx === editableFields.findIndex(
                                    (f) => !f.readOnly && f.type !== "boolean"
                                )
                                return (
                                    <div key={field.name} className={field.type === "string" && field.name === "comment" ? "md:col-span-2" : ""}>
                                        <Label htmlFor={field.name} className="text-xs text-muted-foreground">
                                            {field.label || field.name}
                                            {field.required && <span className="ml-0.5 text-destructive">*</span>}
                                        </Label>
                                        <div className="mt-1">
                                            {fieldToInput(field, formData[field.name], handleFieldChange, saving, isFirstEditable)}
                                        </div>
                                    </div>
                                )
                            })}
                        </div>

                        {/* Table parts */}
                        {meta.tableParts?.map((tp) => (
                            <div key={tp.name} className="space-y-2 mt-8">
                                <Label className="text-sm font-semibold">{tp.label || tp.name}</Label>
                                <div className="rounded border text-sm">
                                    <div className="grid border-b bg-muted/50 px-3 py-2 font-medium" style={{
                                        gridTemplateColumns: `repeat(${tp.columns.filter(c => !HIDDEN_FIELDS.has(c.name)).length}, 1fr)`,
                                    }}>
                                        {tp.columns.filter(c => !HIDDEN_FIELDS.has(c.name)).map((col) => (
                                            <div key={col.name}>{col.label || col.name}</div>
                                        ))}
                                    </div>
                                    {Array.isArray(formData[tp.name]) && (formData[tp.name] as Record<string, unknown>[]).length > 0 ? (
                                        (formData[tp.name] as Record<string, unknown>[]).map((row, idx) => (
                                            <div key={idx} className="grid border-b px-3 py-2 last:border-0" style={{
                                                gridTemplateColumns: `repeat(${tp.columns.filter(c => !HIDDEN_FIELDS.has(c.name)).length}, 1fr)`,
                                            }}>
                                                {tp.columns.filter(c => !HIDDEN_FIELDS.has(c.name)).map((col) => (
                                                    <div key={col.name} className="truncate">{String(row[col.name] ?? "")}</div>
                                                ))}
                                            </div>
                                        ))
                                    ) : (
                                        <div className="px-3 py-4 text-center text-muted-foreground">Нет строк</div>
                                    )}
                                </div>
                            </div>
                        ))}
                    </div>
                </div>
            </ScrollArea>
        </div>
    )
}
