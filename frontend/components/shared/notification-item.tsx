// frontend/components/shared/notification-item.tsx
"use client"

import * as React from "react"
import { format, isToday } from "date-fns"
import { ru } from "date-fns/locale"
import {
    AlertCircle,
    AlertTriangle,
    Check,
    CheckCircle2,
    CircleDot,
    ExternalLink,
    Info,
    MoreHorizontal,
    Trash2,
} from "lucide-react"

import { cn } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import type { NotificationResponse, NotifSeverity } from "@/types/notification"

// ── Severity config ─────────────────────────────────────────────────────

const SEVERITY_CONFIG: Record<NotifSeverity, {
    icon: typeof Info
    colorClass: string
    borderClass: string
    bgClass: string
}> = {
    info: {
        icon: Info,
        colorClass: "text-blue-500",
        borderClass: "border-l-blue-500/60",
        bgClass: "bg-blue-500/5",
    },
    warning: {
        icon: AlertTriangle,
        colorClass: "text-amber-500",
        borderClass: "border-l-amber-500/60",
        bgClass: "bg-amber-500/5",
    },
    error: {
        icon: AlertCircle,
        colorClass: "text-destructive",
        borderClass: "border-l-destructive/60",
        bgClass: "bg-destructive/5",
    },
    success: {
        icon: CheckCircle2,
        colorClass: "text-emerald-500",
        borderClass: "border-l-emerald-500/60",
        bgClass: "bg-emerald-500/5",
    },
}

// ── Types ───────────────────────────────────────────────────────────────

/** Extended notification with optional grouping metadata. */
export interface CollapsedNotification extends NotificationResponse {
    /** Number of collapsed items (1 = no grouping). */
    count: number
    /** IDs of all collapsed items (for batch operations). */
    groupIds: string[]
    /** Whether all items in the group are read. */
    allRead: boolean
}

interface NotificationItemProps {
    notification: NotificationResponse | CollapsedNotification
    /** "full" for /notifications page, "compact" for bell panel */
    variant?: "full" | "compact"
    onMarkAsRead: (id: string) => void
    onMarkAsUnread?: (id: string) => void
    onDelete?: (id: string) => void
    /** Batch mark-as-read for groups. */
    onBatchMarkAsRead?: (ids: string[]) => void
    /** Batch delete for groups. */
    onBatchDelete?: (ids: string[]) => void
    onNavigate: (link: string) => void
}

// ── Grouping utility ────────────────────────────────────────────────────

/**
 * Collapses consecutive notifications with the same title + message
 * into a single CollapsedNotification with a count.
 * Preserves the latest (first) item as the representative.
 */
export function collapseNotifications(items: NotificationResponse[]): CollapsedNotification[] {
    if (items.length === 0) return []

    const result: CollapsedNotification[] = []
    let current: CollapsedNotification | null = null

    for (const item of items) {
        const key = `${item.title}|||${item.message}`
        const currentKey = current ? `${current.title}|||${current.message}` : null

        if (current && key === currentKey) {
            // Same group — merge
            current.count++
            current.groupIds.push(item.id)
            if (!item.isRead) current.allRead = false
        } else {
            // New group
            if (current) result.push(current)
            current = {
                ...item,
                count: 1,
                groupIds: [item.id],
                allRead: item.isRead,
            }
        }
    }
    if (current) result.push(current)

    return result
}

// ── Date grouping ───────────────────────────────────────────────────────

export interface NotificationGroup {
    label: string
    items: NotificationResponse[]
}

export function groupNotificationsByDate(
    items: NotificationResponse[],
    mode: "full" | "compact" = "full"
): NotificationGroup[] {
    const groups = new Map<string, NotificationResponse[]>()
    const keyOrder: string[] = []

    for (const item of items) {
        const d = new Date(item.createdAt)
        let label: string
        if (isToday(d)) {
            label = "Сегодня"
        } else if (mode === "compact") {
            const diff = Math.floor((Date.now() - d.getTime()) / 86400000)
            label = diff === 1 ? "Вчера" : "Ранее"
        } else {
            label = format(d, "d MMMM yyyy", { locale: ru })
        }

        if (!groups.has(label)) {
            groups.set(label, [])
            keyOrder.push(label)
        }
        groups.get(label)!.push(item)
    }

    return keyOrder.map((label) => ({
        label,
        items: groups.get(label)!,
    }))
}

