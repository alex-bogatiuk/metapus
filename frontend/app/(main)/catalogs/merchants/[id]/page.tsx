"use client"

/**
 * Merchant detail page — wraps the auto-generated form with API Key
 * management section.
 *
 * User access management has been moved to the dedicated admin page:
 *   /admin/merchants/[id]/users
 *
 * Route: /catalogs/merchants/[id]
 */

import { Suspense, use } from "react"
import { useRouter } from "next/navigation"
import AutoForm from "@/components/shared/auto-form"
import { FormSkeleton } from "@/components/shared/form-skeleton"
import { MerchantAPIKeysTab } from "@/components/catalogs/merchant-api-keys-tab"
import { FeeScheduleTable } from "@/components/catalogs/fee-schedule-table"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Key, Users, ArrowRight, ReceiptText } from "lucide-react"

interface PageProps {
  params: Promise<{ id: string }>
}

function MerchantSections({ id }: { id: string }) {
  const router = useRouter()
  const isNew = id === "new"

  return (
    <div className="space-y-6">
      {/* Fee Schedule — full width (most important for crypto processing) */}
      <Card>
        <CardHeader className="flex-row items-center gap-3 space-y-0 py-4">
          <ReceiptText className="h-4 w-4 text-muted-foreground" />
          <CardTitle className="text-sm font-medium">Тарифы комиссий</CardTitle>
        </CardHeader>
        <CardContent className="pt-0">
          <FeeScheduleTable merchantId={id} isNew={isNew} />
        </CardContent>
      </Card>

      <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
        {/* API Keys */}
        <Card>
          <CardHeader className="flex-row items-center gap-3 space-y-0 py-4">
            <Key className="h-4 w-4 text-muted-foreground" />
            <CardTitle className="text-sm font-medium">API-ключи</CardTitle>
          </CardHeader>
          <CardContent className="pt-0">
            <MerchantAPIKeysTab merchantId={id} isNew={isNew} />
          </CardContent>
        </Card>

        {/* User access — link to the dedicated admin page */}
        <Card>
          <CardHeader className="flex-row items-center gap-3 space-y-0 py-4">
            <Users className="h-4 w-4 text-muted-foreground" />
            <CardTitle className="text-sm font-medium">Пользователи</CardTitle>
          </CardHeader>
          <CardContent className="pt-0">
            {isNew ? (
              <div className="flex flex-col items-center justify-center py-10 text-center text-muted-foreground gap-3">
                <Users className="h-7 w-7 opacity-40" />
                <p className="text-sm">Сохраните мерчанта, чтобы управлять пользователями</p>
              </div>
            ) : (
              <div className="flex flex-col items-center justify-center py-10 text-center gap-4">
                <Users className="h-7 w-7 text-muted-foreground opacity-40" />
                <div className="space-y-1">
                  <p className="text-sm font-medium">Управление доступом пользователей</p>
                  <p className="text-xs text-muted-foreground">
                    Назначайте пользователей и роли в разделе администрирования
                  </p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => router.push(`/admin/merchants/${id}/users`)}
                >
                  Управление пользователями
                  <ArrowRight className="ml-1.5 h-3.5 w-3.5" />
                </Button>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

export default function MerchantDetailPage({ params }: PageProps) {
  const { id } = use(params)

  return (
    <Suspense fallback={<FormSkeleton variant="catalog" />}>
      <AutoForm
        entityName="Merchant"
        entityType="catalog"
        routePrefix="merchants"
        id={id === "new" ? undefined : id}
        appendContent={<MerchantSections id={id} />}
      />
    </Suspense>
  )
}
