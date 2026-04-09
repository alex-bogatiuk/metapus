"use client"

/**
 * Catch-all document list page — renders metadata-driven AutoList
 * for document entities without a dedicated page component.
 *
 * Route: /documents/[routePrefix]
 */

import { Suspense, use } from "react"
import { entityRegistry } from "@/lib/entity-registry"
import { registerFromMetadata } from "@/lib/entity-registry-defaults"
import { useMetadataStore } from "@/stores/useMetadataStore"
import AutoList from "@/components/shared/auto-list"
import { Loader2 } from "lucide-react"

// Auto-discover entities from backend metadata
registerFromMetadata()

interface PageProps {
    params: Promise<{
        routePrefix: string
    }>
}

function LoadingFallback() {
    return (
        <div className="flex items-center justify-center py-20">
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        </div>
    )
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
    const entityName = registration?.entityName ?? entity?.key ?? toPascalCase(routePrefix)

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

/** Convert kebab-case route prefix to PascalCase entity name */
function toPascalCase(s: string): string {
    return s
        .split("-")
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join("")
}
