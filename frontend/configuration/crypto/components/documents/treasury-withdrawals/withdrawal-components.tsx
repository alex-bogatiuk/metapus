"use client"

import {
  AlertTriangle,
  CheckCircle2,
  Copy,
  Loader2,
  Wallet,
  XCircle,
} from "lucide-react"

import { truncateAddress } from "@/configuration/crypto/lib/blockchain"
import { cn } from "@/lib/utils"
import type { TreasuryWithdrawalStatus } from "@/configuration/crypto/types/treasury"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"

export const STATUS: Record<TreasuryWithdrawalStatus, {
  label: string
  variant: "default" | "secondary" | "destructive" | "outline"
  icon: typeof CheckCircle2
}> = {
  pending_signature: { label: "Ожидает подписи", variant: "outline", icon: Loader2 },
  pending_approval: { label: "На одобрении", variant: "outline", icon: Wallet },
  approved: { label: "Одобрена", variant: "secondary", icon: CheckCircle2 },
  signing: { label: "Подписание", variant: "secondary", icon: Loader2 },
  broadcast: { label: "Отправлена", variant: "secondary", icon: CheckCircle2 },
  confirmed: { label: "Подтверждена", variant: "default", icon: CheckCircle2 },
  held: { label: "На сверке", variant: "outline", icon: AlertTriangle },
  rejected: { label: "Отклонена", variant: "destructive", icon: XCircle },
  failed: { label: "Ошибка", variant: "destructive", icon: XCircle },
}

export function Metric({
  label,
  value,
  detail,
  loading,
}: {
  label: string
  value: string
  detail: string
  loading?: boolean
}) {
  return (
    <div className="rounded-md border bg-background px-3 py-2">
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      {loading ? (
        <div className="mt-2 flex flex-col gap-2">
          <Skeleton className="h-5 w-36" />
          <Skeleton className="h-3 w-48" />
        </div>
      ) : (
        <div className="mt-1 flex flex-col gap-0.5">
          <div className="text-base font-semibold tabular-nums">{value}</div>
          <div className="truncate text-xs text-muted-foreground">{detail}</div>
        </div>
      )}
    </div>
  )
}

export function FormField({
  label,
  className,
  children,
}: {
  label: string
  className?: string
  children: React.ReactNode
}) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-1", className)}>
      <Label className="text-xs text-muted-foreground">{label}</Label>
      {children}
    </div>
  )
}

export function DetailValue({
  label,
  value,
  mono,
  wide,
}: {
  label: string
  value: string
  mono?: boolean
  wide?: boolean
}) {
  return (
    <div className={cn("min-w-0", wide && "col-span-2")}>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={cn("mt-1 truncate text-sm", mono && "font-mono text-xs")}>{value || "—"}</div>
    </div>
  )
}

export function StatusBadge({ status }: { status: TreasuryWithdrawalStatus }) {
  const statusConfig = STATUS[status]
  const Icon = statusConfig.icon
  return (
    <Badge variant={statusConfig.variant} className="gap-1">
      <Icon className={status === "signing" || status === "pending_signature" ? "animate-spin" : undefined} />
      {statusConfig.label}
    </Badge>
  )
}

export function AddressButton({
  address,
  onCopy,
}: {
  address: string
  onCopy: (value: string) => void
}) {
  if (!address) return <span className="text-xs text-muted-foreground">—</span>
  return (
    <TooltipProvider delayDuration={300}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 max-w-[190px] px-1 font-mono text-xs"
            onClick={() => onCopy(address)}
          >
            <span className="truncate">{truncateAddress(address)}</span>
            <Copy className="opacity-50" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          <p className="max-w-[520px] break-all font-mono text-xs">{address}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
