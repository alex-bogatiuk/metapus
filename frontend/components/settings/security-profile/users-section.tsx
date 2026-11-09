"use client"

import { api } from "@/lib/api"
import {
  Loader2
} from "lucide-react"
import { useEffect, useState } from "react"

export function UsersSection({ profileId }: { profileId: string }) {
  const [users, setUsers] = useState<{ id: string; email: string; fullName: string }[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    api.security.profiles.listUsers(profileId).then((res) => {
      if (!cancelled) {
        setUsers(res.items ?? [])
        setLoading(false)
      }
    }).catch(() => {
      if (!cancelled) setLoading(false)
    })
    return () => { cancelled = true }
  }, [profileId])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (users.length === 0) {
    return (
      <div className="text-center text-sm text-muted-foreground py-8">
        Нет привязанных пользователей
      </div>
    )
  }

  return (
    <div className="max-w-xl space-y-2">
      {users.map((u) => (
        <div key={u.id} className="rounded-md border px-4 py-2.5 flex items-center gap-3">
          <div className="flex-1 min-w-0">
            <p className="text-sm font-medium truncate">{u.fullName || u.email}</p>
            <p className="text-xs text-muted-foreground truncate">{u.email}</p>
          </div>
        </div>
      ))}
    </div>
  )
}
