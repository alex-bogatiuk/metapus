/**
 * Metadata Store — Single Source of Truth for entity display names.
 *
 * Fetches lightweight entity metadata from backend once on app init,
 * then provides helpers to resolve labels by entityKey or routePrefix.
 *
 * Usage:
 *   const { getLabel } = useMetadataStore()
 *   getLabel("goods_receipt", "plural")   // → "Goods Receipts"
 *   getLabel("counterparty", "singular")  // → "Counterparty"
 */

import { create } from "zustand"
import type { EntityMeta, EntityPresentation } from "@/types/metadata"
import { api } from "@/lib/api"

type PresentationField = keyof EntityPresentation

interface MetadataState {
    entities: EntityMeta[]
    /** key → EntityMeta index (e.g. "goods_receipt" → EntityMeta) */
    byKey: Record<string, EntityMeta>
    /** PascalCase name → EntityMeta index (e.g. "GoodsReceipt" → EntityMeta) */
    byName: Record<string, EntityMeta>
    /** routePrefix → EntityMeta index (e.g. "goods-receipt" → EntityMeta) */
    byRoute: Record<string, EntityMeta>
    loaded: boolean
    loading: boolean
}

interface MetadataActions {
    /** Fetch entity metadata from backend. Safe to call multiple times — deduplicates. */
    fetch: () => Promise<void>
    /** Get a presentation label by entity key and field. Returns fallback if not found. */
    getLabel: (entityKey: string, field?: PresentationField) => string
    /** Get full EntityMeta by key. */
    getEntity: (entityKey: string) => EntityMeta | undefined
    /** Get EntityMeta by PascalCase name (e.g. "GoodsReceipt"). */
    getEntityByName: (name: string) => EntityMeta | undefined
    /** Get EntityMeta by route prefix (e.g. "goods-receipt", "counterparties"). */
    getEntityByRoute: (routePrefix: string) => EntityMeta | undefined
    /** Get all entities of a given type. */
    getEntitiesByType: (type: "catalog" | "document") => EntityMeta[]
}

export const useMetadataStore = create<MetadataState & MetadataActions>()((set, get) => ({
    entities: [],
    byKey: {},
    byName: {},
    byRoute: {},
    loaded: false,
    loading: false,

    fetch: async () => {
        const state = get()
        if (state.loaded || state.loading) return
        set({ loading: true })
        try {
            const entities = await api.meta.listEntities()
            const byKey: Record<string, EntityMeta> = {}
            const byName: Record<string, EntityMeta> = {}
            const byRoute: Record<string, EntityMeta> = {}
            for (const e of entities) {
                byKey[e.key] = e
                byName[e.name] = e
                if (e.routePrefix) {
                    byRoute[e.routePrefix] = e
                    // Also index by pluralized form so catch-all [routePrefix] pages
                    // can resolve pluralized URL segments (e.g. "crypto-invoices").
                    // Matches pluralizePrefix() in entity-url.ts.
                    const plural = e.routePrefix.endsWith("s")
                        ? e.routePrefix
                        : e.routePrefix + "s"
                    if (plural !== e.routePrefix) {
                        byRoute[plural] = e
                    }
                }
            }
            set({ entities, byKey, byName, byRoute, loaded: true, loading: false })
        } catch {
            set({ loading: false })
        }
    },

    getLabel: (entityKey: string, field: PresentationField = "plural") => {
        const entity = get().byKey[entityKey]
        if (!entity) return entityKey
        return entity.presentation[field] ?? entityKey
    },

    getEntity: (entityKey: string) => get().byKey[entityKey],

    getEntityByName: (name: string) => get().byName[name],

    getEntityByRoute: (routePrefix: string) => get().byRoute[routePrefix],

    getEntitiesByType: (type) => get().entities.filter((e) => e.type === type),
}))
