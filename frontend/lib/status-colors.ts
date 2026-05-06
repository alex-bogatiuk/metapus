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

type StatusCategory = keyof typeof STATUS_CATEGORY_CLASSES

/**
 * Maps status display names to their visual category.
 * New statuses should be added here — not inline in column renderers.
 */
const STATUS_CATEGORY_MAP: Record<string, StatusCategory> = {
    // Crypto Invoice
    "Создан": "neutral",
    "Ожидает оплаты": "warning",
    "Частично оплачен": "info",
    "Оплачен": "success",
    "Подтверждён": "success",
    "Истёк": "danger",
    "Отменён": "danger",

    // Crypto Payment
    "Обнаружен": "warning",
    "Подтверждается": "info",
    "Подписан": "info",
    "Отправлен": "warning",
    "Ошибка": "danger",
    "Reorged": "danger",

    // Wallet
    "Активен": "success",
    "Свободен": "success",
    "Занят": "warning",
    "Ожидает свипа": "info",

    // Merchant KYB
    "Одобрен": "success",
    "На проверке": "warning",
    "Отклонён": "danger",

    // General
    "Частично ошибка": "warning",
}

/**
 * Returns the Tailwind CSS classes for a given status string.
 * Falls back to neutral styling for unknown statuses.
 */
export function getStatusColorClass(status: string): string {
    const category = STATUS_CATEGORY_MAP[status] ?? "neutral"
    return STATUS_CATEGORY_CLASSES[category]
}
