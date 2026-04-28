/**
 * Favorites store — per-user entity bookmarks.
 *
 * Dedicated Zustand store to keep useUserPrefsStore lean.
 * Items are loaded once from UserPreferencesResponse.favorites
 * and persisted to the backend via PUT /me/preferences/favorites.
 *
 * Self-healing: when a form page renders with a fresh title,
 * call `refreshTitle()` to update the cached title.
 */

import { create } from "zustand"
import type { FavoriteItem } from "@/types/user-prefs"
import { api } from "@/lib/api"

const _maxFavorites = 50

interface FavoritesState {
    // Data
    items: FavoriteItem[]
    isLoaded: boolean

    // Actions
    /** Initialize from UserPreferencesResponse (called once at app boot). */
    load: (items: FavoriteItem[]) => void
    /** Add a favorite. Noop if already exists. */
    addFavorite: (item: Omit<FavoriteItem, "addedAt">) => void
    /** Remove a favorite by composite key. */
    removeFavorite: (entityType: string, entityId: string) => void
    /** Toggle favorite (add if missing, remove if present). */
    toggleFavorite: (item: Omit<FavoriteItem, "addedAt">) => void
    /** Check if an entity is favorited. */
    isFavorite: (entityType: string, entityId: string) => boolean
    /** Update cached title if it changed (self-healing). */
    refreshTitle: (entityType: string, entityId: string, newTitle: string) => void
    /** Reorder items (for drag-and-drop). */
    reorder: (fromIndex: number, toIndex: number) => void
    /** Reset store. */
    reset: () => void
}

function persistToServer(items: FavoriteItem[]) {
    api.preferences.saveFavorites(items).catch((err) => {
        console.error("Failed to save favorites:", err)
    })
}

export const useFavoritesStore = create<FavoritesState>()((set, get) => ({
    items: [],
    isLoaded: false,

    load: (items) => {
        set({ items: items ?? [], isLoaded: true })
    },

    addFavorite: (item) => {
        const { items } = get()
        // Already exists — noop
        if (items.some((f) => f.entityType === item.entityType && f.entityId === item.entityId)) {
            return
        }
        if (items.length >= _maxFavorites) {
            return // caller should show toast
        }
        const newItem: FavoriteItem = {
            ...item,
            addedAt: new Date().toISOString(),
        }
        const next = [newItem, ...items]
        set({ items: next })
        persistToServer(next)
    },

    removeFavorite: (entityType, entityId) => {
        const next = get().items.filter(
            (f) => !(f.entityType === entityType && f.entityId === entityId),
        )
        set({ items: next })
        persistToServer(next)
    },

    toggleFavorite: (item) => {
        const { isFavorite, addFavorite, removeFavorite } = get()
        if (isFavorite(item.entityType, item.entityId)) {
            removeFavorite(item.entityType, item.entityId)
        } else {
            addFavorite(item)
        }
    },

    isFavorite: (entityType, entityId) =>
        get().items.some((f) => f.entityType === entityType && f.entityId === entityId),

    refreshTitle: (entityType, entityId, newTitle) => {
        const { items } = get()
        const idx = items.findIndex(
            (f) => f.entityType === entityType && f.entityId === entityId,
        )
        if (idx === -1 || items[idx].title === newTitle) return

        const next = items.map((f, i) =>
            i === idx ? { ...f, title: newTitle } : f,
        )
        set({ items: next })
        persistToServer(next)
    },

    reorder: (fromIndex, toIndex) => {
        const { items } = get()
        if (fromIndex === toIndex) return
        if (fromIndex < 0 || fromIndex >= items.length) return
        if (toIndex < 0 || toIndex >= items.length) return

        const next = [...items]
        const [moved] = next.splice(fromIndex, 1)
        next.splice(toIndex, 0, moved)
        set({ items: next })
        persistToServer(next)
    },

    reset: () => set({ items: [], isLoaded: false }),
}))
