"use client"

import { useParams } from "next/navigation"
import { DocumentMovementsPage } from "@/components/shared/document-movements-page"
import { api } from "@/lib/api"

export default function GoodsReceiptMovementsRoute() {
    const params = useParams<{ id: string }>()

    return (
        <DocumentMovementsPage
            documentId={params.id}
            backHref={`/documents/goods-receipts/${params.id}`}
            documentTitle={`Поступление ${params.id.slice(0, 8)}…`}
            fetcher={(id) => api.goodsReceipts.getMovements(id)}
        />
    )
}
