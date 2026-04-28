"use client"

import { useState, useEffect, useCallback } from "react"
import { useRouter, usePathname, useParams } from "next/navigation"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useTabTitle } from "@/hooks/useTabTitle"
import { useFormDraft } from "@/hooks/useFormDraft"
import { useFormValidation, type FieldRule } from "@/hooks/useFormValidation"
import { useMetadataStore } from "@/stores/useMetadataStore"

// ── Types ───────────────────────────────────────────────────────────────

interface CatalogFormApi<TRes, TCreate, TUpdate> {
  /** Fetch entity by ID (edit mode only). */
  get?: (id: string) => Promise<TRes>
  /** Create a new entity (new mode only). */
  create?: (data: TCreate) => Promise<TRes>
  /** Update an existing entity (edit mode only). */
  update?: (id: string, data: TUpdate) => Promise<TRes>
}

interface UseCatalogFormOptions<TState, TRes, TCreate, TUpdate> {
  /** Entity display name for toolbar (e.g. "Counterparty"). Used as fallback when entityKey is not provided. */
  entityName: string
  /** Entity key for metadata-driven label resolution (e.g. "counterparty"). Overrides entityName when metadata is loaded. */
  entityKey?: string
  /** Initial form state. */
  initialState: TState
  /** API methods. */
  api: CatalogFormApi<TRes, TCreate, TUpdate>
  /** List page path for navigation after save/close. */
  listPath: string
  /** Map form state → create DTO. Required for new mode. */
  mapToCreate?: (state: TState) => TCreate
  /** Map form state → update DTO. Required for edit mode. */
  mapToUpdate?: (state: TState) => TUpdate
  /** Map API response → form state (for loading existing entity). */
  mapFromResponse?: (response: TRes) => TState
  /** Validate form state before save. Return error string or null. */
  validate?: (state: TState) => string | null
  /**
   * Per-field validation rules for on-blur validation with shake animation (M15).
   * When provided, fields with `data-field` attributes will validate on blur.
   * All rules run on submit with auto-scroll to first error.
   */
  fieldRules?: FieldRule<TState>[]
  /** Extract display title from form state (e.g. entity name). */
  titleField?: (state: TState) => string | undefined
  /** Extract version from response for optimistic concurrency. */
  getVersion?: (response: TRes) => number
  /** Check deletionMark from response for status badge. */
  getDeletionMark?: (response: TRes) => boolean
}

interface UseCatalogFormReturn<TState> {
  /** Current form state. */
  f: TState
  /** Partial update of form state. */
  update: (patch: Partial<TState>) => void
  /** Mark form as dirty (called on every field change). */
  handleChange: () => void
  /** Save handler: validates, calls create/update, navigates. */
  handleSave: (andClose: boolean) => Promise<void>
  /** Whether save is in progress. */
  saving: boolean
  /** Current error message, if any. */
  error: string | null
  /** Map of field-specific errors (merged: on-blur M15 + backend). */
  fieldErrors: Record<string, string>
  /** Whether entity is loading (edit mode). */
  loading: boolean
  /** Whether entity has deletionMark (edit mode). */
  deletionMark: boolean
  /** Edit mode: true when editing existing entity (has `id` param). */
  isEditMode: boolean
  /** Resolved entity display name (from metadata store or fallback). */
  entityLabel: string
  /**
   * Generate data-field + onBlur props for a form field (M15).
   * Usage: <Input {...fieldProps("name", f)} value={f.name} />
   */
  fieldProps: (field: string) => {
    "data-field": string
    onBlur: () => void
  }
}

// ── Hook ────────────────────────────────────────────────────────────────

/**
 * Generic hook for catalog entity form pages (both new and edit).
 *
 * Encapsulates:
 *  - useFormDraft + useTabDirty + useTabTitle wiring
 *  - saving / error / loading state
 *  - Fetch entity on mount (edit mode)
 *  - handleSave with create/update dispatch + navigation
 *
 * Usage:
 * ```tsx
 * const form = useCatalogForm<MyState, MyRes, MyCreate, MyUpdate>({ ... })
 * // form.f, form.update, form.handleChange, form.handleSave, form.saving, form.error, form.loading
 * ```
 */
export function useCatalogForm<
  TState extends Record<string, unknown>,
  TRes = unknown,
  TCreate = unknown,
  TUpdate = unknown,
