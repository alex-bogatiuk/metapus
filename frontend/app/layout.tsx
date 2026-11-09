import type { Metadata, Viewport } from "next"
import { Toaster } from "sonner"
import { StepUpDialog } from "@/components/auth/step-up-dialog"
import { ScopedDataProvider } from "@/components/scoped-data-provider"
import "./globals.css"

export const metadata: Metadata = {
  title: "Metapus - ERP Platform",
  description: "Modern ERP platform for business management",
}

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "hsl(220 23% 97%)" },
    { media: "(prefers-color-scheme: dark)", color: "hsl(218 19% 12%)" },
  ],
}

/**
 * Inline script that runs before React hydration to set the `dark` class
 * on <html> immediately. This prevents FOUC (flash of unstyled content).
 * It reads the Zustand-persisted store from localStorage.
 */
const themeScript = `
(function(){
  try {
    var raw = localStorage.getItem('metapus-user-prefs');
    if (!raw) return;
    var parsed = JSON.parse(raw);
    var iface = parsed && parsed.state && parsed.state.interface;
    if (!iface) return;
    var allowedThemes = ['light','dark','system'];
    var theme = iface.theme;
    if (allowedThemes.indexOf(theme) === -1) return;
    if (theme === 'dark') {
      document.documentElement.classList.add('dark');
    } else if (theme === 'system') {
      if (window.matchMedia('(prefers-color-scheme: dark)').matches) {
        document.documentElement.classList.add('dark');
      }
    }
    if (iface.compactMode === true) {
      document.documentElement.setAttribute('data-compact', '');
    }
  } catch(e) {}
})();
`

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="ru" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      <body className="font-sans antialiased">
        <ScopedDataProvider>{children}</ScopedDataProvider>
        <StepUpDialog />
        <Toaster richColors position="top-right" duration={1500} />
      </body>
    </html>
  )
}
