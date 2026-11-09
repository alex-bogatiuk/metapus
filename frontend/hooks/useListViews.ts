"use client"

/**
 * useListViews — manages saved list views (filter presets) for an entity type.
 *
 * Loads views from backend, tracks the active view, and provides CRUD actions.
 * Designed to integrate with FilterSidebar and useEntityListPage.
 */

import { useState, useEffect, useCallback, useRef } from "react"
import { usePathname } from "next/navigation"
import { toast } from "sonner"
import { api, readCachedListViews } from "@/lib/api"
import { useTabStateStore, createTabStateKey, type TabStateKey } from "@/stores/useTabStateStore"
import type { ListView, ListViewConfig, ViewVisibility } from "@/types/list-view"

// ── Types ───────────────────────────────────────────────────────────────

interface UseListViewsOptions {
    /** Entity key (e.g. "CryptoInvoice", "token"). */
    entityType: string
    /** Called when a saved view is selected — apply its config. */
    onApplyConfig?: (config: ListViewConfig) => void
    /** Returns the current list state for saving. */
    getCurrentConfig?: () => ListViewConfig
}

interface UseListViewsReturn {
    /** All views available for this entity type. */
    views: ListView[]
    /** Currently active view ID (null = no saved view selected). */
    activeViewId: string | null
    /** True during initial load. */
    loading: boolean

    // ── Actions ─────────────────────────────────────────────────────
    /** Select a saved view and apply its config. Pass null to clear. */
    selectView: (id: string | null) => void
    /** Save current filters/columns/sort as a new named view. */
    saveCurrentAsView: (name: string, visibility?: ViewVisibility) => Promise<void>
    /** Overwrite the config of an existing view with current state. */
    overwriteView: (id: string) => Promise<void>
    /** Rename an existing view. */
    renameView: (id: string, name: string) => Promise<void>
    /** Delete a view. */
    deleteView: (id: string) => Promise<void>
    /** Mark a view as default for this entity. */
    setDefault: (id: string) => Promise<void>
}

// ── Hook ────────────────────────────────────────────────────────────────

function getDefaultView(views: ListView[]): ListView | undefined {
    return views.find((view) => view.isDefault)
}

const activeViewKeys = new Map<string, TabStateKey<string | null>>()
const defaultAppliedKeys = new Map<string, TabStateKey<string>>()

function activeViewCacheKey(entityType: string): TabStateKey<string | null> {
    let key = activeViewKeys.get(entityType)
    if (!key) { key = createTabStateKey<string | null>(`listViews.activeViewId.${entityType}`); activeViewKeys.set(entityType, key) }
    return key
}

function defaultAppliedCacheKey(entityType: string): TabStateKey<string> {
    let key = defaultAppliedKeys.get(entityType)
    if (!key) { key = createTabStateKey<string>(`listViews.defaultApplied.${entityType}`); defaultAppliedKeys.set(entityType, key) }
    return key
}

