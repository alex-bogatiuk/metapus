"use client"

import type { ReactNode } from "react"
import { usePermissions } from "@/hooks/use-permissions"

interface PermissionGateProps {
  /** Permission the user needs to see the content */
  permission: string
  /** What the user cannot open, in the dative: "журналу событий" */
  subject: string
  children: ReactNode
}

/**
 * Renders the content only for a user with the permission, so a page the user
 * cannot open says so instead of failing its requests with 403. Nothing is
 * shown while the permissions load.
 */
export function PermissionGate({ permission, subject, children }: PermissionGateProps) {
  const permissions = usePermissions()
  if (permissions.loading) return null
  if (!permissions.has(permission)) {
    return <p className="p-6 text-sm text-muted-foreground">Нет доступа к {subject}. Обратитесь к администратору.</p>
  }
  return <>{children}</>
}
