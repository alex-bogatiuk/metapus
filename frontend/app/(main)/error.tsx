"use client"

import { useEffect } from "react"
import { Button } from "@/components/ui/button"

export default function GlobalError({
    error,
    reset,
}: {
    error: Error & { digest?: string }
    reset: () => void
}) {
    useEffect(() => {
        console.error("[GlobalError]", error)
    }, [error])

    return (
        <div className="flex h-full flex-col items-center justify-center gap-4 p-8">
            <div className="flex flex-col items-center gap-2 text-center">
                <div className="flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10">
                    <span className="text-2xl">⚠️</span>
                </div>
                <h2 className="text-lg font-semibold text-foreground">
                    Произошла ошибка
                </h2>
                <p className="max-w-md text-sm text-muted-foreground">
                    {error.message || "Что-то пошло не так. Попробуйте обновить страницу."}
                </p>
            </div>
            <Button variant="outline" onClick={() => reset()}>
                Попробовать снова
            </Button>
        </div>
    )
}
