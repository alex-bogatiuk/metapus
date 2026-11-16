"use client"

import { useState } from "react"
import { usePortalStore } from "@/configuration/crypto/stores/usePortalStore"
import { useValuedBalance } from "@/hooks/use-valued-balance"
import { BalanceOverview, BalanceAssets, BalanceCalculationSheet, type BalanceCalculation } from "@/configuration/crypto/components/portal/balance-overview"
import { VolumeChartCard } from "@/configuration/crypto/components/portal/volume-chart-card"
import { RecentOperationsCard } from "@/configuration/crypto/components/portal/recent-operations-card"
import { RiskBalance } from "@/configuration/crypto/components/portal/risk-balance"
import { ConversionFunnelCard } from "@/configuration/crypto/components/portal/conversion-funnel-card"
import { OverviewEmpty, OverviewError, OverviewLoading } from "@/configuration/crypto/components/portal/overview-states"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"

export default function PortalDashboardPage() {
  const merchantId = usePortalStore(s => s.activeMerchantId)
  // Remount the calculation panel and local filters when the selected merchant changes.
  return <MerchantOverview key={merchantId ?? "none"} merchantId={merchantId} />
}

function MerchantOverview({ merchantId }: { merchantId: string | null }) {
  const merchants = usePortalStore(s => s.merchants)
  const role = merchants.find(merchant => merchant.id === merchantId)?.role ?? 0
  const canManage = role === 1 || role === 2
  const { data, error, isLoading, isValidating, expired, mutate } = useValuedBalance(merchantId, true)
  const [details, setDetails] = useState<BalanceCalculation | null>(null)

  return <div className="flex min-w-0 flex-col gap-6">
    <div><h1 className="text-2xl font-bold tracking-tight">Обзор</h1><p className="mt-1 text-sm text-muted-foreground">Баланс, поступления и последние операции</p></div>
    {!merchantId ? <Card><CardContent className="pt-6"><OverviewEmpty title="Выберите мерчанта" description="Его баланс и операции появятся в обзоре." /></CardContent></Card> : <>
      {data ? <BalanceOverview data={data} expired={expired} refreshFailed={!!error} refreshing={isValidating} refresh={() => mutate().catch(() => undefined)} canCreateInvoice={canManage} canWithdraw={canManage} showDetails={setDetails} /> : <Card>
        <CardHeader><CardTitle>Общий баланс</CardTitle></CardHeader><CardContent>
          {isLoading ? <OverviewLoading label="Загрузка баланса" /> : <OverviewError title="Не удалось загрузить баланс" retry={() => mutate().catch(() => undefined)} />}
        </CardContent>
      </Card>}
      <Tabs defaultValue="funds" className="min-w-0">
        <TabsList variant="line" aria-label="Раздел обзора">
          <TabsTrigger variant="line" value="funds">Средства и операции</TabsTrigger>
          <TabsTrigger variant="line" value="analytics">Аналитика</TabsTrigger>
        </TabsList>
        <TabsContent value="funds" className="mt-5 flex flex-col gap-5">
          <RiskBalance merchantId={merchantId} />
          {data && <BalanceAssets data={data} showDetails={setDetails} />}
          <RecentOperationsCard merchantId={merchantId} />
        </TabsContent>
        <TabsContent value="analytics" className="mt-5 flex flex-col gap-5">
          <VolumeChartCard merchantId={merchantId} balances={data?.byToken ?? []} />
          <ConversionFunnelCard merchantId={merchantId} />
        </TabsContent>
      </Tabs>
      <BalanceCalculationSheet details={details} close={() => setDetails(null)} />
    </>}
  </div>
}
