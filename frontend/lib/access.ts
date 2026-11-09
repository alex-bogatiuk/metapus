/**
 * Centralized access-level helpers.
 *
 * Single source of truth for routing decisions that happen in:
 *  - login-form.tsx  (post-login redirect)
 *  - (main)/layout   (ERP render gate)
 *  - (portal)/layout  (Portal render gate)
 *
 * Rules are intentionally simple — they derive from JWT claims only,
 * so they work synchronously without any async calls.
 */
import type { AuthUserResponse } from "@/types/auth"

/** User has access to the ERP back-office interface (`/`). */
export function hasErpAccess(user: AuthUserResponse | null): boolean {
  if (!user) return false
  return user.isAdmin || (user.roles?.length ?? 0) > 0
}

/** User has access to the Merchant Portal (`/portal`). */
export function hasPortalAccess(user: AuthUserResponse | null): boolean {
  if (!user) return false
  return (user.merchantIds?.length ?? 0) > 0
}

/**
 * User should be routed to the Portal by default.
 * True when: no ERP access AND has portal access.
 */
export function isPortalOnly(user: AuthUserResponse | null): boolean {
  return !hasErpAccess(user) && hasPortalAccess(user)
}
