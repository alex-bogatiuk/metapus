"use client"

import { Button } from "@/components/ui/button"

export function SessionRecovery({ onRetry }: { onRetry: () => void }) {
  return (
    <main className="flex min-h-svh items-center justify-center bg-background p-6">
      <div role="alert" className="max-w-sm space-y-4 text-center">
        <h1 className="text-lg font-semibold">Не удалось восстановить сессию</h1>
        <p className="text-sm text-muted-foreground">
          Сервер временно недоступен или соединение прервалось. Попробуйте ещё раз.
        </p>
        <Button onClick={onRetry}>Повторить попытку</Button>
      </div>
    </main>
  )
}
