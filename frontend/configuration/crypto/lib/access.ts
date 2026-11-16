/**
 * Merchant portal access: the portal is a workspace for users whose session
 * carries merchant access scopes.
 */
import { registerWorkspace } from "@/lib/access"
import type { AuthUserResponse } from "@/types/auth"

/** Merchants the user may open in the Portal, from the session access scopes. */
export function merchantIds(user: AuthUserResponse | null): string[] {
  return user?.accessScopes?.merchant ?? []
}

/** User has access to the Merchant Portal (`/portal`). */
export function hasPortalAccess(user: AuthUserResponse | null): boolean {
  return merchantIds(user).length > 0
}

export function registerPortalWorkspace(): void {
  registerWorkspace({ path: "/portal", hasAccess: hasPortalAccess })
}
