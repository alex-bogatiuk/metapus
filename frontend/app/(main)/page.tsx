"use client"

import { useEffect, useState } from "react"
import { Pencil, Check, RotateCcw, Plus } from "lucide-react"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { useDashboardStore } from "@/stores/useDashboardStore"
import { WidgetGrid } from "@/components/dashboard/widgets/widget-grid"
import { WidgetGalleryDialog } from "@/components/dashboard/widgets/widget-gallery-dialog"

export default function DashboardPage() {
  const [galleryOpen, setGalleryOpen] = useState(false)
  const isEditMode = useDashboardStore((s) => s.isEditMode)
  const setEditMode = useDashboardStore((s) => s.setEditMode)
  const loadLayout = useDashboardStore((s) => s.loadLayout)
  const isLoaded = useDashboardStore((s) => s.isLoaded)

  useEffect(() => {
    loadLayout()
  }, [loadLayout])

  const today = new Date().toLocaleDateString("ru-RU", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  })

  if (!isLoaded) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent" />
      </div>
    )
  }

  return (
    <ScrollArea className="flex-1">
      <div className="p-6">
        <div className="mb-4 flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold text-foreground">
              Начальная страница
            </h1>
            <p className="mt-0.5 text-sm text-muted-foreground">
              {today} — Metapus ERP
            </p>
          </div>
          <div className="flex items-center gap-2">
            {isEditMode ? (
              <>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setGalleryOpen(true)}
                >
                  <Plus className="mr-1.5 h-3.5 w-3.5" />
                  Добавить виджет
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => useDashboardStore.getState().resetToDefault()}
                >
                  <RotateCcw className="mr-1.5 h-3.5 w-3.5" />
                  Сбросить
                </Button>
                <Button size="sm" onClick={() => setEditMode(false)}>
                  <Check className="mr-1.5 h-3.5 w-3.5" />
                  Готово
                </Button>
              </>
            ) : (
              <>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => loadLayout()}
                >
                  <RotateCcw className="mr-1.5 h-3.5 w-3.5" />
                  Обновить
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setEditMode(true)}
                >
                  <Pencil className="mr-1.5 h-3.5 w-3.5" />
                  Настроить
                </Button>
              </>
            )}
          </div>
        </div>

        <WidgetGrid />

        <WidgetGalleryDialog open={galleryOpen} onOpenChange={setGalleryOpen} />
      </div>
    </ScrollArea>
  )
}

