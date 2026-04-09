"use client"

import { useParams } from "next/navigation"
import { RelatedDocumentsPage } from "@/components/shared/related-documents-page"
import { api } from "@/lib/api"

export default function GoodsReceiptRelatedRoute() {
    const params = useParams<{ id: string }>()

    return (
        <RelatedDocumentsPage
            documentId={params.id}
            backHref={`/documents/goods-receipts/${params.id}`}
            entityTypeLabel="Поступление товаров"
            fetcher={(id) => api.goodsReceipts.getRelatedDocuments(id)}
            documentConfig={{
                apiBasePath: "/document/goods-receipt",
                routePrefix: "goods-receipt",
                entityTypeLabel: "Поступление товаров",
                createBasedOn: [
                    { label: "Создать реализацию на основании", routePrefix: "goods-issue", basisType: "GoodsReceipt" },
                ],
            }}
            relatedConfigs={{
                GoodsIssue: {
                    apiBasePath: "/document/goods-issue",
                    routePrefix: "goods-issue",
                    entityTypeLabel: "Реализация товаров",
                    createBasedOn: [
                        { label: "Создать поступление на основании", routePrefix: "goods-receipt", basisType: "GoodsIssue" },
                    ],
                },
            }}
        />
    )
}
