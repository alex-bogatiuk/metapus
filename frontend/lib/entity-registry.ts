/**
 * Entity UI Registry — extensible registry for entity page components.
 *
 * Provides a centralized place to register custom list/form components for
 * catalog and document entities. If an entity has no explicit registration,
 * the system falls back to metadata-driven auto-generated components.
 *
 * This is the frontend counterpart of backend's FactoryRegistry.
 *
 * Usage:
 *   import { entityRegistry } from "@/lib/entity-registry"
 *
 *   // Register a custom entity:
 *   entityRegistry.registerCatalog({
 *       entityName: "Vehicle",
 *       routePrefix: "vehicles",
 *       listColumns: [...],
 *       formComponent: lazy(() => import("@/app/(main)/catalogs/vehicles/[id]/page")),
 *   })
 *
 *   // Lookup:
 *   const reg = entityRegistry.getCatalog("Vehicle")
 *   if (reg?.formComponent) { ... } // custom form
 *   else { ... } // fallback to AutoForm
 */

import type { ComponentType, LazyExoticComponent } from "react"

// ── Types ───────────────────────────────────────────────────────────────

export type EntityRegistrationType = "catalog" | "document"

/** Column definition for auto-generated list pages */
export interface AutoListColumn {
    /** JSON field name (e.g. "name", "inn", "totalAmount") */
    key: string
    /** Display header label */
    label: string
    /** Column type controls rendering (default: "string") */
    type?: "string" | "number" | "money" | "date" | "boolean" | "reference"
    /** For reference columns: the reference endpoint to resolve names */
    refEndpoint?: string
    /** Column width CSS (e.g. "200px", "1fr") */
    width?: string
    /** Whether this column is sortable (default: true) */
    sortable?: boolean
}

/** Props passed to custom form components rendered via UIRegistry */
export interface EntityFormProps {
    /** Entity ID from URL params (undefined for "new") */
    id?: string
    /** Entity metadata from /meta endpoint (pre-fetched) */
    entityName: string
}

/** Registration entry for a UI entity (catalog or document) */
export interface EntityUIRegistration {
    /** Entity type: "catalog" or "document" */
    entityType: EntityRegistrationType
    /** PascalCase entity name matching backend EntityName (e.g. "Counterparty", "GoodsReceipt") */
    entityName: string
    /** URL path segment matching backend RoutePrefix (e.g. "counterparties", "goods-receipt") */
    routePrefix: string
    /** Custom list columns. If undefined → auto-generate from metadata fields */
    listColumns?: AutoListColumn[]
    /** Lazy-loaded custom form component. If undefined → use AutoForm */
    formComponent?: LazyExoticComponent<ComponentType<EntityFormProps>>
    /** Lazy-loaded custom list component. If undefined → use AutoList */
    listComponent?: LazyExoticComponent<ComponentType<{ entityName: string }>>
}

// ── Registry Singleton ──────────────────────────────────────────────────

class UIRegistry {
    private catalogs = new Map<string, EntityUIRegistration>()
    private documents = new Map<string, EntityUIRegistration>()
    private byRoute = new Map<string, EntityUIRegistration>()

    registerCatalog(reg: EntityUIRegistration): void {
        reg.entityType = "catalog"
        this.catalogs.set(reg.entityName, reg)
        this.byRoute.set(reg.routePrefix, reg)
    }

    registerDocument(reg: EntityUIRegistration): void {
        reg.entityType = "document"
        this.documents.set(reg.entityName, reg)
        this.byRoute.set(reg.routePrefix, reg)
    }

    getCatalog(name: string): EntityUIRegistration | undefined {
        return this.catalogs.get(name)
    }

    getDocument(name: string): EntityUIRegistration | undefined {
        return this.documents.get(name)
    }

    getByRoute(routePrefix: string): EntityUIRegistration | undefined {
        return this.byRoute.get(routePrefix)
    }

    getByNameAndType(name: string, type: EntityRegistrationType): EntityUIRegistration | undefined {
        return type === "catalog" ? this.catalogs.get(name) : this.documents.get(name)
    }

    allCatalogs(): EntityUIRegistration[] {
        return Array.from(this.catalogs.values())
    }

    allDocuments(): EntityUIRegistration[] {
        return Array.from(this.documents.values())
    }

    /** Check if entity has a custom form registered (not using AutoForm fallback) */
    hasCustomForm(name: string): boolean {
        const catReg = this.catalogs.get(name)
        if (catReg?.formComponent) return true
        const docReg = this.documents.get(name)
        if (docReg?.formComponent) return true
        return false
    }

    /** Check if entity has a custom list registered (not using AutoList fallback) */
    hasCustomList(name: string): boolean {
        const catReg = this.catalogs.get(name)
        if (catReg?.listComponent) return true
        const docReg = this.documents.get(name)
        if (docReg?.listComponent) return true
        return false
    }
}

/** Global entity UI registry — singleton */
export const entityRegistry = new UIRegistry()
