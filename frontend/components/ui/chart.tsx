"use client"

import type { ComponentProps, CSSProperties } from "react"
import { ResponsiveContainer } from "recharts"
import { cn } from "@/lib/utils"

// Compatible with the project's Recharts 2 API; colors follow the shadcn tokens.
export type ChartConfig = Record<string, { label: string; color: string }>
export function ChartContainer({
  config, className, children, style, ...props
}: ComponentProps<"div"> & { config: ChartConfig; children: ComponentProps<typeof ResponsiveContainer>["children"] }) {
  const colors = Object.fromEntries(Object.entries(config).map(([key, value]) => ["--color-" + key, value.color]))
  return (
    <div data-slot="chart" className={cn("min-w-0 text-xs [&_.recharts-cartesian-axis-tick_text]:fill-muted-foreground [&_.recharts-cartesian-grid_line]:stroke-border [&_.recharts-rectangle.recharts-tooltip-cursor]:fill-muted", className)} style={{ ...colors, ...style } as CSSProperties} {...props}>
      <ResponsiveContainer width="100%" height="100%">{children}</ResponsiveContainer>
    </div>
  )
}
