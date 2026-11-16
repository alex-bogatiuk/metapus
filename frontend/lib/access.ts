/**
 * Centralized access-level helpers.
 *
 * Single source of truth for routing decisions that happen in:
 *  - login-form.tsx  (post-login redirect)
 *  - (main)/layout   (ERP render gate)
 *  - a configuration's own workspace layout
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

/** A separate interface a configuration serves to users without ERP access. */
export interface Workspace {
  /** Root URL of the workspace, e.g. "/cabinet" */
  path: string
  hasAccess(user: AuthUserResponse): boolean
}

const workspaces: Workspace[] = []

/** Adds a configuration's workspace. Call at start-up. */
export function registerWorkspace(workspace: Workspace): void {
  workspaces.push(workspace)
}

/**
 * Where the user works: the ERP for users with ERP access, otherwise the
 * first registered workspace they may open. Users with neither see the ERP,
 * whose API then denies them.
 */
export function landingPath(user: AuthUserResponse | null): string {
  if (!user || hasErpAccess(user)) return "/"
  return workspaces.find((workspace) => workspace.hasAccess(user))?.path ?? "/"
}
