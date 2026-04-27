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
            documentLabel="Поступление"
            numberFetcher={(id) => api.goodsReceipts.get(id)}
            fetcher={(id) => api.goodsReceipts.getMovements(id)}
        />
    )
}
