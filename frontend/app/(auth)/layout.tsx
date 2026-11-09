"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { LogoIcon } from "@/components/icons/logo"
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
    // eslint-disable-next-line react-hooks/set-state-in-effect
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
    <main className="relative flex min-h-screen items-center justify-center overflow-hidden bg-background px-4 py-10">
      <div
        aria-hidden="true"
        className="bg-brand-glow pointer-events-none absolute inset-x-0 top-0 h-80"
      />
      <div className="relative flex w-full flex-col items-center">
        <div className="mb-8 flex items-center justify-center gap-2.5 sm:absolute sm:inset-x-0 sm:bottom-full sm:mb-10">
          <span aria-hidden="true">
            <LogoIcon size={40} className="cursor-default" />
          </span>
          <span className="text-lg font-semibold tracking-tight text-foreground">
            Metapus
          </span>
        </div>
        {children}
      </div>
    </main>
  )
}