>(
  options: UseCatalogFormOptions<TState, TRes, TCreate, TUpdate>,
): UseCatalogFormReturn<TState> {
  const {
    entityName,
    initialState,
    api: entityApi,
    listPath,
    mapToCreate,
    mapToUpdate,
    mapFromResponse,
    validate,
    fieldRules,
    titleField,
    getVersion,
    getDeletionMark,
  } = options

  const router = useRouter()
  const pathname = usePathname()
  const params = useParams<{ id?: string }>()
  const entityId = params?.id
  const isEditMode = !!entityId

  const { markDirty, markClean } = useTabDirty()
  const { state: f, update: draftUpdate, replace, clear, hasDraft } = useFormDraft<TState>(
    pathname,
    initialState,
  )

  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [backendFieldErrors, setBackendFieldErrors] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(isEditMode && !hasDraft)
  const [deletionMark, setDeletionMark] = useState(false)

  // ── M15: On-blur field validation with shake + scroll-to-error ──────
  const validation = useFormValidation<TState>({
    rules: fieldRules ?? [],
  })

  // Tab title — resolve from metadata store when entityKey is provided
  const metaLabel = useMetadataStore((s) => options.entityKey ? s.getLabel(options.entityKey, "singular") : undefined)
  const resolvedEntityName = metaLabel || entityName
  const displayTitle = titleField ? titleField(f) : undefined
  useTabTitle(displayTitle, resolvedEntityName)

  // ── Fetch entity (edit mode only, skipped if draft restored) ────────
  useEffect(() => {
    if (!isEditMode || !entityId || hasDraft || !entityApi.get || !mapFromResponse) return

    setLoading(true)
    entityApi.get(entityId)
      .then((res) => {
        replace(mapFromResponse(res))
        if (getDeletionMark) setDeletionMark(getDeletionMark(res))
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Не удалось загрузить карточку. Проверьте соединение или обновите страницу.")
      })
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [entityId])

  // ── Public API ──────────────────────────────────────────────────────

  const update = useCallback(
    (patch: Partial<TState>) => draftUpdate(patch),
    [draftUpdate],
  )

  const handleChange = useCallback(() => markDirty(), [markDirty])

  const handleSave = useCallback(
    async (andClose: boolean) => {
      // Validate (M15: field-level rules → then legacy string validate)
      if (fieldRules && fieldRules.length > 0) {
        if (!validation.validateAll(f)) return
      }
      if (validate) {
        const err = validate(f)
        if (err) { setError(err); return }
      }

      setSaving(true)
      setError(null)
      setBackendFieldErrors({})
      validation.clearAllErrors()

      try {
        if (isEditMode && entityId && entityApi.update && mapToUpdate) {
          // Update existing
          const res = await entityApi.update(entityId, mapToUpdate(f))
          // Update version in form state if applicable
          if (getVersion) {
            const version = getVersion(res)
            draftUpdate({ version } as unknown as Partial<TState>)
          }
          if (getDeletionMark) setDeletionMark(getDeletionMark(res))
          clear()
          markClean()
          if (andClose) router.push(listPath)
        } else if (!isEditMode && entityApi.create && mapToCreate) {
          // Create new
          const created = await entityApi.create(mapToCreate(f))
          clear()
          markClean()
          if (andClose) {
            router.push(listPath)
          } else {
            // Navigate to edit page for the newly created entity
            const createdId = (created as Record<string, unknown>).id as string
            router.replace(`${listPath}/${createdId}`)
          }
        }
      } catch (err: any) {
        if (err?.parsedBody?.code === "INVALID_REFERENCE" && err.parsedBody.details?.field) {
          setBackendFieldErrors({ [err.parsedBody.details.field]: err.message })
          setError(`Ошибка в поле: ${err.parsedBody.details.field}`)
        } else {
          setError(err instanceof Error ? err.message : "Не удалось сохранить изменения. Проверьте правильность заполнения полей.")
        }
      } finally {
        setSaving(false)
      }
    },
    [
      f, isEditMode, entityId, entityApi, mapToCreate, mapToUpdate,
      validate, fieldRules, validation, getVersion, getDeletionMark,
      clear, markClean, draftUpdate, router, listPath,
    ],
  )

  // Merge field errors: M15 validation errors + backend response errors
  const mergedFieldErrors = {
    ...validation.fieldErrors,
    ...backendFieldErrors,
  }

  // fieldProps wrapper: binds current form state so consumer only passes field name
  const fieldProps = useCallback(
    (field: string) => validation.fieldProps(field, f),
    [validation, f],
  )

  return {
    f,
    update,
    handleChange,
    handleSave,
    saving,
    error,
    fieldErrors: mergedFieldErrors,
    loading,
    deletionMark,
    isEditMode,
    entityLabel: resolvedEntityName,
    fieldProps,
  }
}
