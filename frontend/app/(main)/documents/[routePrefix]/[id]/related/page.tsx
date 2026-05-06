"use client"

/**
 * Catch-all related documents page — renders RelatedDocumentsPage
 * for any document entity without a dedicated related-documents route.
 *
 * Route: /documents/[routePrefix]/[id]/related
 *
 * Uses generic apiFetch to call GET /document/{routePrefix}/{id}/related-documents.
 * Document config (apiBasePath, routePrefix, entityTypeLabel) is derived from metadata.
 */

import { useParams } from "next/navigation"
import { RelatedDocumentsPage } from "@/components/shared/related-documents-page"
import { apiFetch } from "@/lib/api"
import { useMetadataStore } from "@/stores/useMetadataStore"
import type { RelatedDocumentsResponse } from "@/types/common"

export default function DocumentCatchAllRelatedRoute() {
    const params = useParams<{ routePrefix: string; id: string }>()
    const { routePrefix, id } = params

    // Resolve label + API path from metadata
    const entity = useMetadataStore((s) => s.getEntityByRoute(routePrefix))
    const label = entity?.presentation?.singular ?? routePrefix
    const apiRoutePrefix = entity?.routePrefix ?? routePrefix
    const basePath = `/document/${apiRoutePrefix}`

    return (
        <RelatedDocumentsPage
            documentId={id}
            backHref={`/documents/${routePrefix}/${id}`}
            entityTypeLabel={label}
            fetcher={(docId) =>
                apiFetch<RelatedDocumentsResponse>(`${basePath}/${docId}/related-documents`)
            }
            documentConfig={{
                apiBasePath: basePath,
                routePrefix: apiRoutePrefix,
                entityTypeLabel: label,
            }}
        />
    )
}
