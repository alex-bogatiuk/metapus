import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"

export function OverviewLoading({ label }: { label: string }) {
  return <div role="status" aria-label={label} className="flex flex-col gap-3 py-2">
    <Skeleton className="h-8 w-1/3" /><Skeleton className="h-20 w-full" /><Skeleton className="h-8 w-2/3" />
  </div>
}
export function OverviewError({ title, retry }: { title: string; retry: () => void }) {
  return <Alert>
    <AlertTitle>{title}</AlertTitle>
    <AlertDescription className="flex flex-wrap items-center justify-between gap-3">
      <span>Повторите загрузку. Если ошибка сохранится, попробуйте позже.</span>
      <Button variant="outline" size="sm" onClick={() => { void retry() }}>Повторить загрузку</Button>
    </AlertDescription>
  </Alert>
}
export function OverviewEmpty({ title, description }: { title: string; description: string }) {
  return <Empty><EmptyHeader><EmptyTitle>{title}</EmptyTitle><EmptyDescription>{description}</EmptyDescription></EmptyHeader></Empty>
}
