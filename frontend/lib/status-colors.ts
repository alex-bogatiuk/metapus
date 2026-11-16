/**
 * Status Color Map — shared utility for status badge rendering.
 *
 * Centralized status → Tailwind class mapping for reuse across entity
 * list columns, detail views, and any future status displays.
 *
 * Uses Tailwind design tokens only (no hex colors per §Frontend rules).
 */

/** Semantic status categories mapped to Tailwind classes. */
const STATUS_CATEGORY_CLASSES = {
    success: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400",
    warning: "bg-amber-500/15 text-amber-700 dark:text-amber-400",
    danger: "bg-red-500/15 text-red-700 dark:text-red-400",
    info: "bg-blue-500/15 text-blue-700 dark:text-blue-400",
    neutral: "bg-secondary text-secondary-foreground",
} as const

export type StatusCategory = keyof typeof STATUS_CATEGORY_CLASSES

/**
 * Maps status display names to their visual category.
 * New statuses should be added here — not inline in column renderers.
 */
const STATUS_CATEGORY_MAP: Record<string, StatusCategory> = {
    "Создан": "neutral",
    "Подтверждён": "success",
    "Отменён": "danger",
    "Ошибка": "danger",
    "Активен": "success",
    "Одобрен": "success",
    "На проверке": "warning",
    "Отклонён": "danger",
    "Частично ошибка": "warning",
}

/** Adds a configuration's status display names. */
export function registerStatusColors(statuses: Record<string, StatusCategory>): void {
    Object.assign(STATUS_CATEGORY_MAP, statuses)
}

/**
 * Returns the Tailwind CSS classes for a given status string.
 * Falls back to neutral styling for unknown statuses.
 */
export function getStatusColorClass(status: string): string {
    const category = STATUS_CATEGORY_MAP[status] ?? "neutral"
    return STATUS_CATEGORY_CLASSES[category]
}
