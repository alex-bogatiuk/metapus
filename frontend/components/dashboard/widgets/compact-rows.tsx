"use client"
import { createContext, useContext, useEffect } from "react"

type Report = (rows: number | null) => void

const CompactRowsContext = createContext<Report>(() => {})

/** Set by the widget wrapper: forwards a widget's request to the grid under its instance id. */
export const CompactRowsProvider = CompactRowsContext.Provider

/**
 * A widget that has nothing to act on (an empty queue, no tokens yet) asks the grid for fewer rows,
 * so the dashboard does not keep tall empty cards. `null` gives back the configured height.
 * The grid decides whether the row can actually shrink without leaving a gap.
 */
export function useCompactRows(rows: number | null) {
  const report = useContext(CompactRowsContext)
  useEffect(() => {
    report(rows)
    return () => report(null)
  }, [report, rows])
}
