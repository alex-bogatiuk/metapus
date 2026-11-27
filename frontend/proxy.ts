import { NextResponse, type NextRequest } from "next/server"

/**
 * Content-Security-Policy with a fresh nonce for every page. Next.js reads the
 * nonce from the request's CSP header and stamps it on its own scripts; the
 * root layout gives it to the inline theme script. 'strict-dynamic' lets those
 * scripts load the chunks (and Monaco's loader) they need, while an injected
 * <script> without the nonce never runs.
 */
function contentSecurityPolicy(nonce: string): string {
  const dev = process.env.NODE_ENV === "development"
  return [
    // React's development build evaluates strings to rebuild call stacks.
    `script-src 'nonce-${nonce}' 'strict-dynamic' 'report-sample'${dev ? " 'unsafe-eval'" : ""}`,
    // Monaco runs its language services in blob: workers.
    "worker-src 'self' blob:",
    "object-src 'none'",
    "base-uri 'none'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ].join("; ")
}

export function proxy(request: NextRequest) {
  const nonce = btoa(String.fromCharCode(...crypto.getRandomValues(new Uint8Array(16))))
  const policy = contentSecurityPolicy(nonce)

  const requestHeaders = new Headers(request.headers)
  requestHeaders.set("x-nonce", nonce)
  requestHeaders.set("Content-Security-Policy", policy)

  const response = NextResponse.next({ request: { headers: requestHeaders } })
  response.headers.set("Content-Security-Policy", policy)
  return response
}

export const config = {
  matcher: [
    {
      // Pages only: static files and the backend routes rewritten in next.config.mjs carry no HTML.
      source: "/((?!api/|portal/v1|merchant/|_next/static|_next/image|fonts/|favicon.ico).*)",
      // A prefetch is not rendered as a document, so it needs no nonce.
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
  ],
}
