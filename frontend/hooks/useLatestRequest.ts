"use client"

import { getSessionGeneration, isCurrentSession } from "@/lib/auth-scope"
import { useEffect, useMemo } from "react"

// A response may update state only while its request is the latest one in the
// mounted component and the authenticated session is unchanged.
export function useLatestRequest() {
  const requests = useMemo(() => {
    let version = 0
    let mounted = true
    const capture = () => {
      const current = version
      const session = getSessionGeneration()
      return () => mounted && version === current && isCurrentSession(session)
    }
    return {
      begin: () => {
        version++
        return capture()
      },
      capture,
      activate: () => { mounted = true },
      invalidate: () => { version++ },
      deactivate: () => { mounted = false; version++ },
    }
  }, [])

  useEffect(() => {
    requests.activate()
    return () => requests.deactivate()
  }, [requests])
  return requests
}
