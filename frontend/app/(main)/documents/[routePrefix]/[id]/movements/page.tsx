"use client"

/**
 * Catch-all document movements page — renders DocumentMovementsPage
 * for any document entity without a dedicated movements route.
 *
 * Route: /documents/[routePrefix]/[id]/movements
 *
 * Uses generic apiFetch to call GET /document/{routePrefix}/{id}/movements
 * and GET /document/{routePrefix}/{id} for the document number.
 */

import { useParams } from "next/navigation"
import { DocumentMovementsPage } from "@/components/shared/document-movements-page"
import { apiFetch } from "@/lib/api"
import { useMetadataStore } from "@/stores/useMetadataStore"
import type { DocumentMovementsResponse } from "@/types/common"

export default function DocumentCatchAllMovementsRoute() {
    const params = useParams<{ routePrefix: string; id: string }>()
    const { routePrefix, id } = params

    // Resolve singular label from metadata (try byRoute first)
    const entity = useMetadataStore((s) => s.getEntityByRoute(routePrefix))
    const label = entity?.presentation?.singular ?? routePrefix

    // API base path uses singular route prefix from metadata
    const apiRoutePrefix = entity?.routePrefix ?? routePrefix
    const basePath = `/document/${apiRoutePrefix}`

    return (
        <DocumentMovementsPage
            documentId={id}
            backHref={`/documents/${routePrefix}/${id}`}
            documentLabel={label}
            numberFetcher={(docId) =>
                apiFetch<{ number: string }>(`${basePath}/${docId}`)
            }
            fetcher={(docId) =>
                apiFetch<DocumentMovementsResponse>(`${basePath}/${docId}/movements`)
            }
        />
    )
}
