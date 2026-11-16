"use client"

import useSWR from "swr"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"

import { OverviewEmpty, OverviewError, OverviewLoading } from "./overview-states"

import { cryptoApi } from "@/configuration/crypto/lib/api"
export function ConversionFunnelCard({ merchantId }: { merchantId?: string }) {
  const { data, error, isLoading, mutate } = useSWR(
    merchantId ? ["portal-funnel", merchantId] : null,
    () => cryptoApi.portal.funnel(merchantId),
    { keepPreviousData: false, refreshInterval: 60000 },
  )
  const steps = data ? [
    { label: "Создано", value: data.total },
    { label: "Получен платёж", value: data.receivedAny },
    { label: "Полностью оплачено", value: data.fullyPaid },
    { label: "Подтверждено", value: data.confirmed },
  ] : []
  const percent = (value: number) => data?.total ? value / data.total * 100 : 0
  return <Card>
    <CardHeader><CardTitle>Оплата инвойсов</CardTitle><CardDescription>Путь от создания до подтверждения · за всё время</CardDescription></CardHeader>
    <CardContent className="flex flex-col gap-5">
      {isLoading ? <OverviewLoading label="Загрузка аналитики инвойсов" /> : error ? <OverviewError title="Не удалось загрузить аналитику инвойсов" retry={() => mutate().catch(() => undefined)} /> : !data?.total ? <OverviewEmpty title="Инвойсов пока нет" description="После создания инвойсов здесь появится статистика их оплаты." /> : <>
        <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-4">
          {steps.map(step => <div key={step.label} className="flex flex-col gap-2 rounded-lg bg-muted/50 p-4">
            <p className="text-sm text-muted-foreground">{step.label}</p>
            <p className="text-2xl font-semibold tabular-nums">{step.value.toLocaleString("ru-RU")}</p>
            <Progress value={percent(step.value)} aria-label={step.label} />
            <p className="text-xs text-muted-foreground">{percent(step.value).toLocaleString("ru-RU", { maximumFractionDigits: 1 })}% от созданных</p>
          </div>)}
        </div>
        {data.expired > 0 && <p className="text-sm text-muted-foreground">Срок оплаты истёк: {data.expired.toLocaleString("ru-RU")} ({percent(data.expired).toLocaleString("ru-RU", { maximumFractionDigits: 1 })}%). Включая инвойсы с недоплатой.</p>}
      </>}
    </CardContent>
  </Card>
}
