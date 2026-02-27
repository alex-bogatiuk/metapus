"use client"

import {
  Wallet,
  Package,
  TrendingUp,
  TrendingDown,
  Landmark,
  Target,
  ArrowDownToLine,
  CreditCard,
  ShoppingBag,
} from "lucide-react"
import { KpiWidget } from "@/components/dashboard/kpi-widget"
import { RecentActivity } from "@/components/dashboard/recent-activity"
import { QuickActions } from "@/components/dashboard/quick-actions"
import { CurrentTasks } from "@/components/dashboard/current-tasks"
import { Button } from "@/components/ui/button"

export default function DashboardPage() {
  return (
    <div className="flex h-full">
      <div className="flex-1 overflow-auto p-6">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold text-foreground">
              Начальная страница
            </h1>
            <p className="mt-0.5 text-sm text-muted-foreground">
              12.02.2026 - Metapus ERP
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm">
              Обновить
            </Button>
            <Button variant="outline" size="sm">
              Настроить
            </Button>
          </div>
        </div>

        <div className="mb-2">
          <p className="text-center text-xs font-medium uppercase tracking-wider text-muted-foreground">
            На сегодня
          </p>
        </div>

        <div className="mb-6 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <KpiWidget
            title="Остаток денег"
            value="-- ввести остатки"
            icon={Wallet}
          />
          <KpiWidget
            title="Товары"
            value="11,00 Р"
            icon={Package}
            trend="+11,00 за неделю"
            trendUp
          />
          <KpiWidget
            title="Долги нам"
            value="--"
            icon={TrendingUp}
          />
          <KpiWidget
            title="Долги наши"
            value="11,00 Р"
            icon={TrendingDown}
          />
          <KpiWidget
            title="Чистые активы"
            value="--"
            icon={Landmark}
          />
          <KpiWidget
            title="Лиды"
            value="0"
            icon={Target}
          />
        </div>

        <div className="mb-2">
          <p className="text-center text-xs font-medium uppercase tracking-wider text-muted-foreground">
            С начала этого года
          </p>
        </div>

        <div className="mb-6 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <KpiWidget
            title="Поступления"
            value="--"
            icon={ArrowDownToLine}
          />
          <KpiWidget
            title="Платежи"
            value="--"
            icon={CreditCard}
          />
          <KpiWidget
            title="Продажи"
            value="--"
            icon={ShoppingBag}
          />
          <KpiWidget
            title="Конверсия заказов"
            value="0"
            icon={Target}
          />
        </div>

        <RecentActivity />
      </div>

      <div className="hidden w-72 shrink-0 border-l bg-card p-4 xl:block">
        <QuickActions />
        <div className="mt-4">
          <CurrentTasks />
        </div>
      </div>
    </div>
  )
}
