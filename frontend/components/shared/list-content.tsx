"use client"

import React from "react"
import { DataTableSkeleton } from "@/components/shared/data-table-skeleton"
import { Button } from "@/components/ui/button"

interface ListContentProps {
  loading: boolean
  error: string | null
  isEmpty: boolean
  onRetry?: () => void
  emptyMessage?: string
  children: React.ReactNode
}

export function ListContent({
  loading,
  error,
  isEmpty,
  onRetry,
  emptyMessage = "Нет данных.",
  children,
}: ListContentProps) {
  if (loading) {
    return <DataTableSkeleton showToolbar={false} />
  }

  if (error) {
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

  return <div className="animate-skeleton-fade-in">{children}</div>
}
