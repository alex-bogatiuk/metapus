"use client"

import React from "react"
import { DataTableSkeleton } from "@/components/shared/data-table-skeleton"
import { Button } from "@/components/ui/button"

interface ListContentProps {
  /** Legacy alias. Prefer initialLoading for new callers. */
  loading?: boolean
  /** Cold-load state: no cached content exists, so skeleton is allowed. */
  initialLoading?: boolean
  /** Background reload while cached content remains visible. */
  refreshing?: boolean
  error: string | null
  isEmpty: boolean
  onRetry?: () => void
  emptyMessage?: string
  children: React.ReactNode
}

export function ListContent({
  loading,
  initialLoading,
  refreshing = false,
  error,
  isEmpty,
  onRetry,
  emptyMessage = "Список пуст.",
  children,
}: ListContentProps) {
  const shouldShowSkeleton = initialLoading ?? loading ?? false

  if (shouldShowSkeleton) {
    return <DataTableSkeleton showToolbar={false} />
  }

  if (error && isEmpty) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 py-20 text-destructive">
        <p>{error}</p>
        {onRetry && (
          <Button variant="outline" size="sm" onClick={onRetry}>
            Повторить
          </Button>
        )}
      </div>
    )
  }

  if (isEmpty) {
    return (
      <div className="flex items-center justify-center py-20 text-muted-foreground">
        {emptyMessage}
      </div>
    )
  }

  return (
    <div aria-busy={refreshing || undefined}>
      {children}
    </div>
  )
}
