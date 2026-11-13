"use client"
import { useEffect, useState } from "react"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useSWRConfig } from "swr"
import { Pencil, Check, RefreshCw, Plus, RotateCcw, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Empty, EmptyHeader, EmptyTitle, EmptyDescription } from "@/components/ui/empty"
import { useDashboardStore } from "@/stores/useDashboardStore"
import { useAuthStore, isSystemAdmin } from "@/stores/useAuthStore"
import { WidgetGrid } from "@/components/dashboard/widgets/widget-grid"
import { WidgetGalleryDialog } from "@/components/dashboard/widgets/widget-gallery-dialog"
import { DashboardDataScope, RefreshStamp } from "@/components/dashboard/widgets/refresh-stamp"

export default function DashboardPage() {
  const { markDirty, markClean } = useTabDirty()
  const [galleryOpen, setGalleryOpen] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const { mutate } = useSWRConfig()
  const loadLayout = useDashboardStore(s => s.loadLayout)
  const isEditMode = useDashboardStore(s => s.isEditMode)
  const resetToDefault = useDashboardStore(s => s.resetToDefault)
  const isSaving = useDashboardStore(s => s.isSaving)
  const saveLayout = useDashboardStore(s => s.saveLayout)
  const setEditMode = useDashboardStore(s => s.setEditMode)
  const cancelEdit = useDashboardStore(s => s.cancelEdit)
  const loadError = useDashboardStore(s => s.loadError)
  const saveError = useDashboardStore(s => s.saveError)
  const isLoaded = useDashboardStore(s => s.isLoaded)
  const layout = useDashboardStore(s => s.layout)
  useEffect(() => { if (isEditMode) markDirty(); else markClean() }, [isEditMode, markDirty, markClean])
  const user = useAuthStore(s => s.user)
  const admin = isSystemAdmin(user)
  useEffect(() => { void loadLayout() }, [loadLayout])
  const refresh = async () => {
    setRefreshing(true)
    try { await mutate(key => Array.isArray(key) && typeof key[1] === "string" && key[1].startsWith("widget:admin-"), undefined, { revalidate: true }) }
    catch { /* Each affected widget displays its own retry action. */ }
    finally { setRefreshing(false) }
  }
  return <ScrollArea className="flex-1"><div className="p-4 md:p-6">
    <div className="mb-5 flex flex-wrap items-end justify-between gap-3"><div className="space-y-1"><h1 className="text-xl font-semibold tracking-tight">Обзор платформы</h1><p className="text-sm text-muted-foreground">Операции, средства и состояние обработки</p>{admin && !isEditMode && <RefreshStamp />}</div>{admin && isLoaded && <div className="flex flex-wrap items-center gap-2">{isEditMode ? <><Button variant="outline" size="sm" onClick={() => setGalleryOpen(true)}><Plus data-icon="inline-start" />Добавить виджет</Button><Button variant="outline" size="sm" onClick={resetToDefault}><RotateCcw data-icon="inline-start" />Стандартная раскладка</Button><Button variant="ghost" size="sm" disabled={isSaving} onClick={cancelEdit}><X data-icon="inline-start" />Отмена</Button><Button size="sm" disabled={isSaving} onClick={async () => { if (await saveLayout()) setEditMode(false) }}><Check data-icon="inline-start" />{isSaving ? "Сохранение…" : "Сохранить раскладку"}</Button></> : <><Button variant="outline" size="sm" disabled={refreshing} onClick={() => void refresh()}><RefreshCw data-icon="inline-start" />{refreshing ? "Обновление…" : "Обновить"}</Button><Button variant="outline" size="sm" onClick={() => setEditMode(true)}><Pencil data-icon="inline-start" />Настроить</Button></>}</div>}</div>
    {(loadError || saveError) && <Alert variant="destructive" className="mb-4"><AlertTitle>{saveError ? "Раскладка не сохранена" : "Не удалось загрузить раскладку"}</AlertTitle><AlertDescription><p>{saveError ?? loadError}</p><Button variant="outline" size="sm" className="mt-2" onClick={() => void (saveError ? saveLayout() : loadLayout())}>Повторить</Button></AlertDescription></Alert>}
    {!admin ? <Empty><EmptyHeader><EmptyTitle>Обзор доступен администратору системы</EmptyTitle><EmptyDescription>Откройте документы и отчёты через меню.</EmptyDescription></EmptyHeader></Empty> : !isLoaded ? <Skeleton className="h-72 w-full" /> : !layout.widgets.length ? <Empty><EmptyHeader><EmptyTitle>На странице пока нет виджетов</EmptyTitle><EmptyDescription>Добавьте нужные блоки или восстановите стандартную раскладку.</EmptyDescription></EmptyHeader><Button onClick={() => { setEditMode(true); setGalleryOpen(true) }}>Добавить виджет</Button></Empty> : <DashboardDataScope><WidgetGrid /></DashboardDataScope>}
    {admin && <WidgetGalleryDialog open={galleryOpen} onOpenChange={setGalleryOpen} />}
  </div></ScrollArea>
}
