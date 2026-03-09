"use client"

import { useState, useEffect, useCallback } from "react"
import { useRouter, usePathname, useParams } from "next/navigation"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useTabTitle } from "@/hooks/useTabTitle"
import { useFormDraft } from "@/hooks/useFormDraft"

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
  /** Entity display name for toolbar (e.g. "Контрагент"). */
  entityName: string
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
  /** Whether entity is loading (edit mode). */
  loading: boolean
  /** Whether entity has deletionMark (edit mode). */
  deletionMark: boolean
  /** Edit mode: true when editing existing entity (has `id` param). */
  isEditMode: boolean
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
  const [loading, setLoading] = useState(isEditMode && !hasDraft)
  const [deletionMark, setDeletionMark] = useState(false)

  // Tab title
  const displayTitle = titleField ? titleField(f) : undefined
  useTabTitle(displayTitle, entityName)

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
        setError(err instanceof Error ? err.message : "Ошибка загрузки")
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
      // Validate
      if (validate) {
        const err = validate(f)
        if (err) { setError(err); return }
      }

      setSaving(true)
      setError(null)

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
      } catch (err) {
        setError(err instanceof Error ? err.message : "Ошибка сохранения")
      } finally {
        setSaving(false)
      }
    },
    [
      f, isEditMode, entityId, entityApi, mapToCreate, mapToUpdate,
      validate, getVersion, getDeletionMark,
      clear, markClean, draftUpdate, router, listPath,
    ],
  )

  return {
    f,
    update,
    handleChange,
    handleSave,
    saving,
    error,
    loading,
    deletionMark,
    isEditMode,
  }
}
