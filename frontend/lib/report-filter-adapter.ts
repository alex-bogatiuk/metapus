/**
 * Adapter: convert ReportMeta → FilterFieldMeta[]
 *
 * Bridges the report system's metadata format with the existing
 * FilterSidebar/FilterConfigDialog component contract.
 *
 * This enables full reuse of the entity list filter infrastructure
 * (operator selection, reference picker, date picker, between, etc.)
 * for reports, without any duplication.
 */

import type { ReportMeta, FieldTreeNode, ReportFilterDef } from "@/types/report-meta"
import type { FilterFieldMeta, FieldType } from "@/components/shared/filter-config-dialog"

// ── Public API ──────────────────────────────────────────────────────────

/**
 * Convert report metadata into FilterFieldMeta[] for FilterSidebar.
 *
 * Sources:
 * 1. meta.availableFields[] → auto-discovered fields (full tree with refs)
 * 2. meta.filters[] → explicit filter definitions (used for refEndpoint hints)
 *
 * The result is compatible with FilterSidebar's fieldsMeta prop.
 */
export function reportMetaToFilterFieldsMeta(meta: ReportMeta): FilterFieldMeta[] {
    const result: FilterFieldMeta[] = []

    if (!meta.availableFields || meta.availableFields.length === 0) {
        return result
    }

    // Build a map of explicit filter defs for ref endpoint hints
    const filterDefMap = new Map<string, ReportFilterDef>()
    for (const f of meta.filters) {
        filterDefMap.set(f.key, f)
    }

    for (const node of meta.availableFields) {
        // Skip "kind: measure" root fields — measures don't make sense as
        // FilterSidebar conditions (they are computed aggregates). However,
        // simple numeric fields (quantity, price) that are dimensions ARE valid.
        // The heuristic: skip only if kind === "measure" AND no children.
        if (node.kind === "measure" && (!node.children || node.children.length === 0)) {
            continue
        }

        const fieldMeta = fieldTreeNodeToFilterMeta(node, filterDefMap)
        if (fieldMeta) {
            result.push(fieldMeta)
        }
    }

    return result
}

// ── Converters ──────────────────────────────────────────────────────────

function fieldTreeNodeToFilterMeta(
    node: FieldTreeNode,
    filterDefMap: Map<string, ReportFilterDef>,
): FilterFieldMeta | null {
    const fieldType = mapReportTypeToFieldType(node.type)
    if (!fieldType) return null

    const filterDef = filterDefMap.get(node.key)
    const refEndpoint = resolveRefEndpoint(node, filterDef)

    const meta: FilterFieldMeta = {
        key: node.key,
        label: node.label,
        fieldType,
    }

    // Propagate enum options for dropdown rendering
    if (node.enumValues && node.enumValues.length > 0) {
        meta.enumValues = node.enumValues
    }

    if (refEndpoint) {
        meta.refEndpoint = refEndpoint
    }

    // Recursively convert children → refFields (for reference field drill-down)
    if (node.children && node.children.length > 0) {
        const refFields: FilterFieldMeta[] = []
        for (const child of node.children) {
            const childMeta = fieldTreeNodeToFilterMeta(child, filterDefMap)
            if (childMeta) {
                // Use the short name as key (the parent.child composition
                // is handled by FilterConfigDialog's flatFieldsMetaMap)
                childMeta.key = child.name
                refFields.push(childMeta)
            }
        }
        if (refFields.length > 0) {
            meta.refFields = refFields
        }
    }

    return meta
}

/**
 * Map report field types to FilterFieldMeta fieldType.
 * Returns null for unsupported types.
 */
function mapReportTypeToFieldType(type: string): FieldType | null {
    switch (type) {
        case "string":
        case "text":
            return "string"
        case "int":
        case "float":
        case "quantity":
            return "number"
        case "money":
            return "money"
        case "date":
        case "timestamp":
        case "timestamptz":
            return "date"
        case "bool":
        case "boolean":
            return "boolean"
        case "ref":
        case "uuid":
            return "reference"
        case "enum":
            return "enum"
        default:
            // Unknown type — treat as string (safe fallback for contains/eq)
            return "string"
    }
}

/**
 * Resolve the API endpoint for a reference field.
 * Priority: node.refRoute (Auto-Discovery) → explicit filterDef.ref → null
 *
 * node.refRoute is the real RoutePrefix resolved by backend's BuildFieldTree()
 * via metadata.Registry (e.g. "nomenclatures" for product_id).
 *
 * filterDef.ref is the RefEntity name (e.g. "nomenclature") which does NOT
 * always match the route prefix — use it only as a fallback.
 */
function resolveRefEndpoint(
    node: FieldTreeNode,
    filterDef: ReportFilterDef | undefined,
): string | undefined {
    // Prefer refRoute from Auto-Discovery (always matches real RoutePrefix)
    if (node.type === "ref" && node.refRoute) {
        return `/catalog/${node.refRoute}`
    }

    // Fallback: explicit filter def with ref entity name
    if (filterDef?.ref) {
        return `/catalog/${filterDef.ref}`
    }

    return undefined
}
