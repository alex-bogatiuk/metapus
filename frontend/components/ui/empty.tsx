import type { HTMLAttributes } from "react"
import { cn } from "@/lib/utils"

function Empty({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div data-slot="empty" className={cn("flex min-w-0 flex-col items-center justify-center gap-4 rounded-lg px-4 py-10 text-center", className)} {...props} />
}
function EmptyHeader({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div data-slot="empty-header" className={cn("flex max-w-sm flex-col items-center gap-2", className)} {...props} />
}
function EmptyTitle({ className, ...props }: HTMLAttributes<HTMLHeadingElement>) {
  return <h4 data-slot="empty-title" className={cn("font-medium", className)} {...props} />
}
function EmptyDescription({ className, ...props }: HTMLAttributes<HTMLParagraphElement>) {
  return <p data-slot="empty-description" className={cn("text-sm text-muted-foreground", className)} {...props} />
}
export { Empty, EmptyHeader, EmptyTitle, EmptyDescription }
