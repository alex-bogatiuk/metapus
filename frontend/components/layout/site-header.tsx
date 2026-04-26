"use client"

import * as React from "react"
import { useRouter, usePathname } from "next/navigation"
import { SidebarTrigger } from "@/components/ui/sidebar"
import { TooltipProvider } from "@/components/ui/tooltip"
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
import { useCloseTab } from "@/hooks/useCloseTab"
import { TabBar } from "./tab-bar"
import { TabOverflow } from "./tab-overflow"
import { OpenUrlPopover } from "./open-url-popover"
import { NotificationBell } from "./notification-bell"
import { NotificationPanel } from "./notification-panel"
import { useShortcut } from "@/hooks/useShortcut"

/** Pending close state — single tab or batch (with dirty-tab list). */
type PendingClose =
    | { kind: "single"; tabId: string }
    | { kind: "batch"; dirtyTabs: Tab[]; action: () => void }

export function SiteHeader() {
    const router = useRouter()
    const pathname = usePathname()
    const { tabs, activeTabId, setActiveTab } = useTabsStore()
    const { closeOne, closeOthers, closeRight, closeAll } = useCloseTab()

    const [pendingClose, setPendingClose] = React.useState<PendingClose | null>(null)
    const openUrlTriggerRef = React.useRef<HTMLButtonElement>(null)

    // Sync: when pathname changes externally, update active tab
    React.useEffect(() => {
        const matchingTab = tabs.find((t) => t.id === pathname)
        if (matchingTab && matchingTab.id !== activeTabId) {
            setActiveTab(matchingTab.id)
        }
    }, [pathname, tabs, activeTabId, setActiveTab])

    // ── Tab click / close handlers ──

    const handleTabClick = React.useCallback(
        (tab: Tab) => {
            if (tab.id === activeTabId) return
            setActiveTab(tab.id)
            router.push(tab.url)
        },
        [activeTabId, setActiveTab, router],
    )

    const handleTabClose = React.useCallback(
        (e: React.MouseEvent, tab: Tab) => {
            e.stopPropagation()
            if (tab.isDirty) {
                setPendingClose({ kind: "single", tabId: tab.id })
                return
            }
            closeOne(tab.id)
        },
        [closeOne],
    )

    /** Batch close with dirty-check: shows confirmation if any dirty tabs. */
    const batchCloseWithDirtyCheck = React.useCallback(
        (tabsToClose: Tab[], action: () => void) => {
            const dirtyTabs = tabsToClose.filter((t) => t.isDirty)
            if (dirtyTabs.length > 0) {
                setPendingClose({ kind: "batch", dirtyTabs, action })
                return
            }
            action()
        },
        [],
    )

    const handleCloseOthers = React.useCallback(
        (keepId: string) => {
            const toClose = tabs.filter((t) => t.id !== keepId)
            batchCloseWithDirtyCheck(toClose, () => closeOthers(keepId))
        },
        [tabs, closeOthers, batchCloseWithDirtyCheck],
    )

    const handleCloseRight = React.useCallback(
        (id: string) => {
            const idx = tabs.findIndex((t) => t.id === id)
            const toClose = tabs.slice(idx + 1)
            batchCloseWithDirtyCheck(toClose, () => closeRight(id))
        },
        [tabs, closeRight, batchCloseWithDirtyCheck],
    )

    const handleCloseAll = React.useCallback(() => {
        const toClose = tabs.filter((t) => t.id !== "/")
        batchCloseWithDirtyCheck(toClose, () => closeAll())
    }, [tabs, closeAll, batchCloseWithDirtyCheck])

    // ── Confirm / cancel dirty-close ──

    const handleConfirmClose = () => {
        if (!pendingClose) return
        if (pendingClose.kind === "single") {
            closeOne(pendingClose.tabId)
        } else {
            pendingClose.action()
        }
        setPendingClose(null)
    }

    // ── Resolve dialog text ──

    const dialogText = React.useMemo(() => {
        if (!pendingClose) return { title: "", body: "" }
        if (pendingClose.kind === "single") {
            const tab = tabs.find((t) => t.id === pendingClose.tabId)
            return {
                title: "Несохранённые изменения",
                body: `Во вкладке «${tab?.title ?? ""}» есть несохранённые изменения. Закрыть без сохранения?`,
            }
        }
        const n = pendingClose.dirtyTabs.length
        return {
            title: "Несохранённые изменения",
            body: `Есть несохранённые изменения в ${n} ${n === 1 ? "вкладке" : "вкладках"}. Закрыть без сохранения?`,
        }
    }, [pendingClose, tabs])

    // ── Keyboard shortcuts (centralized via useShortcut) ──

    const handleCloseActiveTab = React.useCallback(() => {
        const active = tabs.find((t) => t.id === activeTabId)
        if (active) {
            if (active.isDirty) {
                setPendingClose({ kind: "single", tabId: active.id })
            } else {
                closeOne(active.id)
            }
        }
    }, [tabs, activeTabId, closeOne])

    const handlePrevTab = React.useCallback(() => {
        const idx = tabs.findIndex((t) => t.id === activeTabId)
        if (idx > 0) {
            const prev = tabs[idx - 1]
            setActiveTab(prev.id)
            router.push(prev.url)
        }
    }, [tabs, activeTabId, setActiveTab, router])

    const handleNextTab = React.useCallback(() => {
        const idx = tabs.findIndex((t) => t.id === activeTabId)
        if (idx < tabs.length - 1) {
            const next = tabs[idx + 1]
            setActiveTab(next.id)
            router.push(next.url)
        }
    }, [tabs, activeTabId, setActiveTab, router])

    const handleOpenUrl = React.useCallback(() => {
        openUrlTriggerRef.current?.click()
    }, [])

    useShortcut("nav.close-tab", "alt+w", "Закрыть вкладку", "navigation", handleCloseActiveTab)
    useShortcut("nav.close-tab-ctrl", "mod+w", "Закрыть вкладку", "navigation", handleCloseActiveTab)
    useShortcut("nav.prev-tab", "alt+arrowleft", "Предыдущая вкладка", "navigation", handlePrevTab)
    useShortcut("nav.next-tab", "alt+arrowright", "Следующая вкладка", "navigation", handleNextTab)
    useShortcut("nav.open-url", "mod+l", "Открыть по ссылке", "navigation", handleOpenUrl)

    return (
        <>
            <header className="relative flex h-12 shrink-0 items-end bg-muted/40 pr-4 after:pointer-events-none after:absolute after:inset-x-0 after:bottom-0 after:h-px after:bg-border">
                {/* Sidebar trigger + Separator */}
                <div className="flex h-full items-center pl-2 pr-4">
                    <SidebarTrigger className="h-7 w-7 shrink-0" />
                    <div className="mx-4 h-6 w-px bg-border" />
                </div>

                {/* Scrollable tab bar */}
                <TabBar
                    onTabClick={handleTabClick}
                    onTabClose={handleTabClose}
                    onCloseOthers={handleCloseOthers}
                    onCloseRight={handleCloseRight}
                />

                {/* Tab overflow dropdown + Open URL + Notification Bell */}
                <TooltipProvider delayDuration={300}>
                    <div className="flex shrink-0 items-center justify-end px-2 mb-1 gap-1">
                        <TabOverflow onTabClick={handleTabClick} onCloseAll={handleCloseAll} />
                        <NotificationBell />
                        <NotificationPanel />
                        <OpenUrlPopover triggerRef={openUrlTriggerRef} />
                    </div>
                </TooltipProvider>
            </header>

            {/* Dirty-state AlertDialog */}
            <AlertDialog
                open={!!pendingClose}
                onOpenChange={(open) => {
                    if (!open) setPendingClose(null)
                }}
            >
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>{dialogText.title}</AlertDialogTitle>
                        <AlertDialogDescription>
                            {dialogText.body}
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel onClick={() => setPendingClose(null)}>
                            Отменить
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
