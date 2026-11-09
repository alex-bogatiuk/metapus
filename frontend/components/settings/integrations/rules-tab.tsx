"use client"

import { useRouter } from "next/navigation"
import Link from "next/link"
import { Plus, Trash2, Send, Clock, FileText, Activity, List } from "lucide-react"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { AutomationRule } from "@/types/automation"
import { getActionLabel } from "@/lib/automation-rule-form"
import { cronToConfig, getScheduleDescription } from "@/lib/cron-config"
import { useMetadataStore } from "@/stores/useMetadataStore"

import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
  Table, TableBody, TableCell,
  TableHead, TableHeader, TableRow,
} from "@/components/ui/table"

const TRIGGER_ICONS = { entity_event: FileText, business_event: Activity, scheduled: Clock } as const

function formatTriggerLabel(rule: AutomationRule): string {
  if (rule.triggerType === "scheduled" && rule.eventType.startsWith("cron:")) {
    const expr = rule.eventType.slice(5)
    const config = cronToConfig(expr)
    return getScheduleDescription(config)
  }

  const actionLabel = getActionLabel(rule.eventType)

  if (rule.triggerType === "entity_event") {
    const entities = rule.targetEntities ?? []
    if (entities.length === 0) {
      return `${actionLabel} → Любая сущность`
    }
    const store = useMetadataStore.getState()
    const names = entities.map(k => store.getEntity(k)?.presentation.singular ?? k)
    return `${actionLabel} → ${names.join(", ")}`
  }

  return actionLabel
}

interface RulesTabProps {
  rules: AutomationRule[]
  onRefresh: () => Promise<void>
}

export function RulesTab({ rules, onRefresh }: RulesTabProps) {
  const router = useRouter()

  const handleDeleteRule = async (rule: AutomationRule) => {
    try {
      await api.automation.rules.delete(rule.id)
      toast.success("Правило удалено")
      onRefresh()
    } catch {
      toast.error("Не удалось удалить правило")
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <p className="text-sm text-muted-foreground">
          Правило определяет, при каком событии и куда отправлять уведомление
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" asChild>
            <Link href="/admin/automation-rules">
              <List data-icon="inline-start" />
              Список правил
            </Link>
          </Button>
          <Button size="sm" asChild>
            <Link href="/admin/automation-rules/new">
              <Plus data-icon="inline-start" />
              Создать правило
            </Link>
          </Button>
        </div>
      </div>

      {rules.length === 0 ? (
        <div className="rounded-lg border border-dashed p-10 text-center">
          <Send className="h-10 w-10 mx-auto text-muted-foreground/40 mb-3" />
          <p className="text-sm text-muted-foreground">
            Нет правил автоматизации.
          </p>
          <p className="text-xs text-muted-foreground mt-1">
            Правило определяет, при каком событии отправлять уведомление.
          </p>
        </div>
      ) : (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Наименование</TableHead>
                <TableHead>Триггер</TableHead>
                <TableHead>Подписчики</TableHead>
                <TableHead className="w-[80px]">Статус</TableHead>
                <TableHead className="w-[50px]" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {rules.map((rule) => {
                const TrigIcon = TRIGGER_ICONS[rule.triggerType as keyof typeof TRIGGER_ICONS] ?? FileText
                return (
                  <TableRow
                    key={rule.id}
                    className="cursor-pointer hover:bg-muted/50"
                    onClick={() => router.push(`/admin/automation-rules/${rule.id}`)}
                  >
                    <TableCell className="font-medium text-sm">{rule.name}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      <div className="flex items-center gap-1.5">
                        <TrigIcon className="h-3.5 w-3.5 shrink-0" />
                        <span className="truncate" title={formatTriggerLabel(rule)}>
                          {formatTriggerLabel(rule)}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {(rule.subscribers?.length || 0)} подп.
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={rule.isActive ? "default" : "secondary"}
                        className="text-[10px]"
                      >
                        {rule.isActive ? "Активно" : "Откл."}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7"
                        onClick={(e) => {
                          e.stopPropagation()
                          handleDeleteRule(rule)
                        }}
                      >
                        <Trash2 className="h-3.5 w-3.5 text-muted-foreground hover:text-destructive" />
                      </Button>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
