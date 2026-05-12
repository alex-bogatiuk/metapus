"use client"

/**
 * Admin page: merchant user access management.
 * Route: /admin/merchants/[id]/users
 *
 * Operator assigns/removes platform users and their roles for a specific merchant.
 * All changes go through /api/v1/merchant-admin/merchants/:id/users (JWT + RBAC).
 * Merchant users never reach this page — it lives inside the operator ERP UI.
 */

import { Suspense, use, useEffect, useState, useCallback } from "react"
import { useRouter } from "next/navigation"
import { apiFetch } from "@/lib/api"
import { useTabTitle } from "@/hooks/useTabTitle"
import { MerchantUsersTab } from "@/components/catalogs/merchant-users-tab"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { FormSkeleton } from "@/components/shared/form-skeleton"
import { ScrollArea } from "@/components/ui/scroll-area"
import { toast } from "sonner"

interface PageProps {
  params: Promise<{ id: string }>
}

interface MerchantRecord {
  id: string
  name: string
  code: string
}

function MerchantUsersPage({ merchantId }: { merchantId: string }) {
  const router = useRouter()
  const [merchant, setMerchant] = useState<MerchantRecord | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const m = await apiFetch<MerchantRecord>(`/catalog/merchants/${merchantId}`)
      setMerchant(m)
    } catch {
      toast.error("Не удалось загрузить данные мерчанта")
    } finally {
      setLoading(false)
    }
  }, [merchantId])

  useEffect(() => {
    load()
  }, [load])

  // Update MDI tab title: «Test Merchant · Доступ»
  useTabTitle(merchant?.name, "Доступ")

  if (loading) {
    return <FormSkeleton variant="catalog" />
  }

  const merchantName = merchant?.name ?? merchantId
  const merchantCode = merchant?.code

  return (
    <div className="flex h-full flex-col animate-skeleton-fade-in">
      <FormToolbar
        title={`${merchantName}${merchantCode ? ` (${merchantCode})` : ""}`}
        backHref="/admin/merchants"
        onClose={() => router.push("/admin/merchants")}
        primaryAction={{
          label: "Готово",
          onClick: () => router.push("/admin/merchants"),
        }}
      />

      <ScrollArea className="flex-1">
        <div className="mx-auto max-w-4xl px-6 py-6">
          {/* Breadcrumb hint */}
          <div className="mb-4 flex items-center gap-1.5 text-xs text-muted-foreground">
            <span>Администрирование</span>
            <span>›</span>
            <button
              className="hover:text-foreground transition-colors"
              onClick={() => router.push("/admin/merchants")}
            >
              Мерчанты
            </button>
            <span>›</span>
            <span className="text-foreground font-medium">{merchantName}</span>
            <span>›</span>
            <span className="text-foreground font-medium">Пользователи</span>
          </div>

          <MerchantUsersTab merchantId={merchantId} isNew={false} />
        </div>
      </ScrollArea>
    </div>
  )
}

export default function AdminMerchantUsersPage({ params }: PageProps) {
  const { id } = use(params)
  return (
    <Suspense fallback={<FormSkeleton variant="catalog" />}>
      <MerchantUsersPage merchantId={id} />
    </Suspense>
  )
}
