// frontend/lib/system-permissions.ts
/**
 * Permissions of the platform's system administration, as the backend declares
 * them (internal/domain/auth/system_permissions.go). A system administrator
 * receives them from /auth/me/permissions without a grant.
 */

/** Users and roles, security profiles, system settings, integrations, automation, tools, tenants. */
export const SYSTEM_ADMIN = "system:admin"
/** The event log. */
export const EVENT_LOG_READ = "event_log:read"
/** The worker job monitor. */
export const WORKER_JOB_READ = "worker_job:read"
