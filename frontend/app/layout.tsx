import type { Metadata, Viewport } from "next"
import { Inter, Geist_Mono } from "next/font/google"
import { Toaster } from "sonner"
import "./globals.css"

const inter = Inter({
  subsets: ["latin", "cyrillic"],
  variable: "--font-inter",
})

const geistMono = Geist_Mono({
  subsets: ["latin"],
  variable: "--font-geist-mono",
})

export const metadata: Metadata = {
  title: "Metapus - ERP Platform",
  description: "Modern ERP platform for business management",
}

export const viewport: Viewport = {
  themeColor: "#EAB308",
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
    var theme = iface.theme;
    if (theme === 'dark') {
      document.documentElement.classList.add('dark');
    } else if (theme === 'system') {
      if (window.matchMedia('(prefers-color-scheme: dark)').matches) {
        document.documentElement.classList.add('dark');
      }
    }
    var accent = iface.accentColor;
    if (accent && accent !== 'yellow') {
      document.documentElement.setAttribute('data-accent', accent);
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
      <body className={`${inter.variable} ${geistMono.variable} font-sans antialiased`}>
        {children}
        <Toaster richColors position="top-right" />
      </body>
    </html>
  )
}
