"use client"

import { useEffect, useSyncExternalStore } from "react"
import { useRouter } from "next/navigation"
import { useAuthStore } from "@/stores/useAuthStore"
import { usePortalStore } from "@/stores/usePortalStore"
import { ThemeProvider } from "@/components/theme-provider"
import { PortalShell } from "@/components/portal/portal-shell"
import { api } from "@/lib/api"
import { hasPortalAccess } from "@/lib/access"

const emptySubscribe = () => () => {}

export default function PortalLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const router = useRouter()
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const user = useAuthStore((s) => s.user)
  const setMerchants = usePortalStore((s) => s.setMerchants)
  const hydrated = useSyncExternalStore(emptySubscribe, () => true, () => false)

  // Auth guard: redirect to login if not authenticated
  useEffect(() => {
    if (hydrated && !isAuthenticated) {
      router.replace("/login")
    }
  }, [hydrated, isAuthenticated, router])

  // Portal access guard: redirect to / if no merchant access
  const portalAccess = hasPortalAccess(user)
  useEffect(() => {
    if (hydrated && isAuthenticated && !portalAccess) {
      router.replace("/")
    }
  }, [hydrated, isAuthenticated, portalAccess, router])

  // Load available merchants from portal API
  useEffect(() => {
    if (!hydrated || !isAuthenticated || !user) return
    if ((user?.merchantIds?.length ?? 0) === 0) return

    api.portal.merchants()
      .then((res) => setMerchants(res.items))
      .catch(() => {
        // Silently fail — merchants will remain empty
      })
  }, [hydrated, isAuthenticated, user, setMerchants])

  if (!hydrated || !isAuthenticated || !portalAccess) {
    return null
  }

  return (
    <ThemeProvider>
      <PortalShell>{children}</PortalShell>
    </ThemeProvider>
  )
}
