import { fmtAmount } from "@/lib/format"
import { cn } from "@/lib/utils"
import { useCompactMode } from "@/hooks/useCompactMode"

interface DocumentTotalsFooterProps {
  totalAmount: number
  totalVat: number
  decimalPlaces: number
  currencySymbol: string
}

export function DocumentTotalsFooter({ totalAmount, totalVat, decimalPlaces, currencySymbol }: DocumentTotalsFooterProps) {
  const compact = useCompactMode()

  return (
    <div className={cn("shrink-0 border-t bg-background px-4 shadow-[0_-4px_8px_-2px_rgba(0,0,0,0.05)] z-20 relative", compact ? "py-1" : "py-2")}>
      <div className="flex items-center gap-6 justify-end text-xs">
        <div className="flex items-center gap-1.5">
          <span className="text-muted-foreground">НДС:</span>
          <span className="font-mono text-[11px] font-medium">{fmtAmount(totalVat, decimalPlaces)} {currencySymbol}</span>
        </div>
        <div className="flex items-center gap-1.5">
          <span className="text-sm font-semibold">ИТОГО:</span>
          <span className={cn("font-bold tracking-tight", compact ? "text-base" : "text-xl")}>{fmtAmount(totalAmount, decimalPlaces)}</span>
          <span className="text-sm font-semibold text-muted-foreground">{currencySymbol}</span>
        </div>
      </div>
    </div>
  )
}
