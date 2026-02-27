import type { LucideIcon } from "lucide-react"
import { cn } from "@/lib/utils"

interface KpiWidgetProps {
  title: string
  value: string
  icon: LucideIcon
  trend?: string
  trendUp?: boolean
  className?: string
}

export function KpiWidget({
  title,
  value,
  icon: Icon,
  trend,
  trendUp,
  className,
}: KpiWidgetProps) {
  return (
    <div
      className={cn(
        "flex items-center gap-4 rounded-lg border bg-card p-4 shadow-sm transition-shadow hover:shadow-md",
        className
      )}
    >
      <div className={cn("flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-muted")}>
        <Icon className={cn("h-5 w-5 text-foreground")} />
      </div>
      <div className="min-w-0 flex-1">
        <p className="truncate text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {title}
        </p>
        <p className="mt-0.5 text-lg font-semibold text-foreground">{value}</p>
        {trend && (
          <p
            className={cn(
              "mt-0.5 text-xs font-medium",
              trendUp ? "text-success" : "text-destructive"
            )}
          >
            {trend}
          </p>
        )}
      </div>
    </div>
  )
}
