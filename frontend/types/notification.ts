/**
 * Notification types — mirrors backend domain/notifications model.
 */

/** A single in-app notification. */
export interface NotificationResponse {
    id: string
    userId: string
    title: string
    message: string
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
