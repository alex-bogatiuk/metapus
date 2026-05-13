"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { AppShell } from "@/components/layout/app-shell"
import { useAuthStore } from "@/stores/useAuthStore"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import { ThemeProvider } from "@/components/theme-provider"
import { isPortalOnly } from "@/lib/access"

export default function MainLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const router = useRouter()
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const [hydrated, setHydrated] = useState(false)
  const { isLoaded: prefsLoaded, loadPreferences } = useUserPrefsStore()

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setHydrated(true)
  }, [])

  const user = useAuthStore((s) => s.user)

  useEffect(() => {
    if (hydrated && !isAuthenticated) {
      router.replace("/login")
    }
  }, [hydrated, isAuthenticated, router])

  // Redirect portal-only users away from ERP layout.
  const portalOnly = isPortalOnly(user)
  useEffect(() => {
    if (hydrated && isAuthenticated && portalOnly) {
      router.replace("/portal")
    }
  }, [hydrated, isAuthenticated, portalOnly, router])

  // Load user preferences from server once authenticated
  useEffect(() => {
    if (hydrated && isAuthenticated && !prefsLoaded) {
      loadPreferences()
    }
  }, [hydrated, isAuthenticated, prefsLoaded, loadPreferences])

  // Synchronous render gate: never show ERP shell to portal-only merchants.
  // The useEffect above handles the actual redirect; this prevents any flash.
  if (!hydrated || !isAuthenticated || portalOnly) {
    return null
  }

  return (
    <ThemeProvider>
      <AppShell>{children}</AppShell>
    </ThemeProvider>
  )
}
