"use client"

/**
 * Catch-all document form page — renders metadata-driven AutoForm
 * for document entities without a dedicated form component.
 *
 * Route: /documents/[routePrefix]/[id]
 *
 * [id] can be a UUID (edit existing) or "new" (create).
 *
 * If routePrefix is not found in entityRegistry or metadata store,
 * the page returns 404 to prevent arbitrary slugs from rendering.
 */

import { Suspense, use, useEffect } from "react"
import { notFound } from "next/navigation"
import { entityRegistry } from "@/lib/entity-registry"
import { registerFromMetadata } from "@/lib/entity-registry-defaults"
import { useMetadataStore } from "@/stores/useMetadataStore"
import AutoForm from "@/components/shared/auto-form"
import { CryptoPaymentAdjustments } from "@/components/documents/crypto-payment-adjustments"
import { CryptoPaymentReceipt } from "@/components/documents/crypto-payment-receipt"

import { PaymentAMLPanel } from "@/components/crypto/aml-risk"

// Auto-discover entities from backend metadata
registerFromMetadata()

interface PageProps {
    params: Promise<{
        routePrefix: string
        id: string
    }>
}

function LoadingFallback() {
    return <div className="h-full bg-workspace-surface" aria-hidden="true" />
}

export default function DocumentCatchAllFormPage({ params }: PageProps) {
    const { routePrefix, id } = use(params)
    const getEntityByRoute = useMetadataStore((s) => s.getEntityByRoute)
    const metadataLoaded = useMetadataStore((s) => s.loaded)
    const metadataLoading = useMetadataStore((s) => s.loading)
    const fetchMetadata = useMetadataStore((s) => s.fetch)

    useEffect(() => {
        if (!metadataLoaded && !metadataLoading) {
            fetchMetadata()
        }
    }, [metadataLoaded, metadataLoading, fetchMetadata])

    if (metadataLoaded) {
        registerFromMetadata()
    }

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

    if (!metadataLoaded && !registration && !entity) {
        return <LoadingFallback />
    }

    // 404 if routePrefix is not registered in either registry or metadata
    if (!registration && !entity) {
        notFound()
    }

    const entityName = registration?.entityName ?? entity?.key ?? routePrefix
    const canonicalPrefix = entity?.routePrefix ?? registration?.routePrefix ?? routePrefix

    return (
        <Suspense fallback={<LoadingFallback />}>
            <AutoForm
                entityName={entityName}
                entityType="document"
                routePrefix={canonicalPrefix}
                id={id === "new" ? undefined : id}
                appendContent={canonicalPrefix === "crypto-payment" && id !== "new" ? <div className="space-y-4"><CryptoPaymentReceipt paymentId={id} /><PaymentAMLPanel paymentId={id} /><CryptoPaymentAdjustments paymentId={id} /></div> : undefined}
            />
        </Suspense>
    )
}
