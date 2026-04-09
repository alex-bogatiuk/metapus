/**
 * Entity Registry — Auto-discovery from backend metadata.
 *
 * This is the frontend counterpart of backend content.RegisterDefaults().
 * Instead of hardcoding entity registrations, it auto-discovers entities
 * from the /meta/entities endpoint and registers them in the UIRegistry.
 *
 * Custom columns can be overlaid per entity for richer list views.
 * Custom components are registered separately per entity when needed.
 *
 * Usage:
 *   await registerFromMetadata()  // called once on app init
 *   entityRegistry.registerCatalog({ entityName: "Vehicle", ... })  // extension overlay
 */

import { entityRegistry } from "./entity-registry"
import type { AutoListColumn } from "./entity-registry"
import { useMetadataStore } from "@/stores/useMetadataStore"

// ── Custom Column Overlays ──────────────────────────────────────────────
// These define richer column sets for entities that need more than auto-generation.
// If an entity is not listed here, AutoList will generate columns from metadata fields.

const columnOverlays: Record<string, AutoListColumn[]> = {
    Counterparty: [
        { key: "code", label: "Код", width: "120px" },
        { key: "name", label: "Наименование" },
        { key: "inn", label: "ИНН", width: "150px" },
        { key: "contactPerson", label: "Контактное лицо", width: "180px" },
        { key: "phone", label: "Телефон", width: "150px" },
    ],
    Nomenclature: [
        { key: "code", label: "Код", width: "120px" },
        { key: "name", label: "Наименование" },
        { key: "isFolder", label: "Группа", type: "boolean", width: "80px" },
    ],
    Warehouse: [
        { key: "code", label: "Код", width: "120px" },
        { key: "name", label: "Наименование" },
    ],
    Organization: [
        { key: "code", label: "Код", width: "120px" },
        { key: "name", label: "Наименование" },
        { key: "inn", label: "ИНН", width: "150px" },
    ],
}

// ── Auto-Discovery from Backend Metadata ────────────────────────────────

let registered = false

/**
 * Registers all entities from backend metadata into the UIRegistry.
 * Must be called AFTER useMetadataStore.fetch() completes.
 *
 * Entities already registered (e.g. by extensions) will NOT be overwritten.
 */
export function registerFromMetadata(): void {
    if (registered) return
    registered = true

    const { entities } = useMetadataStore.getState()

    for (const entity of entities) {
        const type = entity.type as "catalog" | "document"
        const routePrefix = entity.routePrefix ?? entity.key

        // Skip if already registered by extension or custom code
        if (type === "catalog" && entityRegistry.getCatalog(entity.name)) continue
        if (type === "document" && entityRegistry.getDocument(entity.name)) continue

        const reg = {
            entityType: type,
            entityName: entity.name,
            routePrefix,
            listColumns: columnOverlays[entity.name],
        }

        if (type === "catalog") {
            entityRegistry.registerCatalog(reg)
        } else {
            entityRegistry.registerDocument(reg)
        }
    }
}
