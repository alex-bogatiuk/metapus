import {
  evaluateExpression,
  isMathExpression,
  type CalcResult,
} from "@/lib/calc-engine"

export const COMMAND_DATA_SEARCH_PREFIX = ">"

export type CommandIntent =
  | { type: "empty" }
  | { type: "internal-url"; href: string; pathname: string }
  | { type: "external-url"; value: string }
  | { type: "invalid-url"; value: string }
  | { type: "data-search"; query: string }
  | { type: "calculator"; result: CalcResult }
  | { type: "fuzzy-search"; query: string }

const _urlSchemePattern = /^[a-z][a-z\d+.-]*:/i

/**
 * Resolves the user's input into one mutually exclusive Command Palette mode.
 * URL intent has the highest priority so pasted links never reach cmdk filtering,
 * the calculator, or the global data-search API.
 */
export function resolveCommandIntent(
  input: string,
  currentOrigin: string | null,
): CommandIntent {
  const value = input.trim()
  if (!value) return { type: "empty" }

  const looksLikeUrl = value.startsWith("/") || _urlSchemePattern.test(value)
  if (looksLikeUrl) {
    if (!currentOrigin) return { type: "invalid-url", value }

    try {
      const baseUrl = new URL(currentOrigin)
      const url = new URL(value, baseUrl)

      if (url.protocol !== "http:" && url.protocol !== "https:") {
        return { type: "invalid-url", value }
      }
      if (url.origin !== baseUrl.origin) {
        return { type: "external-url", value }
      }

      return {
        type: "internal-url",
        pathname: url.pathname,
        href: `${url.pathname}${url.search}${url.hash}`,
      }
    } catch {
      return { type: "invalid-url", value }
    }
  }

  if (value.startsWith(COMMAND_DATA_SEARCH_PREFIX)) {
    return {
      type: "data-search",
      query: value.slice(COMMAND_DATA_SEARCH_PREFIX.length).trim(),
    }
  }

  if (isMathExpression(value)) {
    const result = evaluateExpression(value)
    if (result) return { type: "calculator", result }
  }

  return { type: "fuzzy-search", query: value }
}

/** Stable tab identity excludes URL state stored in the query string or hash. */
export function getTabIdFromHref(href: string): string {
  return href.split(/[?#]/, 1)[0] || "/"
}
