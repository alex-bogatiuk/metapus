"use client"

import { getSessionGeneration, isCurrentSession } from "@/lib/auth-scope"
import { useEffect, useState } from "react"
import { ApiError, bootstrapAccessToken } from "@/lib/api"
import { useAuthStore } from "@/stores/useAuthStore"

interface AuthSessionBootstrapOptions {
  hydrated: boolean
  isAuthenticated: boolean
  accessToken?: string
}

export function useAuthSessionBootstrap({
  hydrated,
  isAuthenticated,
  accessToken,
}: AuthSessionBootstrapOptions) {
  const sessionGeneration = useAuthStore((s) => s.generation)
  const logout = useAuthStore((s) => s.logout)
  const [failure, setFailure] = useState<{ generation: number; attempt: number } | null>(null)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    if (!hydrated || !isAuthenticated || accessToken) return

    const generation = getSessionGeneration()
    let cancelled = false
    let retryTimer: ReturnType<typeof setTimeout> | undefined

    const restore = (retryCount: number) => {
      bootstrapAccessToken()
        .then((tokens) => {
          if (!cancelled && isCurrentSession(generation) && !tokens?.accessToken) logout()
        })
        .catch((error: Error) => {
          if (cancelled || !isCurrentSession(generation)) return
          const temporary = !(error instanceof ApiError) || error.status === 429 || error.status >= 500
          if (temporary && retryCount < 2) {
            retryTimer = setTimeout(() => restore(retryCount + 1), 1000 * 2 ** retryCount)
          } else {
            setFailure({ generation, attempt })
          }
        })
    }
    restore(0)

    return () => {
      cancelled = true
      clearTimeout(retryTimer)
    }
  }, [accessToken, attempt, hydrated, isAuthenticated, logout, sessionGeneration])

  const ready = hydrated && (!isAuthenticated || Boolean(accessToken))
  return {
    ready,
    error: !ready && failure?.generation === sessionGeneration && failure?.attempt === attempt,
    retry: () => setAttempt(value => value + 1),
  }
}
