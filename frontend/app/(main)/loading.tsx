import { Skeleton } from "@/components/ui/skeleton"

export default function GlobalLoading() {
    return (
        <div className="flex h-full flex-col">
            {/* Toolbar skeleton */}
            <div className="flex items-center justify-between border-b bg-card px-4 py-2">
                <div className="flex items-center gap-2">
                    <Skeleton className="h-8 w-20" />
                    <Skeleton className="h-8 w-24" />
                </div>
                <Skeleton className="h-8 w-60" />
            </div>

            {/* Table skeleton */}
            <div className="flex-1 p-0">
                <div className="flex flex-col gap-0">
                    {/* Header row */}
                    <div className="flex items-center gap-4 border-b bg-muted/70 px-4 py-3">
                        <Skeleton className="h-4 w-4" />
                        <Skeleton className="h-3 w-32" />
                        <Skeleton className="h-3 w-20" />
                        <Skeleton className="h-3 w-16" />
                        <Skeleton className="h-3 w-24" />
                    </div>
                    {/* Data rows */}
                    {Array.from({ length: 8 }).map((_, i) => (
                        <div
                            key={i}
                            className="flex items-center gap-4 border-b px-4 py-3"
                        >
                            <Skeleton className="h-4 w-4" />
                            <Skeleton className="h-4 w-40" />
                            <Skeleton className="h-4 w-16" />
                            <Skeleton className="h-4 w-12" />
                            <Skeleton className="h-4 w-20" />
                        </div>
                    ))}
                </div>
            </div>
        </div>
    )
}
