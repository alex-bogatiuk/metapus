"use client"

import { usePortalStore } from "@/stores/usePortalStore"
import { BalanceSummaryCard } from "@/components/portal/balance-summary-card"
import { CurrencyBreakdownCard } from "@/components/portal/currency-breakdown-card"
import { MerchantBalanceCard } from "@/components/portal/merchant-balance-card"
import { VolumeChartCard } from "@/components/portal/volume-chart-card"
import { RecentInvoicesCard } from "@/components/portal/recent-invoices-card"
import { ConversionFunnelCard } from "@/components/portal/conversion-funnel-card"

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
        {/* Row 1: Summary + Fiat Balance + Currencies */}
        <div className="lg:col-span-2">
          <BalanceSummaryCard merchantId={activeMerchantId} />
        </div>
        <div className="space-y-4">
          <MerchantBalanceCard merchantId={activeMerchantId} />
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {/* Row 2: Currencies + Conversion Funnel */}
        <div>
          <CurrencyBreakdownCard merchantId={activeMerchantId} />
        </div>
        <div className="lg:col-span-2">
          <ConversionFunnelCard merchantId={activeMerchantId ?? undefined} />
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {/* Row 3: Chart + Recent Invoices */}
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

