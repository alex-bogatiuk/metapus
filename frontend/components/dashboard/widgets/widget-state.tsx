"use client"
import type { ReactNode } from "react"
import Link from "next/link"
import type { LucideIcon } from "lucide-react"
import { AlertCircle, ArrowRight } from "lucide-react"
import { cn } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { CardTitle, CardDescription } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert"
import { Empty, EmptyHeader, EmptyTitle, EmptyDescription } from "@/components/ui/empty"

/**
 * Card anatomy shared by every widget: a header (title, description, controls or an action on the right),
 * a body that may run edge-to-edge for tables and lists, and an optional footer pinned to the bottom.
 */
export function WidgetBody({ title, description, loading, error, retry, children, controls, action, flush = false, footer }: {
  title: string; description?: string; loading: boolean; error: Error | null; retry: () => void;
  children: ReactNode; controls?: ReactNode;
  /** A link or button for the whole widget, e.g. "Все заявки". */
  action?: ReactNode
  /** Body without side padding, so tables and row lists reach the card edges. */
  flush?: boolean
  footer?: ReactNode
}) {
  const status = error ? <Alert variant="destructive"><AlertCircle /><AlertTitle>Не удалось обновить данные</AlertTitle><AlertDescription><Button variant="outline" size="sm" onClick={retry}>Повторить</Button></AlertDescription></Alert>
    : loading ? <div className="space-y-3" aria-label="Загрузка данных"><Skeleton className="h-8 w-1/2" /><Skeleton className="h-16 w-full" /></div> : null
  return <>
    <div className="flex shrink-0 flex-wrap items-start justify-between gap-x-3 gap-y-2 px-4 pb-3 pt-3.5">
      <div className="min-w-0">
        <CardTitle className="text-[13.5px] leading-snug">{title}</CardTitle>
        {description && <CardDescription className="mt-0.5 text-xs">{description}</CardDescription>}
      </div>
      {(controls || action) && <div className="flex flex-wrap items-center justify-end gap-1.5">{controls}{action}</div>}
    </div>
    <div className={cn("flex min-h-0 flex-1 flex-col overflow-auto", (!flush || status) && "px-4 pb-4")}>
      {status ?? children}
    </div>
    {footer && !status && <div className="flex shrink-0 flex-wrap items-center justify-between gap-x-3 gap-y-1 border-t px-4 py-2.5 text-xs text-muted-foreground">{footer}</div>}
  </>
}

/** Header action that opens the full list behind a widget. */
export function WidgetLink({ href, children }: { href: string; children: ReactNode }) {
  return <Button asChild variant="ghost" size="sm" className="-mr-1.5 text-muted-foreground hover:text-foreground"><Link href={href}>{children}<ArrowRight aria-hidden /></Link></Button>
}

/** Text link for a widget footer: keeps every footer one line high. */
export function FooterLink({ href, children }: { href: string; children: ReactNode }) {
  return <Link href={href} className="inline-flex items-center gap-1 rounded-sm font-medium text-foreground/75 underline-offset-4 hover:text-foreground hover:underline focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/25">{children}<ArrowRight aria-hidden className="size-3.5" /></Link>
}

/** Small status pill with a dot: the same look for every state shown inside widgets. */
export function StatusBadge({ tone, children }: { tone: "success" | "warning" | "destructive" | "info" | "neutral"; children: ReactNode }) {
  return <Badge variant={tone === "neutral" ? "secondary" : tone} className="gap-1.5">{tone !== "neutral" && <span aria-hidden className="size-1.5 rounded-full bg-current" />}{children}</Badge>
}

/** An empty widget says what is missing and, when a person can fix it, where. */
export function WidgetEmpty({ title, description, icon: Icon, action, inline = false, tone = "neutral" }: {
  title: string; description?: string; icon?: LucideIcon; action?: { label: string; href: string }
  /** One line for a widget that asked the grid for fewer rows. */
  inline?: boolean
  /** "success" when emptiness is good news (nothing waits for a decision). */
  tone?: "neutral" | "success"
}) {
  if (inline) return <div className="flex min-h-7 flex-wrap items-center gap-x-2.5 gap-y-1">
    {Icon && <span aria-hidden className={cn("flex size-7 shrink-0 items-center justify-center rounded-md [&_svg]:size-4", tone === "success" ? "bg-success/10 text-success" : "bg-muted text-muted-foreground")}><Icon /></span>}
    <span className="text-sm font-medium">{title}</span>
    {description && <span className="text-xs text-muted-foreground">{description}</span>}
    {action && <span className="ml-auto text-xs"><FooterLink href={action.href}>{action.label}</FooterLink></span>}
  </div>
  return <Empty className="flex-1 gap-3 px-4 py-6"><EmptyHeader>
    {Icon && <span aria-hidden className="mb-1 flex size-9 items-center justify-center rounded-lg bg-muted text-muted-foreground [&_svg]:size-4"><Icon /></span>}
    <EmptyTitle className="text-sm">{title}</EmptyTitle>{description && <EmptyDescription className="text-xs">{description}</EmptyDescription>}
  </EmptyHeader>{action && <Button asChild variant="outline" size="sm"><Link href={action.href}>{action.label}</Link></Button>}</Empty>
}
