"use client"

import { usePortalStore } from "@/stores/usePortalStore"
import { BalanceSummaryCard } from "@/components/portal/balance-summary-card"
import { CurrencyBreakdownCard } from "@/components/portal/currency-breakdown-card"
import { VolumeChartCard } from "@/components/portal/volume-chart-card"
import { RecentInvoicesCard } from "@/components/portal/recent-invoices-card"

export default function PortalDashboardPage() {
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Дашборд</h1>
        <p className="text-sm text-muted-foreground">
          Обзор платежей и транзакций
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {/* Row 1: Summary + Currencies */}
        <div className="lg:col-span-2">
          <BalanceSummaryCard merchantId={activeMerchantId} />
        </div>
        <div>
          <CurrencyBreakdownCard merchantId={activeMerchantId} />
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {/* Row 2: Chart + Recent Invoices */}
        <div className="lg:col-span-2">
          <VolumeChartCard merchantId={activeMerchantId} />
        </div>
        <div>
          <RecentInvoicesCard merchantId={activeMerchantId} />
        </div>
      </div>
    </div>
  )
}
