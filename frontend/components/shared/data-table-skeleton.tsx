// components/shared/data-table-skeleton.tsx
"use client"

interface DataTableSkeletonProps {
    rows?: number
    columns?: number
    showCheckbox?: boolean
    showPrefix?: boolean
    showToolbar?: boolean
}

/**
 * Temporary diagnostic fallback: deliberately renders no loading affordance.
 *
 * Every list loading path is still routed through this component, so the
 * screen remains stable while we isolate whether the table skeleton caused
 * the visual flicker.
 */
export function DataTableSkeleton(props: DataTableSkeletonProps) {
    void props

    return <div className="h-full bg-workspace-surface" aria-busy="true" />
}