export function useListViews(options: UseListViewsOptions): UseListViewsReturn {
    const { entityType, onApplyConfig, getCurrentConfig } = options
    const pathname = usePathname()
    const normalizedEntityType = entityType.trim()
    const cachedViews = normalizedEntityType ? readCachedListViews(normalizedEntityType) : undefined
    const cachedActiveViewId = normalizedEntityType
        ? useTabStateStore.getState().get(pathname, activeViewCacheKey(normalizedEntityType)) as string | null | undefined
        : undefined

    const [views, setViews] = useState<ListView[]>(() => cachedViews ?? [])
    const [activeViewId, setActiveViewId] = useState<string | null>(() => {
        if (cachedActiveViewId !== undefined) return cachedActiveViewId
        return getDefaultView(cachedViews ?? [])?.id ?? null
    })
    // Derive loading from fetch status — initialised to "loading" when entityType is present,
    // avoiding the need to call setState synchronously inside the effect body.
    const [fetchStatus, setFetchStatus] = useState<"loading" | "done">(
        normalizedEntityType && cachedViews === undefined ? "loading" : "done",
    )
    const loading = fetchStatus === "loading"

    // Keep stable references for callbacks.
    const onApplyRef = useRef(onApplyConfig)
    useEffect(() => { onApplyRef.current = onApplyConfig }, [onApplyConfig])
    const getConfigRef = useRef(getCurrentConfig)
    useEffect(() => { getConfigRef.current = getCurrentConfig }, [getCurrentConfig])

    const setCachedActiveViewId = useCallback((id: string | null) => {
        if (!normalizedEntityType) return
        useTabStateStore.getState().set(pathname, activeViewCacheKey(normalizedEntityType), id)
    }, [normalizedEntityType, pathname])

    const applyDefaultViewOnce = useCallback((nextViews: ListView[]) => {
        if (!normalizedEntityType) return
        const defaultView = getDefaultView(nextViews)
        if (!defaultView) return

        const store = useTabStateStore.getState()
        const activeKey = activeViewCacheKey(normalizedEntityType)
        const appliedKey = defaultAppliedCacheKey(normalizedEntityType)
        const cachedActive = store.get(pathname, activeKey) as string | null | undefined

        if (cachedActive !== undefined) return

        const appliedDefaultId = store.get(pathname, appliedKey) as string | undefined
        setActiveViewId(defaultView.id)
        store.set(pathname, activeKey, defaultView.id)
        if (appliedDefaultId !== defaultView.id) {
            store.set(pathname, appliedKey, defaultView.id)
            onApplyRef.current?.(defaultView.config)
        }
    }, [normalizedEntityType, pathname])

    // ── Load views on mount ─────────────────────────────────────────
    // fetchStatus initialised above; only .then/.finally callbacks call setState.
    useEffect(() => {
        if (!normalizedEntityType) return
        let cancelled = false
        const cached = readCachedListViews(normalizedEntityType)

        if (cached !== undefined) {
            setViews(cached)
            const cachedActive = useTabStateStore.getState().get(
                pathname,
                activeViewCacheKey(normalizedEntityType),
            ) as string | null | undefined
            setActiveViewId(cachedActive !== undefined ? cachedActive : getDefaultView(cached)?.id ?? null)
            applyDefaultViewOnce(cached)
            setFetchStatus("done")
            return () => { cancelled = true }
        }

        setFetchStatus("loading")
        api.listViews.list(normalizedEntityType).then((data) => {
            if (cancelled) return
            const nextViews = data ?? []
            setViews(nextViews)
            applyDefaultViewOnce(nextViews)
        }).catch((err) => {
            console.error("Failed to load list views:", err)
        }).finally(() => {
            if (!cancelled) setFetchStatus("done")
        })

        return () => { cancelled = true }
    }, [normalizedEntityType, pathname, applyDefaultViewOnce])

    // ── Select view ─────────────────────────────────────────────────
    const selectView = useCallback((id: string | null) => {
        setActiveViewId(id)
        setCachedActiveViewId(id)
        if (id) {
            const view = views.find((v) => v.id === id)
            if (view) {
                onApplyRef.current?.(view.config)
            }
        }
        // When id is null, the caller handles reset (existing "Сбросить" in FilterSidebar).
    }, [views, setCachedActiveViewId])

    // ── Save current as new view ────────────────────────────────────
    const saveCurrentAsView = useCallback(async (
        name: string,
        visibility: ViewVisibility = "personal",
    ) => {
        const config = getConfigRef.current?.()
        if (!config) return

        try {
            const created = await api.listViews.create({
                entityType,
                name,
                visibility,
                isDefault: false,
                config,
            })
            setViews((prev) => [...prev, created])
            setActiveViewId(created.id)
            setCachedActiveViewId(created.id)
            toast.success(`Вид «${name}» сохранён`)
        } catch (err) {
            console.error("Failed to save list view:", err)
            toast.error("Не удалось сохранить вид")
            throw err
        }
    }, [entityType, setCachedActiveViewId])

    // ── Overwrite existing view's config ─────────────────────────────
    const overwriteView = useCallback(async (id: string) => {
        const config = getConfigRef.current?.()
        if (!config) return

        const existing = views.find((v) => v.id === id)
        if (!existing) return

        try {
            const updated = await api.listViews.update(id, {
                name: existing.name,
                visibility: existing.visibility,
                isDefault: existing.isDefault,
                config,
                version: existing.version,
            })
            setViews((prev) => prev.map((v) => v.id === id ? updated : v))
            toast.success(`Вид «${existing.name}» обновлён`)
        } catch (err) {
            console.error("Failed to overwrite list view:", err)
            toast.error("Не удалось обновить вид")
            throw err
        }
    }, [views])

    // ── Rename ───────────────────────────────────────────────────────
    const renameView = useCallback(async (id: string, name: string) => {
        const existing = views.find((v) => v.id === id)
        if (!existing) return

        try {
            const updated = await api.listViews.update(id, {
                name,
                visibility: existing.visibility,
                isDefault: existing.isDefault,
                config: existing.config,
                version: existing.version,
            })
            setViews((prev) => prev.map((v) => v.id === id ? updated : v))
            toast.success(`Вид переименован: «${name}»`)
        } catch (err) {
            console.error("Failed to rename list view:", err)
            toast.error("Не удалось переименовать вид")
            throw err
        }
    }, [views])

    // ── Delete ───────────────────────────────────────────────────────
    const deleteView = useCallback(async (id: string) => {
        const existing = views.find((v) => v.id === id)
        try {
            await api.listViews.delete(id)
            setViews((prev) => prev.filter((v) => v.id !== id))
            if (activeViewId === id) {
                setActiveViewId(null)
                setCachedActiveViewId(null)
            }
            toast.success(`Вид «${existing?.name ?? ""}» удалён`)
        } catch (err) {
            console.error("Failed to delete list view:", err)
            toast.error("Не удалось удалить вид")
            throw err
        }
    }, [activeViewId, views, setCachedActiveViewId])

    // ── Set default ──────────────────────────────────────────────────
    const setDefault = useCallback(async (id: string) => {
        const target = views.find((v) => v.id === id)
        try {
            await api.listViews.setDefault(id)
            setViews((prev) => prev.map((v) => ({
                ...v,
                isDefault: v.id === id,
            })))
            toast.success(`Вид «${target?.name ?? ""}» установлен по умолчанию`)
        } catch (err) {
            console.error("Failed to set default list view:", err)
            toast.error("Не удалось установить вид по умолчанию")
            throw err
        }
    }, [views])

    return {
        views,
        activeViewId,
        loading,
        selectView,
        saveCurrentAsView,
        overwriteView,
        renameView,
        deleteView,
        setDefault,
    }
}
