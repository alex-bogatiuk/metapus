"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { AppShell } from "@/components/layout/app-shell"
import { useAuthStore } from "@/stores/useAuthStore"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"

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

  useEffect(() => {
    if (hydrated && !isAuthenticated) {
      router.replace("/login")
    }
  }, [hydrated, isAuthenticated, router])

  // Load user preferences from server once authenticated
  useEffect(() => {
    if (hydrated && isAuthenticated && !prefsLoaded) {
      loadPreferences()
    }
  }, [hydrated, isAuthenticated, prefsLoaded, loadPreferences])

  if (!hydrated || !isAuthenticated) {
    return null
  }

  return <AppShell>{children}</AppShell>
}
