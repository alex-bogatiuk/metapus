"use client"

import type { JsonObject } from "@/lib/json-value"

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
 *   money      → checked text input of minor units
 *   integer    → <Input type="number" />
 *   number     → <Input type="number" />
 *   enum       → <Select />, or <ColorPicker /> for palette colours (widget "color")
 */

import React, { useEffect, useState, useCallback, useRef } from "react"
import { z } from "zod"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { api, apiFetch, ApiError, idempotencyHeaders } from "@/lib/api"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { ColorPicker } from "@/components/ui/color-picker"
import { Button } from "@/components/ui/button"
import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { PrintMenuButton } from "@/components/shared/print-menu-button"
import { ReasonedDecisionDialog } from "@/components/shared/reasoned-decision-dialog"
import { decisionUnavailableReason, useReasonedDecision } from "@/hooks/useReasonedDecision"
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert"
import { ScrollArea } from "@/components/ui/scroll-area"
import { ReferenceField } from "@/components/shared/reference-field"
import { UserPicker } from "@/components/shared/user-picker"
import { DateTimePicker } from "@/components/ui/date-time-picker"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { Save, ArrowLeft, ArrowRightLeft, ChevronRight, Network } from "lucide-react"
import { FormSkeleton } from "@/components/shared/form-skeleton"
import { toast } from "sonner"
import { useTabTitle } from "@/hooks/useTabTitle"
import { useAutoFormDraft, AUTO_FORM_STATE_KEY } from "@/hooks/useAutoFormDraft"
import { useTabStateStore } from "@/stores/useTabStateStore"
import { useTabsStore } from "@/stores/useTabsStore"
import { jsonObjectSchema, type JsonValue } from "@/lib/json-value"
import { isCurrentSession, getSessionGeneration } from "@/lib/auth-scope"
import { useCloseTab } from "@/hooks/useCloseTab"
import { useHasTabCache, useTabState } from "@/hooks/useTabState"
import type { DocumentCapabilities, EntityActionDef } from "@/types/metadata"
import { buildEntityUrlByRoute } from "@/lib/entity-url"
import { buildCatalogFormPayload } from "@/lib/catalog-form-payload"
import { isSystemAdmin, useAuthStore } from "@/stores/useAuthStore"
import { createTabStateKey } from "@/stores/useTabStateStore"
import { majorToMinor, minorUnitsSchema } from "@/lib/money"
import type { EntityMetadata, MetadataField } from "@/lib/schemas/metadata"
import { parseEntityValues } from "@/lib/schemas/entity-values"
import { formRequestFieldNames, formRequestParts, projectFormRequest } from "@/lib/http-contracts"
import { displayValue, isWideField, isWritableField, planFormLayout } from "@/lib/form-layout"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { resolveRefEndpoint } from "@/lib/reference-endpoint"
import { TablePartEditor } from "@/components/shared/table-part-editor"
import { LineInputError, checkRequiredCells } from "@/lib/table-part-rows"
import { applyFormulas, formulaLabel, hasFormulas } from "@/lib/formula"
import { useDocumentCurrency, type DocumentCurrency } from "@/hooks/useDocumentCurrency"
import { moneyText } from "@/lib/grid-paste"
import { formatSummaryNumber } from "@/lib/selection-summary"

const tabStateKeys = {
  "autoFormMeta": createTabStateKey<EntityMetaDef | null>("frontend/components/shared/auto-form.tsx:autoFormMeta"),
}


type FieldDef = MetadataField
type EntityMetaDef = EntityMetadata

export interface AutoFormProps {
    /** Entity name for metadata lookup (e.g. "Vehicle", "Counterparty") */
    entityName: string
    /** Entity ID — undefined means "create new" */
    id?: string
    /** Entity type for API path construction */
    entityType: "catalog" | "document"
    /** Route prefix for API path (e.g. "vehicles", "counterparties") */
    routePrefix: string
    /**
     * Optional content rendered BELOW the auto-generated form fields.
     * Use for entity-specific extensions (e.g. API key management for a client account).
     */
    appendContent?: React.ReactNode
    hiddenFields?: readonly string[]
    readOnly?: boolean
    /**
     * Entity-specific input for a field; return undefined to keep the default.
     * Receives the whole draft for fields whose choices depend on other fields.
     */
    renderField?: (field: FieldDef, context: AutoFormFieldContext) => React.ReactNode | undefined
}

export interface AutoFormFieldContext {
    value: JsonValue | undefined
    values: Record<string, JsonValue | undefined>
    onChange: (value: JsonValue) => void
    readOnly: boolean
}

/** System/auto-managed fields that should not appear in the form */
/** Tab value of the "Дополнительно" zone when table parts are on tabs. */
const EXTRA_TAB = "__extra"

const HIDDEN_FIELDS = new Set([
    "id", "version", "deletionMark", "attributes",
    "createdAt", "updatedAt", "createdBy", "updatedBy",
    "posted", "postedVersion", "txid", "deletedAt",
    "basisType", "basisId",
])

function isRenderableItemAction(action: EntityActionDef, placement: "formToolbar" | "toolbarMenu"): boolean {
    if (action.scope !== "item" || !action.ui?.[placement]) return false
    return ["GET", "POST", "PUT", "PATCH", "DELETE"].includes(action.method.toUpperCase())
}

function isActionAllowedInState(action: EntityActionDef, status: JsonValue | undefined): boolean {
    if (!action.allowedStates?.length) return true
    return typeof status === "string" && action.allowedStates.includes(status)
}

function buildItemActionPath(basePath: string, id: string, action: EntityActionDef): string {
    const rel = action.path.startsWith("/") ? action.path : `/${action.path}`
    return `${basePath}${rel
        .replace(":id", encodeURIComponent(id))
        .replace("{id}", encodeURIComponent(id))}`
}

/**
 * Money typed in the document's currency (major units, "1 999,99") and sent
 * as minor units, exactly: more places than the currency has are refused,
 * not rounded, since the user typed them.
 */
