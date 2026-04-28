"use client"

import { useParams } from "next/navigation"
import { DocumentMovementsPage } from "@/components/shared/document-movements-page"
import { api } from "@/lib/api"
import { useMetadataStore } from "@/stores/useMetadataStore"

export default function GoodsReceiptMovementsRoute() {
    const params = useParams<{ id: string }>()
    const label = useMetadataStore((s) => s.getLabel("goods_receipt", "singular"))

    return (
        <DocumentMovementsPage
            documentId={params.id}
            backHref={`/documents/goods-receipts/${params.id}`}
            documentLabel={label}
            numberFetcher={(id) => api.goodsReceipts.get(id)}
            fetcher={(id) => api.goodsReceipts.getMovements(id)}
        />
    )
}
