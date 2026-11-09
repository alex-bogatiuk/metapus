"use client"
import type { ReactNode } from "react"
import { AlertCircle, RefreshCw } from "lucide-react"
import { CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert"
import { Empty, EmptyHeader, EmptyTitle, EmptyDescription } from "@/components/ui/empty"
import { observedTime } from "@/lib/admin-dashboard"

export function WidgetBody({ title, description, loading, error, retry, children, observedAt, controls }: {
  title: string; description?: string; loading: boolean; error: Error | null; retry: () => void;
  children: ReactNode; observedAt?: string; controls?: ReactNode;
}) {
  return <>
    <CardHeader className="shrink-0 gap-2 space-y-0 p-4 pb-3">
      <div className="flex flex-wrap items-center justify-between gap-2"><CardTitle>{title}</CardTitle>{controls}</div>
      {description && <CardDescription>{description}</CardDescription>}
    </CardHeader>
    <CardContent className="min-h-0 flex-1 overflow-auto p-4 pt-0">
      {error ? <Alert variant="destructive"><AlertCircle /><AlertTitle>Не удалось обновить данные</AlertTitle><AlertDescription><Button variant="outline" size="sm" onClick={retry}>Повторить</Button></AlertDescription></Alert>
        : loading ? <div className="space-y-3" aria-label="Загрузка данных"><Skeleton className="h-8 w-1/2" /><Skeleton className="h-16 w-full" /></div> : children}
    </CardContent>
    {observedAt && !error && <CardFooter className="shrink-0 justify-between gap-2 p-4 pt-0 text-xs text-muted-foreground"><span>Обновлено {observedTime(observedAt)}</span><Button variant="ghost" size="icon" className="h-6 w-6" aria-label={`Обновить: ${title}`} onClick={retry}><RefreshCw /></Button></CardFooter>}
  </>
}
export function WidgetEmpty({ title, description }: { title: string; description?: string }) {
  return <Empty className="py-6"><EmptyHeader><EmptyTitle>{title}</EmptyTitle>{description && <EmptyDescription>{description}</EmptyDescription>}</EmptyHeader></Empty>
}
