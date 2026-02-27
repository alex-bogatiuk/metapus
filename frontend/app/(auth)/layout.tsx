"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { useAuthStore } from "@/stores/useAuthStore"

export default function AuthLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const router = useRouter()
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const [hydrated, setHydrated] = useState(false)

  useEffect(() => {
    setHydrated(true)
  }, [])

  useEffect(() => {
    if (hydrated && isAuthenticated) {
      router.replace("/")
    }
  }, [hydrated, isAuthenticated, router])

  if (!hydrated || isAuthenticated) {
    return null
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/40">
      {children}
    </div>
  )
}
