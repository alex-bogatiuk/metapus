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
            documentTitle={`Реализация ${params.id.slice(0, 8)}…`}
            fetcher={(id) => api.goodsIssues.getMovements(id)}
        />
    )
}
