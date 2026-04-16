"use client"

import { useEffect, useState } from "react"
import { Plus, Settings2, Trash2 } from "lucide-react"
import { api } from "@/lib/api"
import type { ServiceAccount } from "@/types/service-account"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"

export function IntegrationsSection() {
  const [accounts, setAccounts] = useState<ServiceAccount[]>([])
  const [loading, setLoading] = useState(true)

  const fetchAccounts = async () => {
    try {
      const data = await api.system.serviceAccounts.list()
      setAccounts(data)
    } catch (e) {
      console.error(e)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchAccounts()
  }, [])

  if (loading) return <div className="text-sm text-muted-foreground py-4">Загрузка...</div>

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <p className="text-sm text-muted-foreground">
          Настроенные каналы связи и сервисные аккаунты для системы автоматизаций.
        </p>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="secondary" onClick={() => window.location.href = '/settings/automation-rules'}>
            Настройка правил автоматизации
          </Button>
          <Button size="sm">
            <Plus className="w-4 h-4 mr-2" />
            Добавить аккаунт (Stub)
          </Button>
        </div>
      </div>

      <div className="rounded-md border">
        {accounts.length === 0 ? (
          <div className="p-8 text-center text-sm text-muted-foreground">
            Нет настроенных интеграций
          </div>
        ) : (
          <div className="divide-y">
            {accounts.map((acc) => (
              <div
                key={acc.id}
                className="p-4 flex items-center justify-between hover:bg-muted/30"
              >
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-sm">{acc.name}</span>
                    <Badge variant={acc.status === "active" ? "default" : "destructive"}>
                      {acc.status}
                    </Badge>
                    {acc.isDefault && <Badge variant="outline">По-умолчанию</Badge>}
                  </div>
                  <div className="text-xs text-muted-foreground">Тип: {acc.accountType}</div>
                </div>
                <div className="flex items-center gap-1">
                  <Button variant="ghost" size="icon" title="Настроить">
                    <Settings2 className="w-4 h-4 text-muted-foreground" />
                  </Button>
                  <Button variant="ghost" size="icon" title="Удалить">
                    <Trash2 className="w-4 h-4 text-muted-foreground hover:text-destructive" />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
