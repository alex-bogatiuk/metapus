import { AlertCircle, AlertTriangle, Info, XCircle } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import type { EventSeverity } from "@/types/event-log"

const SEVERITY_CONFIG: Record<
  EventSeverity,
  { icon: typeof Info; color: string; bg: string }
> = {
  info: { icon: Info, color: "text-blue-600", bg: "bg-blue-50" },
  warning: { icon: AlertTriangle, color: "text-yellow-600", bg: "bg-yellow-50" },
  error: { icon: AlertCircle, color: "text-red-600", bg: "bg-red-50" },
  critical: { icon: XCircle, color: "text-red-800", bg: "bg-red-100" },
}

export function SeverityBadge({ severity }: { severity: EventSeverity }) {
  const cfg = SEVERITY_CONFIG[severity] ?? SEVERITY_CONFIG.info
  const Icon = cfg.icon
  return (
    <Badge variant="outline" className={`gap-1 ${cfg.color} border-current/20`}>
      <Icon className="h-3 w-3" />
      {severity}
    </Badge>
  )
}
