"use client"

import { useParams } from "next/navigation"
import { DocumentMovementsPage } from "@/components/shared/document-movements-page"
import { api } from "@/lib/api"

export default function GoodsIssueMovementsRoute() {
    const params = useParams<{ id: string }>()

    return (
        <DocumentMovementsPage
            documentId={params.id}
            backHref={`/documents/goods-issues/${params.id}`}
            documentLabel="Реализация"
            numberFetcher={(id) => api.goodsIssues.get(id)}
            fetcher={(id) => api.goodsIssues.getMovements(id)}
        />
    )
}
