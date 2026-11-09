"use client"

/**
 * Admin page: list of merchants for user access management.
 * Route: /admin/merchants
 *
 * Operator navigates here to choose a merchant and manage its users.
 * Merchants are loaded from the standard catalog API — only operators
 * (with JWT + RBAC) can reach this route.
 */

import { useEffect, useState, useCallback } from "react"
import { useRouter } from "next/navigation"
import { apiFetch } from "@/lib/api"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Store, Search, ChevronRight, Users } from "lucide-react"
import { toast } from "sonner"

interface MerchantListItem {
  id: string
  code: string
  name: string
  isActive: boolean
}

interface MerchantListResponse {
  items: MerchantListItem[]
  total: number
}

export default function AdminMerchantsPage() {
  const router = useRouter()
  const [merchants, setMerchants] = useState<MerchantListItem[]>([])
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState("")

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const resp = await apiFetch<MerchantListResponse>("/catalog/merchants?limit=200")
      setMerchants(resp.items ?? [])
    } catch {
      toast.error("Не удалось загрузить список мерчантов")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const filtered = merchants.filter((m) => {
    const q = search.toLowerCase()
    return (
      m.name.toLowerCase().includes(q) ||
      m.code.toLowerCase().includes(q)
    )
  })

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex items-center justify-between border-b px-6 py-4">
        <div>
          <h2 className="text-base font-semibold">Мерчанты</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            Управление пользователями и доступом
          </p>
        </div>
        <div className="relative w-64">
          <Search className="absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pl-9 h-8 text-sm"
            placeholder="Поиск по названию или коду…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
      </div>

      {/* List */}
      <ScrollArea className="flex-1">
        <div className="p-4 space-y-1">
          {loading ? (
            Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-16 w-full rounded-lg" />
            ))
          ) : filtered.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-muted-foreground gap-3">
              <Store className="h-8 w-8 opacity-40" />
              <p className="text-sm">
                {search ? "Ничего не найдено" : "Мерчанты не созданы"}
              </p>
            </div>
          ) : (
            filtered.map((merchant) => (
              <button
                key={merchant.id}
                onClick={() => router.push(`/admin/merchants/${merchant.id}/users`)}
                className="flex w-full items-center justify-between rounded-lg border bg-card px-4 py-3 text-left transition-colors hover:bg-accent hover:text-accent-foreground"
              >
                <div className="flex items-center gap-3 min-w-0">
                  <Store className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium truncate">{merchant.name}</span>
                      {!merchant.isActive && (
                        <Badge variant="secondary" className="text-[10px] shrink-0">
                          Неактивен
                        </Badge>
                      )}
                    </div>
                    <div className="text-xs text-muted-foreground font-mono mt-0.5">
                      {merchant.code}
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-2 text-muted-foreground shrink-0 ml-4">
                  <Users className="h-3.5 w-3.5" />
                  <ChevronRight className="h-4 w-4" />
                </div>
              </button>
            ))
          )}
        </div>
      </ScrollArea>
    </div>
  )
}
