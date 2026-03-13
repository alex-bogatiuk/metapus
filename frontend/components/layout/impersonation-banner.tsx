"use client"

import { useRouter } from "next/navigation"
import { LogOut } from "lucide-react"
import { useAuthStore } from "@/stores/useAuthStore"

export function ImpersonationBanner() {
  const router = useRouter()
  const isImpersonating = useAuthStore((s) => s.isImpersonating)
  const user = useAuthStore((s) => s.user)
  const originalUser = useAuthStore((s) => s.originalSession?.user)
  const stopImpersonation = useAuthStore((s) => s.stopImpersonation)

  if (!isImpersonating) return null

  return (
    <div className="flex items-center justify-between gap-3 bg-amber-500 px-4 py-1.5 text-xs font-medium text-white">
      <span>
        Вы вошли как <strong>{user?.fullName || user?.email}</strong>
        {originalUser && <> (от имени {originalUser.fullName || originalUser.email})</>}
      </span>
      <button
        onClick={() => {
          stopImpersonation()
          router.push("/")
          router.refresh()
        }}
        className="flex items-center gap-1.5 rounded bg-white/20 px-2.5 py-1 text-[11px] font-semibold hover:bg-white/30 transition-colors"
      >
        <LogOut className="h-3 w-3" />
        Завершить сеанс имитации
      </button>
    </div>
  )
}
