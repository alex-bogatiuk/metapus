"use client"

import * as React from "react"
import { useRouter, usePathname } from "next/navigation"
import { X, FileText } from "lucide-react"
import { cn } from "@/lib/utils"
import { SidebarTrigger } from "@/components/ui/sidebar"
import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { useTabsStore, type Tab } from "@/stores/useTabsStore"
import { useFormDraftStore } from "@/stores/useFormDraftStore"
import { useTabStateStore } from "@/stores/useTabStateStore"

export function SiteHeader() {
    const router = useRouter()
    const pathname = usePathname()
    const { tabs, activeTabId, setActiveTab, closeTab } = useTabsStore()

    // Dialog state for dirty tab close confirmation
    const [pendingCloseTabId, setPendingCloseTabId] = React.useState<string | null>(null)
    const pendingTab = pendingCloseTabId
        ? tabs.find((t) => t.id === pendingCloseTabId)
        : null

    // Sync: when pathname changes externally, update active tab
    React.useEffect(() => {
        const matchingTab = tabs.find((t) => t.id === pathname)
        if (matchingTab && matchingTab.id !== activeTabId) {
            setActiveTab(matchingTab.id)
        }
    }, [pathname, tabs, activeTabId, setActiveTab])

    const handleTabClick = (tab: Tab) => {
        if (tab.id === activeTabId) return
        setActiveTab(tab.id)
        router.push(tab.url)
    }

    const handleTabClose = (e: React.MouseEvent, tab: Tab) => {
        e.stopPropagation()

        if (tab.isDirty) {
            setPendingCloseTabId(tab.id)
            return
        }

        doCloseTab(tab.id)
    }

    const clearDraft = useFormDraftStore((s) => s.clearDraft)
    const clearTabState = useTabStateStore((s) => s.clearTab)

    const doCloseTab = (id: string) => {
        const isActive = id === activeTabId

        closeTab(id)
        clearDraft(id)
        clearTabState(id)

        // Navigate to the new active tab if we closed the current one
        if (isActive) {
            const remaining = tabs.filter((t) => t.id !== id)
            if (remaining.length > 0) {
                const closedIndex = tabs.findIndex((t) => t.id === id)
                const newIndex = Math.min(closedIndex, remaining.length - 1)
                router.push(remaining[newIndex].url)
            }
        }
    }

    const handleConfirmClose = () => {
        if (pendingCloseTabId) {
            doCloseTab(pendingCloseTabId)
            setPendingCloseTabId(null)
        }
    }

    const handleCancelClose = () => {
        setPendingCloseTabId(null)
    }

    return (
        <>
            <header className="relative flex h-12 shrink-0 items-end bg-muted/40 pr-4 after:pointer-events-none after:absolute after:inset-x-0 after:bottom-0 after:h-px after:bg-border">
                {/* Sidebar trigger + Separator */}
                <div className="flex h-full items-center pl-2 pr-4">
                    <SidebarTrigger className="h-7 w-7 shrink-0" />
                    <div className="mx-4 h-6 w-px bg-border" />
                </div>

                {/* Tab bar — all tabs */}
                <div className="flex flex-1 items-end gap-1 overflow-x-auto scrollbar-hide">
                    {tabs.map((tab) => {
                        const isActive = tab.id === activeTabId
                        return (
                            <button
                                key={tab.id}
                                onClick={() => handleTabClick(tab)}
                                className={cn(
                                    "group flex h-[36px] shrink-0 items-center gap-2 rounded-t-md border-x border-t px-3 text-xs font-medium transition-colors",
                                    "min-w-[8rem] max-w-[14rem]",
                                    isActive
                                        ? "relative z-10 -mb-px bg-background text-foreground border-border border-t-primary border-t-2"
                                        : "relative bg-muted/30 text-muted-foreground border-border hover:bg-muted hover:text-foreground"
                                )}
                            >
                                {/* Title + dirty indicator */}
                                <span className="flex-1 truncate text-left">
                                    {tab.isDirty && (
                                        <span className="mr-1 text-destructive">●</span>
                                    )}
                                    {tab.title}
                                </span>

                                {/* Close button — show if not the only tab */}
                                {tabs.length > 1 && (
                                    <span
                                        role="button"
                                        tabIndex={0}
                                        onClick={(e) => handleTabClose(e, tab)}
                                        onKeyDown={(e) => {
                                            if (e.key === "Enter" || e.key === " ") {
                                                handleTabClose(e as unknown as React.MouseEvent, tab)
                                            }
                                        }}
                                        className={cn(
                                            "flex h-4 w-4 shrink-0 items-center justify-center rounded-sm transition-colors",
                                            "hover:bg-gray-200 hover:text-foreground",
                                            isActive
                                                ? "text-muted-foreground"
                                                : "opacity-0 group-hover:opacity-100 text-muted-foreground/60"
                                        )}
                                    >
                                        <X className="h-3 w-3" />
                                    </span>
                                )}
                            </button>
                        )
                    })}
                </div>
            </header>

            {/* Dirty-state AlertDialog */}
            <AlertDialog
                open={!!pendingCloseTabId}
                onOpenChange={(open) => {
                    if (!open) setPendingCloseTabId(null)
                }}
            >
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>Несохранённые изменения</AlertDialogTitle>
                        <AlertDialogDescription>
                            Данные были изменены в вкладке &laquo;{pendingTab?.title}&raquo;.
                            Закрыть вкладку без сохранения?
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel onClick={handleCancelClose}>
                            Отмена
                        </AlertDialogCancel>
                        <AlertDialogAction
                            onClick={handleConfirmClose}
                            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                        >
                            Закрыть без сохранения
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </>
    )
}