function MajorMoneyInput({ value, currency, disabled, readOnly, autoFocus, onChange }: {
    value: JsonValue | undefined
    currency: DocumentCurrency
    disabled: boolean
    readOnly: boolean
    autoFocus: boolean
    onChange: (minor: string) => void
}) {
    const [draft, setDraft] = useState<string | null>(null)
    const parse = (text: string): { minor: string } | { error: string } => {
        try {
            return { minor: majorToMinor(text.replace(/[\s\u00a0]/g, ""), currency.scale) }
        } catch (error) {
            return { error: error instanceof Error ? error.message : "Введите сумму" }
        }
    }
    const shown = draft ?? moneyText(value, currency.scale)
    const parsed = draft !== null && draft.trim() !== "" ? parse(draft) : null
    const error = parsed && "error" in parsed ? parsed.error : null
    return (
        <div className="grid gap-1">
            <div className="relative">
                <Input type="text" inputMode="decimal" autoComplete="off" value={shown} className="pr-12 text-right tabular-nums"
                    aria-invalid={!!error} disabled={disabled} readOnly={readOnly} autoFocus={autoFocus}
                    onChange={(event) => {
                        const text = event.target.value
                        setDraft(text)
                        if (!text.trim()) onChange("")
                        else {
                            const result = parse(text)
                            if ("minor" in result) onChange(result.minor)
                        }
                    }}
                    onBlur={() => { if (!error) setDraft(null) }} />
                <span className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-xs text-muted-foreground">{currency.code}</span>
            </div>
            {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
        </div>
    )
}

function fieldToInput(
    field: FieldDef,
    value: JsonValue | undefined,
    onChange: (name: string, val: JsonValue) => void,
    disabled: boolean,
    readOnly: boolean,
    isFirstEditable = false,
    writeOnlyConfigured = false,
    currency: DocumentCurrency | null = null,
) {
    const v = value ?? ""
    const handleChange = (val: JsonValue) => onChange(field.name, val)
    const effectiveReadOnly = readOnly || !!field.readOnly

	if (field.writeOnly) {
		return (
			<Input
				id={field.name}
				type="password"
				value={typeof value === "string" ? value : ""}
				onChange={(event) => handleChange(event.target.value)}
				disabled={disabled}
				readOnly={effectiveReadOnly}
				autoComplete="new-password"
				placeholder={writeOnlyConfigured ? "Введите новый ключ для замены" : "Введите ключ"}
				autoFocus={isFirstEditable}
			/>
		)
	}

    switch (field.type) {
        case "boolean":
            return (
                <Switch
                    checked={!!v}
                    onCheckedChange={handleChange}
                    disabled={disabled || effectiveReadOnly}
                />
            )
        case "date":
        case "datetime":
            return (
                <DateTimePicker
                    value={typeof v === "string" && v ? new Date(v) : undefined}
                    onChange={(date) => handleChange(date ? date.toISOString() : null)}
                    disabled={disabled || effectiveReadOnly}
                />
            )
        case "money":
            if (currency) {
                return <MajorMoneyInput value={value} currency={currency} disabled={disabled} readOnly={effectiveReadOnly}
                    autoFocus={isFirstEditable} onChange={handleChange} />
            }
            return (
                <div className="grid gap-1">
                    <Input type="text" inputMode="numeric" value={String(v)}
                        onChange={(e) => handleChange(e.target.value)}
                        aria-invalid={v !== "" && !minorUnitsSchema.safeParse(v).success}
                        disabled={disabled} readOnly={effectiveReadOnly} autoFocus={isFirstEditable} />
                    <p className="text-xs text-muted-foreground">В минимальных единицах валюты: например, 100 копеек = 1 рубль.</p>
                    {v !== "" && !minorUnitsSchema.safeParse(v).success && <p role="alert" className="text-xs text-destructive">Введите целое число минимальных единиц в допустимом диапазоне</p>}
                </div>
            )
        case "integer":
        case "number":
        case "decimal":
            return (
                <Input
                    type="number"
                    value={String(v)}
                    onChange={(e) => {
                        const raw = e.target.value
                        // Preserve original type: if server sent a string (e.g. RawAmount),
                        // keep it as string to match Go DTO contract.
                        if (typeof value === "string") {
                            handleChange(raw === "" ? "0" : raw)
                        } else {
                            handleChange(raw === "" ? 0 : Number(raw))
                        }
                    }}
                    disabled={disabled}
                    readOnly={effectiveReadOnly}
                    step={field.type === "integer" ? 1 : "any"}
                    autoFocus={isFirstEditable}
                />
            )
        case "reference": {
            if (field.referenceType === "user") {
                return (
                    <UserPicker
                        value={typeof v === "string" ? v : ""}
                        onChange={(userID) => handleChange(userID)}
                        disabled={disabled || effectiveReadOnly}
                    />
                )
            }
            if (field.referenceType && field.referenceType !== "parent") {
                const endpoint = resolveRefEndpoint(field.referenceType)
                if (endpoint) {
                    return (
                        <ReferenceField
                            value={typeof v === "string" ? v : ""}
                            onChange={(id) => handleChange(id)}
                            apiEndpoint={endpoint}
                            placeholder="Выберите…"
                            disabled={disabled}
                            readOnly={effectiveReadOnly}
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
                    disabled={disabled}
                    readOnly={effectiveReadOnly}
                    autoFocus={isFirstEditable}
                />
            )
        }
        default:
            if (field.widget === "color" && field.enumValues) {
                return (
                    <ColorPicker
                        id={field.name}
                        value={typeof v === "string" ? v : ""}
                        onChange={handleChange}
                        options={field.enumValues}
                        disabled={disabled}
                        readOnly={effectiveReadOnly}
                    />
                )
            }
            // Enum fields: render as dropdown when enumValues are available
            if (field.enumValues && field.enumValues.length > 0) {
                return (
                    <Select
                        value={v ? String(v) : undefined}
                        onValueChange={handleChange}
                        disabled={disabled || effectiveReadOnly}
                    >
                        <SelectTrigger id={field.name}>
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
                    disabled={disabled}
                    readOnly={effectiveReadOnly}
                    autoFocus={isFirstEditable}
                />
            )
    }
}

export default function AutoForm({ entityName, id, entityType, routePrefix, appendContent, hiddenFields = [], readOnly = false, renderField }: AutoFormProps) {
    const router = useRouter()
    const pathname = usePathname()
    const searchParams = useSearchParams()
    const copyFromId = searchParams?.get("copyFrom")
    const { closeOne, closeAndShowInList } = useCloseTab()
    const isNew = !id || id === "new"
    const hasCachedMeta = useHasTabCache(tabStateKeys["autoFormMeta"])
    const hasCachedFormData = useHasTabCache(AUTO_FORM_STATE_KEY)
    const hasCachedContent = hasCachedMeta && ((isNew && !copyFromId) || hasCachedFormData)
    const [cachedMeta, setMeta] = useTabState(tabStateKeys["autoFormMeta"], null)
    const metaRef = useRef<EntityMetadata | null>(null)
    if (cachedMeta) {
        metaRef.current = cachedMeta
    }
    const meta = cachedMeta ?? metaRef.current
    const { state: formState, edit: editDraft, startRead, finishRead, startSave, finishSave, current: currentDraft } = useAutoFormDraft()
    const formData = formState.draft
    // The document's currency: its money reads and is typed in major units.
    const currency = useDocumentCurrency(meta?.fields, formData)
    const generation = getSessionGeneration()
    const [confirmReload, setConfirmReload] = useState(false)
    const [loading, setLoading] = useState(!hasCachedContent)
    const [refreshing, setRefreshing] = useState(false)
    const [saving, setSaving] = useState(false)
    const [pendingAction, setPendingAction] = useState<EntityActionDef | null>(null)
    const [error, setError] = useState<string | null>(null)
    const getLabel = useMetadataStore((s) => s.getLabel)
    const user = useAuthStore((s) => s.user)
    const systemAdmin = isSystemAdmin(user)

    const basePath = entityType === "catalog" ? `/catalog/${routePrefix}` : `/document/${routePrefix}`
    const listPath = buildEntityUrlByRoute(routePrefix, entityType)

    const closeCurrentTabOrGoToList = useCallback((savedId: string) => {
        closeAndShowInList(pathname, listPath, savedId)
    }, [closeAndShowInList, listPath, pathname])

    // Load metadata + entity data
    useEffect(() => {
        let cancelled = false
        const ticket = startRead()
        async function load() {
            if (hasCachedContent) {
                setLoading(false)
                setRefreshing(true)
            } else {
                setRefreshing(false)
                setLoading(true)
            }
            setError(null)
            try {
                const metaData = await api.meta.getEntity(entityName)
                if (cancelled) return
                setMeta(metaData)

                if (!isNew && id) {
                    const entity = await apiFetch<JsonObject>(`${basePath}/${id}`)
                    if (cancelled) return
                    finishRead(parseEntityValues(metaData, entity), ticket)
                } else if (isNew && copyFromId && !hasCachedFormData) {
                    const sourceEntity = await apiFetch<JsonObject>(`${basePath}/${copyFromId}`)
                    if (cancelled) return
                    const parsed = parseEntityValues(metaData, sourceEntity)
                    const copiedData: JsonObject = { ...parsed }
                    delete copiedData.id
                    delete copiedData.version
                    delete copiedData.deletionMark
                    delete copiedData.createdAt
                    delete copiedData.updatedAt
                    delete copiedData.createdBy
                    delete copiedData.updatedBy
                    delete copiedData.code
                    delete copiedData.number
                    if ("posted" in copiedData) {
                        copiedData.posted = false
                    }
                    finishRead(copiedData, ticket, true)
                }
            } catch (err) {
                if (!cancelled) {
                    setError(err instanceof Error ? err.message : "Failed to load")
                }
            } finally {
                if (!cancelled) {
                    setLoading(false)
                    setRefreshing(false)
                }
            }
        }
        load()
        return () => { cancelled = true }
    }, [entityName, id, isNew, copyFromId, basePath, hasCachedContent, hasCachedFormData, setMeta, startRead, finishRead])

    // Update tab title when entity data loads (e.g. "CP-001 (Крипто-платежи)")
    // entityName may be PascalCase (from registry) or snake_case (from metadata key) —
    // resolve via byName first, then byKey, then fall back to meta.presentation.
    const getEntityByName = useMetadataStore((s) => s.getEntityByName)
    const entityMeta = getEntityByName(entityName) ?? useMetadataStore.getState().byKey[entityName]
    const entityLabel = entityMeta?.presentation?.singular
        ?? meta?.presentation?.singular
        ?? getLabel(entityName, "singular")
    const displayNumber = (typeof formData.number === "string" && formData.number) || (typeof formData.name === "string" ? formData.name : undefined)
    const tabTitle = useTabTitle(displayNumber, entityLabel, !isNew ? "edit" : copyFromId ? "copy" : "create")

    const isDocument = entityType === "document"

    const documentCapabilities = isDocument ? meta?.capabilities : undefined
    const hasDocCapability = useCallback(
        (key: keyof DocumentCapabilities) => documentCapabilities?.[key] !== false,
        [documentCapabilities],
    )
    const supportsCreate = !isDocument || hasDocCapability("create")
    const supportsUpdate = !isDocument || hasDocCapability("update")
    const supportsPost = isDocument && !isNew && hasDocCapability("post")
    const supportsUnpost = isDocument && !isNew && hasDocCapability("unpost")
    const supportsRepost = isDocument && !isNew && hasDocCapability("repost")
    const supportsSetDeletionMark = isDocument && !isNew && hasDocCapability("setDeletionMark")
    const canCreate = supportsCreate
    const canUpdate = supportsUpdate
    const canPost = supportsPost
    const canUnpost = supportsUnpost
    const canRepost = supportsRepost
    const canSetDeletionMark = supportsSetDeletionMark
    const canShowMovements = isDocument && !isNew && hasDocCapability("movements")
    const canShowRelatedDocuments = isDocument && !isNew && hasDocCapability("relatedDocuments")
    // Print forms are opt-in: an absent flag must not render a menu that 404s.
    const canPrint = isDocument && !isNew && !!id && documentCapabilities?.printForms === true
    const formReadOnly = readOnly || (!isNew && isDocument && !canUpdate && !canRepost)

    // Table-part line fields come from the form's own request contract: create
    // for a new document, update (repost takes the same request) otherwise.
    const formRoute = isNew ? basePath : `${basePath}/${id}`
    const tablePartFields = formRequestParts(isNew ? "POST" : "PUT", formRoute)
    // Fields the form's own request does not accept are shown read-only under
    // "Дополнительно" instead of as inputs whose values would be dropped.
    const formRequestFields = formRequestFieldNames(isNew ? "POST" : "PUT", formRoute)
    const [activePart, setActivePart] = useState<string | undefined>(undefined)
    const [partError, setPartError] = useState<string | null>(null)
    // The table part cell a failed save takes the user to.
    const [lineFocus, setLineFocus] = useState<{ part: string; line: number; column: string; seq: number } | null>(null)

    const handleFieldChange = useCallback((name: string, value: JsonValue) => {
        const next = editDraft(name, value)
        // Calculated header fields follow their inputs as the user types; the
        // server computes the stored value on save.
        const fields = meta?.fields ?? []
        if (!hasFormulas(fields)) return
        const computed = applyFormulas(next.draft, fields)
        for (const field of fields) {
            if (field.calc && computed[field.name] !== next.draft[field.name]) editDraft(field.name, computed[field.name])
        }
    }, [editDraft, meta])

    // Background refresh observes the server without replacing a dirty draft.
    const refetchDoc = useCallback(async (discard = false) => {
        if (!id || isNew || !meta) return
        const ticket = startRead()
        setRefreshing(true)
        try {
            const entity = await apiFetch<Record<string, JsonValue>>(`${basePath}/${id}`)
            finishRead(parseEntityValues(meta, entity), ticket, discard)
            setError(null)
        } catch (err) {
            if (!isCurrentSession(generation)) return
            setError("Не удалось обновить карточку")
            throw err
        } finally { if (isCurrentSession(generation)) setRefreshing(false) }
    }, [id, isNew, basePath, startRead, finishRead, generation, meta])

    const requestReload = () => {
        if (currentDraft().dirty) setConfirmReload(true)
        else void refetchDoc(true).catch(() => undefined)
    }

    const decision = useReasonedDecision(refetchDoc)
    const actionAvailabilityResult = z.record(z.string(), z.object({
        allowed: z.boolean(), reasonCode: z.string().optional(), decisionMode: z.enum(["independent", "admin_self"]).optional(),
    })).safeParse(formData.actionAvailability)
    const actionAvailability = actionAvailabilityResult.success ? actionAvailabilityResult.data : undefined

    const handleSave = async (andClose: boolean = false) => {
        if (readOnly || !meta) return
        if (isNew && !canCreate) {
            toast.error("Создание этого документа недоступно")
            return
        }
        if (!isNew && isDocument && formData.posted && !canRepost) {
            toast.error("Сохранение проведенного документа недоступно")
            return
        }
        if (!isNew && !canUpdate) {
            toast.error("Изменение этого документа недоступно")
            return
        }
        if (saving) return
        const ticket = startSave()
        const snapshot = currentDraft()
        setSaving(true)
        try {
            const data = !isNew && snapshot.baseVersion !== null ? { ...ticket.draft, version: snapshot.baseVersion } : ticket.draft
            for (const field of meta.fields) {
                if (field.type === "money" && !field.readOnly && data[field.name] !== undefined) {
                    if (!minorUnitsSchema.safeParse(data[field.name]).success) throw new Error(`${field.label ?? field.name}: введите целое число минимальных единиц в допустимом диапазоне`)
                }
            }
            checkRequiredCells(meta.tableParts ?? [], data, tablePartFields)
            setPartError(null)
            const requisites = entityType === "catalog" ? buildCatalogFormPayload(meta.fields, data, isNew, hiddenFields) : data
            const repost = !isNew && isDocument && ticket.draft.posted && canRepost
            const target = isNew ? basePath : `${basePath}/${id}${repost ? "/repost" : ""}`
            const payload = projectFormRequest(isNew ? "POST" : "PUT", target, requisites)
            parseEntityValues(meta, payload, "request")
            const response = await apiFetch<Record<string, JsonValue>>(target, { method: isNew ? "POST" : "PUT", body: JSON.stringify(payload) })
            const next = finishSave(parseEntityValues(meta, response), ticket)
            toast.success(repost ? "Записано и перепроведено" : "Записано")
            const savedId = typeof response.id === "string" ? response.id : id
            if (andClose && !next.dirty && savedId) {
                closeCurrentTabOrGoToList(savedId)
            } else if (isNew && typeof response.id === "string") {
                const editPath = buildEntityUrlByRoute(routePrefix, entityType, response.id)
                useTabStateStore.getState().set(editPath, AUTO_FORM_STATE_KEY, next)
                useTabStateStore.getState().set(editPath, tabStateKeys["autoFormMeta"], meta)
                useTabsStore.getState().openTab({ id: editPath, url: editPath, title: String(response.name ?? response.number ?? entityLabel), isDirty: next.dirty })
                useTabsStore.getState().setTabDirty(pathname, false)
                closeOne(pathname)
                router.push(editPath)
            }
        } catch (err) {
            if (!isCurrentSession(generation)) return
            if (err instanceof ApiError) {
                toast.error(err.parsedBody?.message ?? `Не удалось сохранить данные (код ${err.status})`)
            } else if (err instanceof LineInputError) {
                setPartError(err.part)
                setActivePart(err.part)
                setLineFocus({ part: err.part, line: err.line, column: err.column, seq: Date.now() })
                toast.error(err.message)
            } else {
                toast.error("Не удалось сохранить данные")
            }
        } finally {
            if (isCurrentSession(generation)) setSaving(false)
        }
    }

    // ── Document-mode: write current form state + post ───────────────────
    const postCurrentDocument = async (closeAfter: boolean) => {
        if (!isDocument || isNew || !meta) return
        if (!canPost) {
            toast.error("Проведение этого документа недоступно")
            return
        }
        if (saving) return
        const ticket = startSave()
        setSaving(true)
        try {
            if (canRepost) {
                const target = `${basePath}/${id}/repost`
                checkRequiredCells(meta.tableParts ?? [], ticket.draft, tablePartFields)
                const payload = projectFormRequest("PUT", target, ticket.draft)
                parseEntityValues(meta, payload, "request")
                const updated = await apiFetch<Record<string, JsonValue>>(target, { method: "PUT", body: JSON.stringify(payload) })
                finishSave(parseEntityValues(meta, updated), ticket)
            } else {
                await apiFetch<void>(`${basePath}/${id}/post`, { method: "POST" })
                await refetchDoc()
            }
            if (closeAfter && !currentDraft().dirty) {
                closeCurrentTabOrGoToList(id)
                return
            }
            toast.success("Проведено")
        } catch (err) {
            if (!isCurrentSession(generation)) return
            if (err instanceof ApiError) {
                toast.error(err.parsedBody?.message ?? "Не удалось провести документ")
            } else if (err instanceof LineInputError) {
                setPartError(err.part)
                setActivePart(err.part)
                setLineFocus({ part: err.part, line: err.line, column: err.column, seq: Date.now() })
                toast.error(err.message)
            } else {
                toast.error("Не удалось провести документ")
            }
        } finally {
            if (isCurrentSession(generation)) setSaving(false)
        }
    }

    // ── Document-mode: Unpost ────────────────────────────────────────────
    const handleUnpost = async () => {
        if (!isDocument || isNew) return
        if (!canUnpost) {
            toast.error("Отмена проведения этого документа недоступна")
            return
        }
        setSaving(true)
        try {
            await apiFetch(`${basePath}/${id}/unpost`, { method: "POST" })
            await refetchDoc()
            toast.success("Проведение отменено")
        } catch (err) {
            if (!isCurrentSession(generation)) return
            if (err instanceof ApiError) {
                toast.error(err.parsedBody?.message ?? "Не удалось отменить проведение")
            } else {
                toast.error("Не удалось отменить проведение")
            }
        } finally {
            if (isCurrentSession(generation)) setSaving(false)
        }
    }

    // ── Document-mode: Toggle Deletion Mark ──────────────────────────────
    const handleToggleDeletionMark = async () => {
        if (!isDocument || isNew) return
        if (!canSetDeletionMark) {
            toast.error("Пометка удаления для этого документа недоступна")
            return
        }
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
            if (!isCurrentSession(generation)) return
            if (err instanceof ApiError) {
                toast.error(err.parsedBody?.message ?? "Не удалось изменить пометку")
            } else {
                toast.error("Не удалось изменить пометку")
            }
        } finally {
            if (isCurrentSession(generation)) setSaving(false)
        }
    }

    const handleCustomAction = useCallback(async (action: EntityActionDef) => {
        if (!id || isNew) return
        setSaving(true)
        try {
            await apiFetch<void>(buildItemActionPath(basePath, id, action), {
                method: action.method.toUpperCase(),
                headers: action.idempotent ? idempotencyHeaders() : undefined,
            })
            await refetchDoc()
            toast.success(action.label)
        } catch (err) {
            if (!isCurrentSession(generation)) return
            if (err instanceof ApiError) {
                toast.error(err.parsedBody?.message ?? `Не удалось выполнить действие "${action.label}"`)
            } else {
                toast.error(`Не удалось выполнить действие "${action.label}"`)
            }
        } finally {
            if (isCurrentSession(generation)) setSaving(false)
        }
    }, [basePath, id, isNew, refetchDoc, generation])

    const requestCustomAction = useCallback((action: EntityActionDef) => {
        if (decision.pending || decision.busy || saving) return
        if (action.input?.kind === "reasonedDecision") {
            const availability = actionAvailability?.[action.key]
            if (!id || !availability?.allowed || loading || refreshing || error || !Number.isInteger(formData.version)) return
            decision.open({ action, path: buildItemActionPath(basePath, id, action), version: formData.version as number, availability,
                summary: (action.input.summaryFields ?? []).map((field) => ({ label: meta?.fields.find((item) => item.name === field)?.label ?? field, value: String(formData[field] ?? "—") })) })
            return
        }
        if (action.confirmation) {
            setPendingAction(action)
            return
        }
        void handleCustomAction(action)
    }, [handleCustomAction, decision, saving, actionAvailability, id, loading, refreshing, error, formData, meta, basePath])

    const hasFormContent = !!meta && (isNew || Object.keys(formData).length > 0 || hasCachedContent)

    // During tab navigation/close, Next.js updates pathname context before unmounting
    // this component. Check if this form is still on its expected route.
    const expectedPath = buildEntityUrlByRoute(routePrefix, entityType, isNew ? undefined : id, isNew ? "new" : undefined)
    const isStaleRender = pathname !== expectedPath

    // If navigating away and no content yet, show skeleton; otherwise keep rendering existing form
    if (isStaleRender && !hasFormContent) {
        return <FormSkeleton variant={entityType} />
    }

    if (loading && !hasFormContent) {
        return <FormSkeleton variant={entityType} />
    }

    // Only render error message if an actual error string was returned from the API
    if (error && !hasFormContent) {
        return (
            <div className="p-6 text-center text-destructive">
                {error}
            </div>
        )
    }

    if (!meta) {
        return <FormSkeleton variant={entityType} />
    }

    if (isNew && isDocument && meta.capabilities?.create === false) {
        return (
            <div className="flex h-full items-center justify-center p-6">
                <div className="max-w-md rounded-md border bg-card p-6 text-center shadow-sm">
                    <h1 className="text-sm font-semibold text-foreground">Создание недоступно</h1>
                    <p className="mt-2 text-sm text-muted-foreground">
                        Этот документ создается другим workflow и не поддерживает стандартное создание в ERP.
                    </p>
                    <Button className="mt-4" variant="outline" onClick={() => router.push(listPath)}>
                        Вернуться к списку
                    </Button>
                </div>
            </div>
        )
    }

    const editableFields = meta.fields.filter((f) => !HIDDEN_FIELDS.has(f.name) && !hiddenFields.includes(f.name))
    const tableParts = meta.tableParts ?? []
    const layout = planFormLayout({ kind: entityType, fields: editableFields, partCount: tableParts.length, accepted: formRequestFields })
    const activeTab = activePart ?? tableParts[0]?.name ?? EXTRA_TAB
    // M7: autoFocus on the first editable non-boolean field of a new entity, in layout order
    const firstEditable = isNew && !formReadOnly
        ? [...layout.header, ...layout.groups.flatMap((group) => group.fields), ...layout.wide]
            .find((f) => isWritableField(f, formRequestFields) && f.type !== "boolean")?.name
        : undefined

    const renderFieldBlock = (field: FieldDef, forceReadOnly = false) => {
        // Server-owned values read as text, not as inputs that look editable;
        // a reference keeps its picker for the display name and navigation.
        const ownReadOnly = forceReadOnly && renderField
            ? renderField(field, { value: formData[field.name], values: formData, onChange: () => undefined, readOnly: true })
            : undefined
        const value = formData[field.name]
        const emptyReference = field.type === "reference" && (value === undefined || value === null || value === "")
        if (forceReadOnly && ownReadOnly === undefined && (field.type !== "reference" || emptyReference)) {
            return (
                <div key={field.name} data-field={field.name}>
                    <p className="text-xs text-muted-foreground" title={field.calc ? `Рассчитывается: ${formulaLabel(field.calc, meta?.fields ?? [])}` : undefined}>
                        {field.label || field.name}
                        {field.calc && <span className="ml-1 font-serif italic text-muted-foreground/70" aria-label="рассчитывается">ƒ</span>}
                    </p>
                    <p className="mt-1 break-words text-sm">
                        {field.type === "money" && currency && moneyText(value, currency.scale)
                            ? `${formatSummaryNumber(moneyText(value, currency.scale).replace(",", "."))} ${currency.code}`
                            : displayValue(field, value)}
                    </p>
                </div>
            )
        }
        const writeOnlyModified = Object.prototype.hasOwnProperty.call(formData, field.name)
        const writeOnlyConfigured = formData[`${field.name}Configured`] === true
        const fieldReadOnly = formReadOnly || forceReadOnly
        const wide = isWideField(field)
        return (
            <div key={field.name} className={wide ? "md:col-span-2" : ""} data-field={field.name}>
                <Label htmlFor={field.name} className="text-xs text-muted-foreground">
                    {field.label || field.name}
                    {field.required && !fieldReadOnly && <span className="ml-0.5 text-destructive">*</span>}
                </Label>
                <div className="mt-1">
                    {renderField?.(field, {
                        value: formData[field.name],
                        values: formData,
                        onChange: (value) => handleFieldChange(field.name, value),
                        readOnly: fieldReadOnly || !!field.readOnly,
                    }) ?? fieldToInput(
                        field,
                        formData[field.name],
                        handleFieldChange,
                        false,
                        fieldReadOnly,
                        field.name === firstEditable,
                        writeOnlyConfigured,
                        currency,
                    )}
                </div>
                {field.writeOnly && (
                    <div className="mt-1 flex items-center justify-between gap-3">
                        <p className="text-xs text-muted-foreground" role="status">
                            {writeOnlyModified
                                ? formData[field.name] === ""
                                    ? "Ключ будет очищен после записи."
                                    : writeOnlyConfigured
                                        ? "Ключ будет заменён после записи."
                                        : "Ключ будет сохранён после записи."
                                : writeOnlyConfigured
                                    ? "Ключ настроен. Сохранённое значение не отображается."
                                    : "Ключ не настроен."}
                        </p>
                        {writeOnlyConfigured && !writeOnlyModified && !fieldReadOnly && (
                            <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                disabled={saving}
                                onClick={() => handleFieldChange(field.name, "")}
                            >
                                Очистить
                            </Button>
                        )}
                    </div>
                )}
            </div>
        )
    }

    const renderTablePart = (tp: (typeof tableParts)[number], bare: boolean) => (
        <TablePartEditor
            key={tp.name}
            part={tp}
            rows={z.array(jsonObjectSchema).parse(formData[tp.name] ?? [])}
            writable={tablePartFields[tp.name] ?? null}
            readOnly={formReadOnly}
            disabled={saving}
            hiddenColumns={HIDDEN_FIELDS}
            bare={bare}
            onChange={(rows) => handleFieldChange(tp.name, rows)}
            renderInput={(column, value, onChange) => fieldToInput(column, value, (_, next) => onChange(next), saving, false)}
            widthsKey={`${entityName}.${tp.name}`}
            focusRequest={lineFocus?.part === tp.name ? lineFocus : null}
            currency={currency}
        />
    )
    const title = isNew
        ? (tabTitle ?? meta.presentation?.new ?? `Новый: ${meta.presentation?.singular ?? entityName}`)
        : (meta.presentation?.singular ?? entityName)

    // ── Build document status badge ──────────────────────────────────────
    const hasReasonedActions = meta.actions?.some((action) => action.input?.kind === "reasonedDecision")
    const docStatus = isDocument && !isNew
        ? hasReasonedActions && formData.status === "rejected"
            ? { label: "Отклонён", variant: "destructive" as const }
            : hasReasonedActions && formData.status === "pending_approval"
                ? { label: "Ожидает решения", variant: "outline" as const }
        : formData.posted
            ? { label: "Проведён", variant: "success" as const }
            : formData.deletionMark
                ? { label: "Помечен на удаление", variant: "destructive" as const }
                : { label: "Черновик", variant: "outline" as const }
        : undefined

    const customFormActions = (meta.actions ?? [])
        .filter((action) => isRenderableItemAction(action, "formToolbar"))
        .filter((action) => isActionAllowedInState(action, formData.status))
        .filter((action) => !action.requiresSystemAdmin || systemAdmin)
    const customMenuActions = (meta.actions ?? [])
        .filter((action) => isRenderableItemAction(action, "toolbarMenu"))
        .filter((action) => isActionAllowedInState(action, formData.status))
        .filter((action) => !action.requiresSystemAdmin || systemAdmin)

    // ── Build toolbar props based on entity type and capabilities ───────
    const canSaveCurrent = !readOnly && (isNew
        ? canCreate
        : isDocument
            ? formData.posted ? canRepost : canUpdate
            : canUpdate)
    const supportsSaveCurrent = isNew
        ? supportsCreate
        : isDocument
            ? formData.posted ? supportsRepost : supportsUpdate
            : supportsUpdate
    const canPostCurrent = canPost && !formData.posted
    const canUnpostCurrent = canUnpost && !!formData.posted
    const supportsPostCurrent = supportsPost && !formData.posted
    const supportsUnpostCurrent = supportsUnpost && !!formData.posted

    const toolbarPrimaryAction = supportsPostCurrent
        ? {
            label: saving ? "Сохранение…" : "Провести и закрыть",
            variant: "default" as const,
            onClick: () => postCurrentDocument(true),
            disabled: saving || !canPostCurrent,
        }
        : supportsSaveCurrent
            ? {
                label: saving ? "Сохранение…" : "Записать и закрыть",
                onClick: () => handleSave(true),
                disabled: saving || !canSaveCurrent,
            }
            : undefined

    const toolbarSecondaryActions = [
        ...(!isNew ? [{ label: "Перезагрузить", onClick: requestReload, disabled: saving || refreshing }] : []),
        ...(supportsSaveCurrent ? [{ label: "Записать", onClick: () => handleSave(false), disabled: saving || !canSaveCurrent }] : []),
        ...(supportsPostCurrent ? [{ label: "Провести", onClick: () => postCurrentDocument(false), disabled: saving || !canPostCurrent }] : []),
        ...(supportsUnpostCurrent ? [{ label: "Отменить проведение", onClick: handleUnpost, disabled: saving || !canUnpostCurrent }] : []),
        ...customFormActions.map((action) => ({
            label: action.label,
            onClick: () => requestCustomAction(action),
            disabled: saving || decision.busy || decision.pending || (action.input?.kind === "reasonedDecision" && (loading || refreshing || !!error || !actionAvailability?.[action.key]?.allowed)),
        })),
    ]

    const toolbarExtraMenuItems = [
        ...(supportsSetDeletionMark ? [{
            label: formData.deletionMark ? "Снять пометку удаления" : "Пометить на удаление",
            onClick: handleToggleDeletionMark,
            destructive: !formData.deletionMark,
            disabled: saving || !canSetDeletionMark,
        }] : []),
        ...customMenuActions.map((action) => ({
            label: action.label,
            onClick: () => requestCustomAction(action),
            destructive: action.destructive,
            disabled: saving || decision.busy || decision.pending || (action.input?.kind === "reasonedDecision" && (loading || refreshing || !!error || !actionAvailability?.[action.key]?.allowed)),
        })),
    ]

    return (
        <div className={hasFormContent ? "relative flex h-full flex-col" : "relative flex h-full flex-col animate-skeleton-fade-in"}>
            {error && (
                <Alert variant="destructive">
                    <AlertDescription>{error}</AlertDescription>
                </Alert>
            )}
            {formState.conflict && (
                <Alert variant="destructive">
                    <AlertTitle>Данные на сервере изменились</AlertTitle>
                    <AlertDescription>Ваш ввод сохранён. Перезагрузите карточку, чтобы принять серверную версию; несохранённые изменения будут удалены после подтверждения.</AlertDescription>
                </Alert>
            )}
            {refreshing && (
                <div className="pointer-events-none absolute inset-x-0 top-0 z-20 h-px bg-primary/60" />
            )}
            <FormToolbar
                title={title}
                status={docStatus}
                primaryAction={toolbarPrimaryAction}
                secondaryActions={toolbarSecondaryActions}
                extraMenuItems={toolbarExtraMenuItems.length > 0 ? toolbarExtraMenuItems : undefined}
                printMenu={canPrint && id ? <PrintMenuButton documentType={routePrefix} documentId={id} /> : undefined}
                toolbarIcons={isDocument && !isNew ? [
                    ...(formData.posted && canShowMovements ? [{
                        icon: <ArrowRightLeft className="h-3.5 w-3.5" />,
                        title: "Движения документа",
                        onClick: () => router.push(`${buildEntityUrlByRoute(routePrefix, "document", id)}/movements`),
                    }] : []),
                    ...(canShowRelatedDocuments ? [{
                        icon: <Network className="h-3.5 w-3.5" />,
                        title: "Связанные документы",
                        onClick: () => router.push(`${buildEntityUrlByRoute(routePrefix, "document", id)}/related`),
                    }] : []),
                ] : undefined}
                backHref={listPath}
                backTargetId={id === "new" ? undefined : id}
                favoriteTarget={!isNew && id ? {
                    entityType: entityName,
                    entityId: id,
                    title: `${entityLabel} ${displayNumber || ""}`,
                    url: buildEntityUrlByRoute(routePrefix, entityType, id),
                } : undefined}
            />

            {/* The form's content keeps the panel's width (Radix lays the viewport's
                content out as a table that grows to its widest child), so a wide
                table part scrolls sideways inside its own frame, not the page. */}
            <ScrollArea className="flex-1 [&_[data-radix-scroll-area-viewport]>div]:!block [&_[data-radix-scroll-area-viewport]>div]:!min-w-0">
                <div className="p-6">
                    <div className="max-w-3xl space-y-6">
                        {hasReasonedActions && formData.status === "pending_approval" && customFormActions.some((action) => !actionAvailability?.[action.key]?.allowed) && <Alert>
                            <AlertTitle>Ограничения принятия решения</AlertTitle>
                            <AlertDescription>{customFormActions.filter((action) => !actionAvailability?.[action.key]?.allowed).map((action) => <p key={action.key}>{action.label}: {decisionUnavailableReason(actionAvailability?.[action.key])}</p>)}</AlertDescription>
                        </Alert>}
                        {hasReasonedActions && formData.decisionMode === "admin_self" && <Alert>
                            <AlertTitle>Решение принято автором-администратором</AlertTitle>
                            <AlertDescription>Без независимого оператора. Версия политики: {String(formData.decisionPolicyVersion)}.</AlertDescription>
                        </Alert>}
                        {hasReasonedActions && formData.decisionMode === "independent" && <Alert>
                            <AlertTitle>Независимое решение</AlertTitle>
                            <AlertDescription>Решение принято другим оператором. Версия политики: {String(formData.decisionPolicyVersion)}.</AlertDescription>
                        </Alert>}
                        {/* Auto-generated fields, placed by lib/form-layout */}
                        {layout.header.length > 0 && (
                            <div className="grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-2" data-form-zone="header">
                                {layout.header.map((field) => renderFieldBlock(field))}
                            </div>
                        )}
                        {layout.groups.map((group) => (
                            <section key={group.key} aria-label={group.title} className="space-y-3" data-form-group={group.key}>
                                {group.title && <h3 className="border-b pb-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{group.title}</h3>}
                                <div className="grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-2">
                                    {group.fields.map((field) => renderFieldBlock(field, !!field.calc))}
                                </div>
                            </section>
                        ))}
                        {layout.wide.length > 0 && (
                            <div className="grid grid-cols-1 gap-y-4" data-form-zone="wide">
                                {layout.wide.map((field) => renderFieldBlock(field))}
                            </div>
                        )}
                    </div>

                    {/* Table parts take the full width of the form, as in 1C and SAP:
                        the header fields stay a readable column, the grid gets room
                        for its columns. "Дополнительно" goes back to the column. */}
                    <div className="space-y-6">
                        {layout.partsMode === "inline" && (
                            <>
                                {tableParts.map((tp) => renderTablePart(tp, false))}
                                {layout.extra.length > 0 && (
                                    <Collapsible className="mt-6 max-w-3xl rounded border" data-form-zone="extra">
                                        <CollapsibleTrigger className="group flex w-full items-center gap-2 px-3 py-2 text-left text-sm font-medium">
                                            <ChevronRight className="h-3.5 w-3.5 transition-transform group-data-[state=open]:rotate-90" />
                                            Дополнительно <span className="text-muted-foreground">({layout.extra.length})</span>
                                        </CollapsibleTrigger>
                                        <CollapsibleContent>
                                            <div className="grid grid-cols-1 gap-x-6 gap-y-4 border-t px-3 py-3 md:grid-cols-2">
                                                {layout.extra.map((field) => renderFieldBlock(field, true))}
                                            </div>
                                        </CollapsibleContent>
                                    </Collapsible>
                                )}
                            </>
                        )}
                        {layout.partsMode === "tabs" && (
                            <Tabs value={activeTab} onValueChange={setActivePart} data-form-zone="parts" className="mt-6">
                                <TabsList>
                                    {tableParts.map((tp) => {
                                        const count = Array.isArray(formData[tp.name]) ? (formData[tp.name] as JsonValue[]).length : 0
                                        return (
                                            <TabsTrigger key={tp.name} value={tp.name} className="gap-1.5">
                                                {tp.label || tp.name}
                                                <span className="tabular-nums text-muted-foreground">{count}</span>
                                                {partError === tp.name && <span className="h-1.5 w-1.5 rounded-full bg-destructive" aria-label="Есть ошибка" />}
                                            </TabsTrigger>
                                        )
                                    })}
                                    {layout.extra.length > 0 && (
                                        <TabsTrigger value={EXTRA_TAB} className="gap-1.5">
                                            Дополнительно <span className="tabular-nums text-muted-foreground">{layout.extra.length}</span>
                                        </TabsTrigger>
                                    )}
                                </TabsList>
                                {tableParts.map((tp) => (
                                    <TabsContent key={tp.name} value={tp.name} className="mt-3">
                                        {renderTablePart(tp, true)}
                                    </TabsContent>
                                ))}
                                {layout.extra.length > 0 && (
                                    <TabsContent value={EXTRA_TAB} className="mt-3 grid max-w-3xl grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-2">
                                        {layout.extra.map((field) => renderFieldBlock(field, true))}
                                    </TabsContent>
                                )}
                            </Tabs>
                        )}
                    </div>

                    {/* Entity-specific extensions (e.g. API keys, user access).
                        Rendered inside ScrollArea so they scroll with the form. */}
                    {appendContent && (
                        <div className="mt-6 pb-8">
                            {appendContent}
                        </div>
                    )}
                </div>
            </ScrollArea>

            <ReasonedDecisionDialog controller={decision} />
            <AlertDialog open={confirmReload} onOpenChange={setConfirmReload}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>Перезагрузить карточку?</AlertDialogTitle>
                        <AlertDialogDescription>Несохранённые изменения будут заменены данными с сервера.</AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>Отмена</AlertDialogCancel>
                        <AlertDialogAction onClick={() => { setConfirmReload(false); void refetchDoc(true).catch(() => undefined) }}>Перезагрузить</AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
            <AlertDialog open={pendingAction !== null} onOpenChange={(open) => { if (!open) setPendingAction(null) }}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>{pendingAction?.confirmation?.title}</AlertDialogTitle>
                        <AlertDialogDescription>{pendingAction?.confirmation?.description}</AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>{pendingAction?.confirmation?.cancelLabel ?? "Отмена"}</AlertDialogCancel>
                        <AlertDialogAction
                            onClick={() => {
                                const action = pendingAction
                                setPendingAction(null)
                                if (action) void handleCustomAction(action)
                            }}
                        >
                            {pendingAction?.confirmation?.confirmLabel ?? pendingAction?.label}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </div>
    )
}
