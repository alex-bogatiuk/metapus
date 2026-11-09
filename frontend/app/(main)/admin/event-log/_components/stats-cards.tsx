import { AlertCircle, AlertTriangle, Info, ShieldAlert } from "lucide-react"
import { Card, CardContent } from "@/components/ui/card"
import type { EventLogStats } from "@/types/event-log"

export function StatsCards({ stats, loading }: { stats: EventLogStats | null; loading: boolean }) {
  if (loading || !stats) {
    return (
      <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
        {Array.from({ length: 5 }).map((_, i) => (
          <Card key={i}>
            <CardContent className="p-4">
              <div className="h-8 bg-muted animate-pulse rounded" />
            </CardContent>
          </Card>
        ))}
      </div>
    )
  }

  const cards = [
    { label: "Всего", value: stats.total, color: "text-foreground", icon: Info },
    { label: "Информация", value: stats.info, color: "text-blue-600", icon: Info },
    { label: "Предупреждения", value: stats.warning, color: "text-yellow-600", icon: AlertTriangle },
    { label: "Ошибки", value: stats.error, color: "text-red-600", icon: AlertCircle },
    { label: "Критические", value: stats.critical, color: "text-red-800", icon: ShieldAlert },
  ]

  return (
    <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
      {cards.map((c) => (
        <Card key={c.label}>
          <CardContent className="p-4 flex items-center gap-3">
            <c.icon className={`h-5 w-5 ${c.color}`} />
            <div>
              <p className="text-xs text-muted-foreground">{c.label}</p>
              <p className={`text-xl font-semibold ${c.color}`}>
                {c.value.toLocaleString("ru-RU")}
              </p>
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}
