// components/shared/form-skeleton.tsx
"use client"

import { Skeleton } from "@/components/ui/skeleton"

/**
 * Default number of field placeholders per variant.
 */
const DEFAULT_FIELD_COUNT = {
    catalog: 6,
    document: 8,
} as const

/**
 * Default number of table part rows in document skeleton.
 */
const DEFAULT_TABLE_ROWS = 3

interface FormSkeletonProps {
    /** Form variant — determines layout (tabs, table part, footer). */
    variant?: "catalog" | "document"
    /** Number of field placeholders. */
    fields?: number
    /** Show tab bar skeleton. Default: true for documents. */
    showTabs?: boolean
    /** Show table part skeleton. Default: true for documents. */
    showTablePart?: boolean
    /** Number of table rows in table part. */
    tableRows?: number
}

/**
 * Ghost toolbar matching FormToolbar layout: back button + title + action buttons.
 */
function FormToolbarSkeleton() {
    return (
        <div className="flex items-center gap-2 border-b bg-card px-4 py-2">
            {/* Back button */}
            <Skeleton className="h-7 w-7 rounded-md" />

            {/* Title + status badge */}
            <div className="mr-4 flex items-center gap-2">
                <Skeleton className="h-5 w-40" />
                <Skeleton className="h-5 w-20 rounded-full" />
            </div>

            {/* Action buttons */}
            <Skeleton className="h-8 w-36" />
            <Skeleton className="h-8 w-24" />
            <Skeleton className="h-8 w-20 hidden md:block" />

            {/* Spacer + "More" button */}
            <div className="flex-1" />
            <Skeleton className="h-8 w-16" />
            <Skeleton className="h-7 w-7 rounded-md" />
        </div>
    )
}

/**
 * Ghost field: label + input.
 * Widths alternate for visual variety.
 */
const LABEL_WIDTHS = ["w-24", "w-20", "w-28", "w-32", "w-16", "w-36", "w-24", "w-20"]

function FieldSkeleton({ index }: { index: number }) {
    const labelW = LABEL_WIDTHS[index % LABEL_WIDTHS.length]
    return (
        <div>
            <Skeleton className={`h-3 ${labelW} mb-2`} />
            <Skeleton className="h-9 w-full rounded-md" />
        </div>
    )
}

/**
 * Ghost table part: header + rows (matches DocumentLineRow layout).
 */
function TablePartSkeleton({ rows = DEFAULT_TABLE_ROWS }: { rows?: number }) {
    const colWidths = ["w-10", "min-w-[160px]", "w-[140px]", "w-24", "w-24", "w-24", "w-10"]

    return (
        <div className="flex-1 min-h-0 flex flex-col">
            {/* Inline action bar */}
            <div className="flex items-center gap-1 p-2 bg-card/50 border-b shrink-0">
                <Skeleton className="h-8 w-24" />
                <Skeleton className="h-8 w-20" />
            </div>

            {/* Table header */}
            <div className="flex items-center gap-2 border-b bg-muted/50 px-2 py-2">
                {colWidths.map((w, i) => (
                    <Skeleton key={`th-${i}`} className={`h-3 ${w}`} />
                ))}
            </div>

            {/* Table rows */}
            {Array.from({ length: rows }).map((_, rowIdx) => (
                <div key={rowIdx} className="flex items-center gap-2 border-b px-2 py-3">
                    <Skeleton className="h-4 w-10" />
                    <Skeleton className="h-8 flex-[2]" />
                    <Skeleton className="h-8 w-[140px]" />
                    <Skeleton className="h-4 w-24" />
                    <Skeleton className="h-4 w-24" />
                    <Skeleton className="h-4 w-24" />
                    <Skeleton className="h-4 w-10" />
                </div>
            ))}
        </div>
    )
}

/**
 * FormSkeleton — metadata-agnostic ghost loading for entity forms.
 *
 * Usage:
 *   <FormSkeleton variant="catalog" />           — catalog form
 *   <FormSkeleton variant="document" />           — document form with tabs + table
 *   <FormSkeleton variant="document" fields={10} /> — custom field count
 */
export function FormSkeleton({
    variant = "catalog",
    fields,
    showTabs,
    showTablePart,
    tableRows = DEFAULT_TABLE_ROWS,
}: FormSkeletonProps) {
    const fieldCount = fields ?? DEFAULT_FIELD_COUNT[variant]
    const effectiveShowTabs = showTabs ?? variant === "document"
    const effectiveShowTablePart = showTablePart ?? variant === "document"

    return (
        <div className="flex h-full flex-col">
            <FormToolbarSkeleton />

            {/* Header fields */}
            <div className="border-b bg-card shrink-0 p-4">
                <div className="grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-2 lg:grid-cols-3 max-w-3xl">
                    {Array.from({ length: fieldCount }).map((_, i) => (
                        <FieldSkeleton key={i} index={i} />
                    ))}
                </div>
            </div>

            {/* Tabs + Table Part (documents) */}
            {effectiveShowTabs && (
                <div className="flex items-center border-b bg-card px-4">
                    <div className="flex items-center gap-4 py-3">
                        <Skeleton className="h-4 w-20" />
                        <Skeleton className="h-4 w-28" />
                    </div>
                </div>
            )}

            {effectiveShowTablePart && <TablePartSkeleton rows={tableRows} />}

            {/* Footer (document totals) */}
            {variant === "document" && (
                <div className="flex items-center justify-end gap-6 border-t bg-card px-4 py-2.5">
                    <div className="flex items-center gap-2">
                        <Skeleton className="h-3 w-12" />
                        <Skeleton className="h-4 w-20" />
                    </div>
                    <div className="flex items-center gap-2">
                        <Skeleton className="h-3 w-8" />
                        <Skeleton className="h-4 w-16" />
                    </div>
                </div>
            )}
        </div>
    )
}