// ── Time formatting ─────────────────────────────────────────────────────

function formatNotificationTime(createdAt: string, groupIsToday: boolean): string {
    const d = new Date(createdAt)
    if (groupIsToday || isToday(d)) {
        return format(d, "HH:mm")
    }
    return format(d, "d MMM HH:mm", { locale: ru })
}

// ── Event isolation helper ──────────────────────────────────────────────

/** Stops event from reaching parent card's onClick handler.
 *  Applied to the dropdown wrapper to block all phases:
 *  pointerdown → mousedown → click → pointerup → mouseup */
const stopAllPropagation: React.HTMLAttributes<HTMLDivElement> = {
    onClick: (e) => e.stopPropagation(),
    onPointerDown: (e) => e.stopPropagation(),
    onPointerUp: (e) => e.stopPropagation(),
    onMouseDown: (e) => e.stopPropagation(),
    onMouseUp: (e) => e.stopPropagation(),
}

// ── Component ───────────────────────────────────────────────────────────

export function NotificationItem({
    notification,
    variant = "full",
    onMarkAsRead,
    onMarkAsUnread,
    onDelete,
    onBatchMarkAsRead,
    onBatchDelete,
    onNavigate,
}: NotificationItemProps) {
    const hasLink = !!notification.link
    const isCompact = variant === "compact"
    const hasActions = !!(onMarkAsUnread || onDelete)

    // Grouping metadata
    const isGrouped = "count" in notification && notification.count > 1
    const groupCount = isGrouped ? (notification as CollapsedNotification).count : 1
    const groupIds = isGrouped ? (notification as CollapsedNotification).groupIds : [notification.id]
    const allRead = isGrouped ? (notification as CollapsedNotification).allRead : notification.isRead

    // Severity
    const severity = notification.severity || "info"
    const config = SEVERITY_CONFIG[severity]
    const SeverityIcon = config.icon

    const handleClick = () => {
        if (!notification.isRead) {
            if (isGrouped && onBatchMarkAsRead) {
                onBatchMarkAsRead(groupIds)
            } else {
                onMarkAsRead(notification.id)
            }
        }
        if (notification.link) {
            onNavigate(notification.link)
        }
    }

    return (
        <div
            className={cn(
                "group relative flex gap-3 transition-all duration-200",
                isCompact
                    ? "rounded-md px-3 py-2.5 text-sm"
                    : "rounded-lg border px-4 py-3.5",
                // Left border accent for unread items (severity-colored)
                !isCompact && !allRead && "border-l-[3px]",
                !isCompact && !allRead && config.borderClass,
                hasLink && "cursor-pointer",
                !allRead
                    ? isCompact
                        ? "bg-muted/40 hover:bg-muted/60"
                        : cn("hover:bg-muted/40 shadow-sm", config.bgClass)
                    : isCompact
                        ? "hover:bg-muted/30"
                        : "border-border/60 bg-card hover:bg-muted/40"
            )}
            role={hasLink ? "button" : undefined}
            tabIndex={hasLink ? 0 : undefined}
            onClick={handleClick}
            onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault()
                    handleClick()
                }
            }}
        >
            {/* Severity icon (replaces unread dot) */}
            <div className={cn("flex shrink-0", isCompact ? "pt-1" : "pt-0.5")}>
                <SeverityIcon
                    className={cn(
                        "transition-all duration-300",
                        isCompact ? "h-3.5 w-3.5" : "h-4 w-4",
                        allRead ? "text-muted-foreground/40" : config.colorClass
                    )}
                />
            </div>

            {/* Content */}
            <div className={cn("flex-1 min-w-0", isCompact ? "space-y-0.5" : "space-y-1")}>
                <div className="flex items-start justify-between gap-2">
                    <div className="flex items-center gap-1.5 min-w-0">
                        <span
                            className={cn(
                                "leading-snug text-sm truncate",
                                !allRead
                                    ? isCompact ? "font-medium" : "font-semibold"
                                    : "font-medium text-muted-foreground/80"
                            )}
                        >
                            {notification.title}
                        </span>
                        {/* Group count badge */}
                        {isGrouped && (
                            <Badge
                                variant="secondary"
                                className="h-4.5 min-w-5 justify-center px-1 text-[10px] font-bold rounded-full shrink-0"
                            >
                                ×{groupCount}
                            </Badge>
                        )}
                    </div>
                    <div className="flex items-center gap-1 shrink-0">
                        <span className={cn(
                            "tabular-nums",
                            isCompact
                                ? "text-[10px] text-muted-foreground/70"
                                : "text-[11px] text-muted-foreground/60"
                        )}>
                            {isCompact
                                ? format(new Date(notification.createdAt), "HH:mm")
                                : formatNotificationTime(notification.createdAt, isToday(new Date(notification.createdAt)))
                            }
                        </span>
                    </div>
                </div>

                <p
                    className={cn(
                        "leading-relaxed",
                        isCompact ? "text-xs line-clamp-2" : "text-sm",
                        !allRead
                            ? isCompact ? "text-muted-foreground" : "text-foreground/80"
                            : "text-muted-foreground/70"
                    )}
                >
                    {notification.message}
                </p>

                {hasLink && !isCompact && (
                    <div className="flex items-center gap-1 pt-0.5 text-xs text-primary/70 opacity-0 group-hover:opacity-100 transition-opacity">
                        <ExternalLink className="h-3 w-3" />
                        <span>Перейти</span>
                    </div>
                )}

                {hasLink && isCompact && (
                    <ExternalLink className="absolute right-3 top-3 h-3 w-3 shrink-0 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                )}
            </div>

            {/* P2: Hover actions — fully event-isolated from parent card */}
            {hasActions && !isCompact && (
                <div
                    className="absolute right-2 top-2"
                    {...stopAllPropagation}
                >
                    <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                            <Button
                                variant="ghost"
                                size="icon"
                                className="h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity"
                            >
                                <MoreHorizontal className="h-3.5 w-3.5" />
                            </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end" className="w-48">
                            {allRead && onMarkAsUnread && (
                                <DropdownMenuItem onSelect={() => onMarkAsUnread(notification.id)}>
                                    <CircleDot className="mr-2 h-4 w-4" />
                                    Отметить непрочитанным
                                </DropdownMenuItem>
                            )}
                            {!allRead && (
                                <DropdownMenuItem onSelect={() => {
                                    if (isGrouped && onBatchMarkAsRead) {
                                        onBatchMarkAsRead(groupIds)
                                    } else {
                                        onMarkAsRead(notification.id)
                                    }
                                }}>
                                    <Check className="mr-2 h-4 w-4" />
                                    {isGrouped ? `Прочитать все (${groupCount})` : "Отметить прочитанным"}
                                </DropdownMenuItem>
                            )}
                            {onDelete && (
                                <DropdownMenuItem
                                    className="text-destructive focus:text-destructive"
                                    onSelect={() => {
                                        if (isGrouped && onBatchDelete) {
                                            onBatchDelete(groupIds)
                                        } else {
                                            onDelete(notification.id)
                                        }
                                    }}
                                >
                                    <Trash2 className="mr-2 h-4 w-4" />
                                    {isGrouped ? `Удалить все (${groupCount})` : "Удалить"}
                                </DropdownMenuItem>
                            )}
                        </DropdownMenuContent>
                    </DropdownMenu>
                </div>
            )}
        </div>
    )
}
