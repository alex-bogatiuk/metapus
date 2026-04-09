"use client"

/**
 * Catch-all document form page — renders metadata-driven AutoForm
 * for document entities without a dedicated form component.
 *
 * Route: /documents/[routePrefix]/[id]
 *
 * [id] can be a UUID (edit existing) or "new" (create).
 */

import { Suspense, use } from "react"
import { entityRegistry } from "@/lib/entity-registry"
import { registerFromMetadata } from "@/lib/entity-registry-defaults"
import { useMetadataStore } from "@/stores/useMetadataStore"
import AutoForm from "@/components/shared/auto-form"
import { Loader2 } from "lucide-react"

// Auto-discover entities from backend metadata
registerFromMetadata()

interface PageProps {
    params: Promise<{
        routePrefix: string
        id: string
    }>
}

function LoadingFallback() {
    return (
        <div className="flex items-center justify-center py-20">
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        </div>
    )
}

export default function DocumentCatchAllFormPage({ params }: PageProps) {
    const { routePrefix, id } = use(params)
    const getEntityByRoute = useMetadataStore((s) => s.getEntityByRoute)

    // Check UIRegistry for custom form component
    const registration = entityRegistry.getByRoute(routePrefix)

    if (registration?.formComponent) {
        const CustomForm = registration.formComponent
        return (
            <Suspense fallback={<LoadingFallback />}>
                <CustomForm
                    id={id === "new" ? undefined : id}
                    entityName={registration.entityName}
                />
            </Suspense>
        )
    }

    const entity = getEntityByRoute(routePrefix)
    const entityName = registration?.entityName ?? entity?.key ?? toPascalCase(routePrefix)

    return (
        <Suspense fallback={<LoadingFallback />}>
            <AutoForm
                entityName={entityName}
                entityType="document"
                routePrefix={routePrefix}
                id={id === "new" ? undefined : id}
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
