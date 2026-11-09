import type { AuthUserResponse } from "@/types/auth"
import { isSystemAdmin } from "@/stores/useAuthStore"

export function canEditSavedView(view: { visibility: "personal" | "shared" | "system"; authorId: string | null } | undefined, user: AuthUserResponse | null): boolean {
  if (!view || !user) return false
  const owner = view.authorId === user.id
  if (view.visibility === "personal") return owner
  if (view.visibility === "shared") return owner || isSystemAdmin(user)
  return isSystemAdmin(user)
}
