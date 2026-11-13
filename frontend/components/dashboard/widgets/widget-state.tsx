"use client"
import type { ReactNode } from "react"
import Link from "next/link"
import type { LucideIcon } from "lucide-react"
import { AlertCircle } from "lucide-react"
import { CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert"
import { Empty, EmptyHeader, EmptyTitle, EmptyDescription } from "@/components/ui/empty"

export function WidgetBody({ title, description, loading, error, retry, children, controls }: {
  title: string; description?: string; loading: boolean; error: Error | null; retry: () => void;
  children: ReactNode; controls?: ReactNode;
}) {
  return <>
    <CardHeader className="shrink-0 gap-2 space-y-0 p-4 pb-3">
      <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1.5"><CardTitle className="text-base">{title}</CardTitle>{controls}</div>
      {description && <CardDescription className="text-xs">{description}</CardDescription>}
    </CardHeader>
    <CardContent className="flex min-h-0 flex-1 flex-col overflow-auto p-4 pt-0">
      {error ? <Alert variant="destructive"><AlertCircle /><AlertTitle>Не удалось обновить данные</AlertTitle><AlertDescription><Button variant="outline" size="sm" onClick={retry}>Повторить</Button></AlertDescription></Alert>
        : loading ? <div className="space-y-3" aria-label="Загрузка данных"><Skeleton className="h-8 w-1/2" /><Skeleton className="h-16 w-full" /></div> : children}
    </CardContent>
  </>
}
/** An empty widget says what is missing and, when a person can fix it, where. */
export function WidgetEmpty({ title, description, icon: Icon, action }: { title: string; description?: string; icon?: LucideIcon; action?: { label: string; href: string } }) {
  return <Empty className="flex-1 gap-3 py-6"><EmptyHeader>
    {Icon && <span aria-hidden className="mb-1 flex size-9 items-center justify-center rounded-lg bg-muted text-muted-foreground [&_svg]:size-4"><Icon /></span>}
    <EmptyTitle className="text-sm">{title}</EmptyTitle>{description && <EmptyDescription className="text-xs">{description}</EmptyDescription>}
  </EmptyHeader>{action && <Button asChild variant="outline" size="sm"><Link href={action.href}>{action.label}</Link></Button>}</Empty>
}
