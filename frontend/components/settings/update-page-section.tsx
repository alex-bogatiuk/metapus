"use client"

import { UpdateSection } from "@/components/settings/update-section"

/**
 * UpdatePageSection wraps UpdateSection with the updater URL from env.
 * Rendered as a section in the Settings page sidebar.
 */
export function UpdatePageSection() {
  // In production, the updater URL comes from the env variable.
  // In dev, fall back to localhost:9090.
  const updaterUrl =
    process.env.NEXT_PUBLIC_UPDATER_URL || "http://localhost:9090"

  return <UpdateSection updaterUrl={updaterUrl} />
}
