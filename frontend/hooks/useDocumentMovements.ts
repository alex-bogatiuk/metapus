import { useState, useEffect } from "react"
import type { DocumentMovementsResponse, DocumentMovement } from "@/types/common"

interface UseDocumentMovementsOptions {
    fetcher: (id: string) => Promise<DocumentMovementsResponse>
    documentId: string
    enabled?: boolean
}

export function useDocumentMovements({ fetcher, documentId, enabled = true }: UseDocumentMovementsOptions) {
    const [movements, setMovements] = useState<DocumentMovement[]>([])
    const [count, setCount] = useState<number>(0)
    const [loading, setLoading] = useState<boolean>(true)
    const [error, setError] = useState<string | null>(null)

    useEffect(() => {
        let isMounted = true

        void Promise.resolve().then(() => {
            if (!isMounted) return

            if (!enabled || !documentId) {
                setLoading(false)
                return
            }

            setLoading(true)
            setError(null)

            fetcher(documentId)
                .then((res) => {
                    if (isMounted) {
                        setMovements(res.movements || [])
                        setCount(res.count || 0)
                        setLoading(false)
                    }
                })
                .catch((err) => {
                    if (isMounted) {
                        setError(err instanceof Error ? err.message : "Ошибка загрузки движений")
                        setLoading(false)
                    }
                })
        })

        return () => {
            isMounted = false
        }
    }, [documentId, enabled, fetcher])

    return { movements, count, loading, error }
}
