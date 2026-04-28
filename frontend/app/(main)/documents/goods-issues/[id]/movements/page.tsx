"use client"

import { useParams } from "next/navigation"
import { DocumentMovementsPage } from "@/components/shared/document-movements-page"
import { api } from "@/lib/api"
import { useMetadataStore } from "@/stores/useMetadataStore"

export default function GoodsIssueMovementsRoute() {
    const params = useParams<{ id: string }>()
    const label = useMetadataStore((s) => s.getLabel("goods_issue", "singular"))

    return (
        <DocumentMovementsPage
            documentId={params.id}
            backHref={`/documents/goods-issues/${params.id}`}
            documentLabel={label}
            numberFetcher={(id) => api.goodsIssues.get(id)}
            fetcher={(id) => api.goodsIssues.getMovements(id)}
        />
    )
}
