"use client"

/**
 * Catch-all document list page — renders metadata-driven AutoList
 * for document entities without a dedicated page component.
 *
 * Route: /documents/[routePrefix]
 *
 * If routePrefix is not found in entityRegistry or metadata store,
 * the page returns 404 to prevent arbitrary slugs from rendering.
 */

import { Suspense, use } from "react"
import { notFound } from "next/navigation"
import { entityRegistry } from "@/lib/entity-registry"
import { registerFromMetadata } from "@/lib/entity-registry-defaults"
import { useMetadataStore } from "@/stores/useMetadataStore"
import AutoList from "@/components/shared/auto-list"
import { DataTableSkeleton } from "@/components/shared/data-table-skeleton"

// Auto-discover entities from backend metadata
registerFromMetadata()

interface PageProps {
    params: Promise<{
        routePrefix: string
    }>
}

function LoadingFallback() {
    return <DataTableSkeleton />
}

export default function DocumentCatchAllPage({ params }: PageProps) {
    const { routePrefix } = use(params)
    const getEntityByRoute = useMetadataStore((s) => s.getEntityByRoute)

    // Check UIRegistry for custom list component
    const registration = entityRegistry.getByRoute(routePrefix)

    if (registration?.listComponent) {
        const CustomList = registration.listComponent
        return (
            <Suspense fallback={<LoadingFallback />}>
                <CustomList entityName={registration.entityName} />
            </Suspense>
        )
    }

    const entity = getEntityByRoute(routePrefix)

    // 404 if routePrefix is not registered in either registry or metadata
    if (!registration && !entity) {
        notFound()
    }

    const entityName = registration?.entityName ?? entity?.key ?? routePrefix

    return (
        <Suspense fallback={<LoadingFallback />}>
            <AutoList
                entityName={entityName}
                entityType="document"
                routePrefix={routePrefix}
            />
        </Suspense>
    )
}
