"use client"

import Link from "next/link"
import { Home } from "lucide-react"
import { Button } from "@/components/ui/button"
import { ImpersonationBanner } from "@/components/layout/impersonation-banner"

export default function NotFound() {
  return (
    <>
      <ImpersonationBanner />
      <div className="flex min-h-screen flex-col items-center justify-center gap-4 px-4 text-center">
        <h1 className="text-6xl font-bold text-muted-foreground/40">404</h1>
        <p className="text-lg text-muted-foreground">Страница не найдена</p>
        <p className="text-sm text-muted-foreground/70 max-w-md">
          Запрошенная страница не существует или у вас нет доступа к ней.
        </p>
        <Button asChild variant="outline" className="mt-2">
          <Link href="/">
            <Home className="mr-2 h-4 w-4" />
            На главную
          </Link>
        </Button>
      </div>
    </>
  )
}
