"use client"

import { useCloseTab } from "@/hooks/useCloseTab"
import { useFormDraft } from "@/hooks/useFormDraft"
import { useFormValidation, type FieldRule } from "@/hooks/useFormValidation"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useTabTitle } from "@/hooks/useTabTitle"
import { ApiError } from "@/lib/api"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { useParams, usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useEffectEvent, useRef, useState } from "react"

// ── Types ───────────────────────────────────────────────────────────────

interface CatalogFormApi<TRes, TCreate, TUpdate> {
  /** Fetch entity by ID (edit mode only). */
  get?: (id: string) => Promise<TRes>
  /** Create a new entity (new mode only). */
  create?: (data: TCreate) => Promise<{ id: string }>
  /** Update an existing entity (edit mode only). */
  update?: (id: string, data: TUpdate) => Promise<TRes>
}

interface UseCatalogFormOptions<TState, TRes, TCreate, TUpdate> {
  draftKey: import("@/stores/useFormDraftStore").FormDraftKey<TState>
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
  /** Adopt canonical server fields after save (for forms disabled while saving). */
  replaceOnSave?: boolean
  /** Compare an existing record's restored draft with the server without overwriting user input. */
  checkDraftOnLoad?: boolean
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
  /** Reload the saved record, or reset a new form, and discard its draft. */
  reset: () => Promise<boolean>
  clearErrors: () => void
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
  /** Id of the edited entity; undefined while creating. */
  entityId?: string
  /** Resolved entity display name (from metadata store or fallback). */
  entityLabel: string
  /** Form heading that identifies unsaved creation and copying. */
  formTitle: string
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
  TState extends object,
  TRes = never,
  TCreate = never,
  TUpdate = never,
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
    replaceOnSave,
    checkDraftOnLoad,
  } = options

  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()
  const copyFromId = searchParams?.get("copyFrom")
  const params = useParams<{ id?: string }>()
  const entityId = params?.id === "new" ? undefined : params?.id
  const isEditMode = !!entityId
  const { closeAndShowInList } = useCloseTab()

  const { markDirty, markClean } = useTabDirty()
  const { state: f, update: draftUpdate, replace, clear, hasDraft } = useFormDraft<TState>(
    pathname,
    options.draftKey,
    initialState,
  )

  const [saving, setSaving] = useState(false)
  const mutationInFlight = useRef(false)
  const [error, setError] = useState<string | null>(null)
  const [backendFieldErrors, setBackendFieldErrors] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(
    isEditMode ? !hasDraft || !!checkDraftOnLoad : !!copyFromId && !hasDraft,
  )
  const [deletionMark, setDeletionMark] = useState(false)

  // ── M15: On-blur field validation with shake + scroll-to-error ──────
  const validation = useFormValidation<TState>({
    rules: fieldRules ?? [],
  })

  // Tab title — resolve from metadata store when entityKey is provided
  const metaLabel = useMetadataStore((s) => options.entityKey ? s.getLabel(options.entityKey, "singular") : undefined)
  const resolvedEntityName = metaLabel || entityName
  const displayTitle = titleField ? titleField(f) : undefined
  const tabTitle = useTabTitle(displayTitle, resolvedEntityName, isEditMode ? "edit" : copyFromId ? "copy" : "create")
  const formTitle = isEditMode ? displayTitle || resolvedEntityName : tabTitle || resolvedEntityName

  const getEntity = entityApi.get
  const canMapResponse = !!mapFromResponse
  const applyResponse = useEffectEvent((res: TRes) => {
    if (!mapFromResponse) return
    const mapped = mapFromResponse(res)
    if (hasDraft && checkDraftOnLoad) {
      if (JSON.stringify(mapped) !== JSON.stringify(f)) markDirty()
    } else {
      replace(mapped)
    }
    if (getDeletionMark) setDeletionMark(getDeletionMark(res))
  })

  useEffect(() => {
    if (!isEditMode || !entityId || (hasDraft && !checkDraftOnLoad) || !getEntity || !canMapResponse) return
    let cancelled = false
    setLoading(true)
    getEntity(entityId)
      .then((res) => { if (!cancelled) applyResponse(res) })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Не удалось загрузить карточку. Проверьте соединение или обновите страницу.")
      })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [entityId, isEditMode, hasDraft, getEntity, canMapResponse, checkDraftOnLoad])

  useEffect(() => {
    if (isEditMode || !copyFromId || hasDraft || !getEntity || !canMapResponse) return
    let cancelled = false
    setLoading(true)
    getEntity(copyFromId)
      .then((res) => {
        if (!cancelled && mapFromResponse) {
          const mapped = mapFromResponse(res)
          const copied = { ...mapped }
          delete (copied as { id?: string }).id
          delete (copied as { code?: string }).code
          delete (copied as { number?: string }).number
          delete (copied as { version?: number }).version
          delete (copied as { deletionMark?: boolean }).deletionMark
          delete (copied as { createdAt?: string }).createdAt
          delete (copied as { updatedAt?: string }).updatedAt
          delete (copied as { createdBy?: string }).createdBy
          delete (copied as { updatedBy?: string }).updatedBy
          replace(copied)
          markDirty()
        }
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Не удалось скопировать элемент.")
      })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [isEditMode, copyFromId, hasDraft, getEntity, canMapResponse, mapFromResponse, replace, markDirty])

  // ── Public API ──────────────────────────────────────────────────────

  const update = useCallback(
    (patch: Partial<TState>) => draftUpdate(patch),
    [draftUpdate],
  )

  const handleChange = useCallback(() => markDirty(), [markDirty])

  const closeCurrentTabOrGoToList = useCallback((savedId: string) => {
    closeAndShowInList(pathname, listPath, savedId)
  }, [closeAndShowInList, listPath, pathname])

  const handleSave = useCallback(
    async (andClose: boolean) => {
      if (mutationInFlight.current) return
      // Validate (M15: field-level rules → then legacy string validate)
      if (fieldRules && fieldRules.length > 0) {
        if (!validation.validateAll(f)) return
      }
      if (validate) {
        const err = validate(f)
        if (err) { setError(err); return }
      }

      mutationInFlight.current = true
      setSaving(true)
      setError(null)
      setBackendFieldErrors({})
      validation.clearAllErrors()

      try {
        if (isEditMode && entityId && entityApi.update && mapToUpdate) {
          // Update existing
          const res = await entityApi.update(entityId, mapToUpdate(f))
          // Update version in form state if applicable
          if (replaceOnSave && mapFromResponse) {
            replace(mapFromResponse(res))
          } else if (getVersion) {
            const version = getVersion(res)
            draftUpdate(prev => ({ ...prev, version }))
          }
          if (getDeletionMark) setDeletionMark(getDeletionMark(res))
          clear()
          markClean()
          if (andClose) closeCurrentTabOrGoToList(entityId)
        } else if (!isEditMode && entityApi.create && mapToCreate) {
          // Create new
          const created = await entityApi.create(mapToCreate(f))
          clear()
          markClean()
          if (andClose) {
            closeCurrentTabOrGoToList(created.id)
          } else {
            // Navigate to edit page for the newly created entity
            const createdId = created.id
            router.replace(`${listPath}/${createdId}`)
          }
        }
      } catch (err) {
        if (err instanceof ApiError && err.parsedBody?.code === "INVALID_REFERENCE" && typeof err.parsedBody.details?.field === "string") {
          setBackendFieldErrors({ [err.parsedBody.details.field]: err.message })
          setError(`Ошибка в поле: ${err.parsedBody.details.field}`)
        } else {
          setError(err instanceof Error ? err.message : "Не удалось сохранить изменения. Проверьте правильность заполнения полей.")
        }
      } finally {
        mutationInFlight.current = false
        setSaving(false)
      }
    },
    [
      f, isEditMode, entityId, entityApi, mapToCreate, mapToUpdate,
      validate, fieldRules, validation, getVersion, getDeletionMark,
      clear, markClean, draftUpdate, router, listPath, closeCurrentTabOrGoToList,
      replaceOnSave, mapFromResponse, replace,
    ],
  )

  const reset = useCallback(async (): Promise<boolean> => {
    if (mutationInFlight.current) return false
    mutationInFlight.current = true
    setSaving(true)
    setError(null)
    try {
      if (isEditMode && entityId && entityApi.get && mapFromResponse) {
        const response = await entityApi.get(entityId)
        replace(mapFromResponse(response))
        if (getDeletionMark) setDeletionMark(getDeletionMark(response))
      } else {
        replace(initialState)
      }
      clear()
      markClean()
      setBackendFieldErrors({})
      validation.clearAllErrors()
      return true
    } catch (err) {
      setError(err instanceof Error ? err.message : "Не удалось загрузить сохранённые настройки. Попробуйте ещё раз.")
      return false
    } finally {
      mutationInFlight.current = false
      setSaving(false)
    }
  }, [isEditMode, entityId, entityApi, mapFromResponse, replace, getDeletionMark, initialState, clear, markClean, validation])

  const clearErrors = useCallback(() => {
    setError(null)
    setBackendFieldErrors({})
    validation.clearAllErrors()
  }, [validation])

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
    reset,
    clearErrors,
    saving,
    error,
    fieldErrors: mergedFieldErrors,
    loading,
    deletionMark,
    isEditMode,
    entityId,
    entityLabel: resolvedEntityName,
    formTitle,
    fieldProps,
  }
}
