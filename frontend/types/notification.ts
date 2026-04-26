/**
 * Notification types — mirrors backend domain/notifications model.
 */

/** Notification severity level — drives icon and color in the UI. */
export type NotifSeverity = "info" | "warning" | "error" | "success"

/** A single in-app notification. */
export interface NotificationResponse {
    id: string
    userId: string
    title: string
    message: string
    severity: NotifSeverity
    link?: string | null
    isRead: boolean
    attributes?: Record<string, unknown>
    version: number
    deletionMark: boolean
    createdAt: string
    updatedAt: string
}

/** Response envelope for the notification list endpoint. */
export interface NotificationListResponse {
    items: NotificationResponse[]
    unreadCount: number
}
