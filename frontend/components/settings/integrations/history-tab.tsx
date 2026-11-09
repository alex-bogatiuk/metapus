"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import {
  Clock, RefreshCw, Copy, Loader2, CheckCircle2, XCircle, Send,
} from "lucide-react"
import { format } from "date-fns"
import { ru } from "date-fns/locale"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { AutomationRule, AutomationChannel, AutomationHistoryEntry, HistoryStatsResponse } from "@/types/automation"
import { HISTORY_STATUS_MAP } from "@/lib/automation-helpers"
import { getActionLabel } from "@/lib/automation-rule-form"

import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Label } from "@/components/ui/label"
import {
  Dialog, DialogContent, DialogDescription,
  DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import {
  Select, SelectContent, SelectItem,
  SelectTrigger, SelectValue,
} from "@/components/ui/select"
import {
  Table, TableBody, TableCell,
  TableHead, TableHeader, TableRow,
} from "@/components/ui/table"
import {
  Tooltip, TooltipContent, TooltipTrigger,
} from "@/components/ui/tooltip"
import { ScrollArea } from "@/components/ui/scroll-area"
import { ScrollSentinel } from "@/components/shared/scroll-sentinel"

interface HistoryTabProps {
  rules: AutomationRule[]
  channels: AutomationChannel[]
}

export function HistoryTab({ rules, channels }: HistoryTabProps) {
  const [history, setHistory] = useState<AutomationHistoryEntry[]>([])
  const [historyStats, setHistoryStats] = useState<HistoryStatsResponse | null>(null)
  const [historyStatusFilter, setHistoryStatusFilter] = useState<string>("")
  const [historyRuleFilter, setHistoryRuleFilter] = useState<string>("")
  const [historyChannelFilter, setHistoryChannelFilter] = useState<string>("")
  const [historyHasMore, setHistoryHasMore] = useState(false)
  const [historyLoading, setHistoryLoading] = useState(false)
  const [historyLoadingMore, setHistoryLoadingMore] = useState(false)
  const historyOffsetRef = useRef(0)
  const historyScrollRef = useRef<HTMLDivElement>(null)

  // Dialog state
  const [selectedHistory, setSelectedHistory] = useState<AutomationHistoryEntry | null>(null)
  const [isReplaying, setIsReplaying] = useState(false)

  const fetchHistory = useCallback(async () => {
    setHistoryLoading(true)
    historyOffsetRef.current = 0
    try {
      const params: Record<string, string | number> = { limit: 50, offset: 0 }
      if (historyStatusFilter) params.status = historyStatusFilter
      if (historyRuleFilter) params.ruleId = historyRuleFilter
      if (historyChannelFilter) params.channelId = historyChannelFilter
      const resp = await api.automation.history.list(params as Parameters<typeof api.automation.history.list>[0])
      setHistory(resp.items ?? [])
      setHistoryHasMore((resp.items?.length ?? 0) >= 50)
    } catch (e) {
      console.error(e)
    } finally {
      setHistoryLoading(false)
    }
  }, [historyStatusFilter, historyRuleFilter, historyChannelFilter])

  const loadMoreHistory = useCallback(async () => {
    if (historyLoadingMore) return
    setHistoryLoadingMore(true)
    const nextOffset = historyOffsetRef.current + 50
    try {
      const params: Record<string, string | number> = { limit: 50, offset: nextOffset }
      if (historyStatusFilter) params.status = historyStatusFilter
      if (historyRuleFilter) params.ruleId = historyRuleFilter
      if (historyChannelFilter) params.channelId = historyChannelFilter
      const resp = await api.automation.history.list(params as Parameters<typeof api.automation.history.list>[0])
      setHistory(prev => {
        const existingIds = new Set(prev.map(i => i.id))
        const newItems = (resp.items ?? []).filter(i => !existingIds.has(i.id))
        return [...prev, ...newItems]
      })
      setHistoryHasMore((resp.items?.length ?? 0) >= 50)
      historyOffsetRef.current = nextOffset
    } catch (e) {
      console.error(e)
    } finally {
      setHistoryLoadingMore(false)
    }
  }, [historyStatusFilter, historyRuleFilter, historyChannelFilter, historyLoadingMore])

  const fetchHistoryStats = useCallback(async () => {
    try {
      const params: Record<string, string> = {}
      if (historyRuleFilter) params.ruleId = historyRuleFilter
      if (historyChannelFilter) params.channelId = historyChannelFilter
      const stats = await api.automation.history.stats(params)
      setHistoryStats(stats)
    } catch (e) {
      console.error(e)
    }
  }, [historyRuleFilter, historyChannelFilter])

  const handleBatchReplay = async () => {
    try {
      const params: Record<string, string> = {}
      if (historyRuleFilter) params.ruleId = historyRuleFilter
      if (historyChannelFilter) params.channelId = historyChannelFilter
      const result = await api.automation.history.batchReplay(params)
      toast.success(`${result.queued} задач поставлено в очередь`)
      fetchHistory()
      fetchHistoryStats()
    } catch {
      toast.error("Не удалось выполнить повторную отправку")
    }
  }

  const handleReplay = async (id: string) => {
    setIsReplaying(true)
    try {
      await api.automation.history.replay(id)
      toast.success("Повторная отправка поставлена в очередь")
      setSelectedHistory(null)
      fetchHistory()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Не удалось отправить сообщение")
    } finally {
      setIsReplaying(false)
    }
  }

  useEffect(() => {
    fetchHistory()
    fetchHistoryStats()
  }, [fetchHistory, fetchHistoryStats])

  return (
    <div className="flex flex-col gap-3 min-h-0">
      {/* ── Stats Cards ────────────────────────────────── */}
      <div className="grid grid-cols-4 gap-3">
        {([
          { key: "", label: "Всего", count: historyStats?.total ?? 0, color: "text-foreground" },
          { key: "success", label: "Выполнено", count: historyStats?.byStatus?.success ?? 0, color: "text-green-600 dark:text-green-400" },
          { key: "error", label: "Ошибки", count: historyStats?.byStatus?.error ?? 0, color: "text-red-600 dark:text-red-400" },
          { key: "skipped", label: "Пропущено", count: (historyStats?.byStatus?.skipped ?? 0) + (historyStats?.byStatus?.condition_false ?? 0), color: "text-muted-foreground" },
        ] as const).map((card) => (
          <button
            key={card.key}
            className={`rounded-lg border p-3 text-left transition-colors hover:bg-muted/50 ${historyStatusFilter === card.key ? "ring-2 ring-primary" : ""}`}
            onClick={() => setHistoryStatusFilter(historyStatusFilter === card.key ? "" : card.key)}
          >
            <p className="text-xs text-muted-foreground">{card.label}</p>
            <p className={`text-2xl font-bold tabular-nums ${card.color}`}>{card.count.toLocaleString("ru-RU")}</p>
          </button>
        ))}
      </div>

      {/* ── Filters Row ────────────────────────────────── */}
      <div className="flex items-center gap-2 flex-wrap">
        <Select value={historyStatusFilter || "all"} onValueChange={(v) => setHistoryStatusFilter(v === "all" ? "" : v)}>
          <SelectTrigger className="w-[150px] h-9">
            <SelectValue placeholder="Статус" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Все статусы</SelectItem>
            <SelectItem value="success">Выполнено</SelectItem>
            <SelectItem value="error">Ошибка</SelectItem>
            <SelectItem value="condition_false">Условие не выполнено</SelectItem>
            <SelectItem value="skipped">Пропущено</SelectItem>
            <SelectItem value="pending">Ожидание</SelectItem>
          </SelectContent>
        </Select>

        <Select value={historyRuleFilter || "all"} onValueChange={(v) => setHistoryRuleFilter(v === "all" ? "" : v)}>
          <SelectTrigger className="w-[200px] h-9">
            <SelectValue placeholder="Правило" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Все правила</SelectItem>
            {rules.map((r) => (
              <SelectItem key={r.id} value={r.id}>{r.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={historyChannelFilter || "all"} onValueChange={(v) => setHistoryChannelFilter(v === "all" ? "" : v)}>
          <SelectTrigger className="w-[180px] h-9">
            <SelectValue placeholder="Канал" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Все каналы</SelectItem>
            {channels.map((ch) => (
              <SelectItem key={ch.id} value={ch.id}>{ch.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        <div className="flex-1" />

        {historyStatusFilter === "error" && (historyStats?.byStatus?.error ?? 0) > 0 && (
          <Button variant="outline" size="sm" className="h-9 gap-1.5 text-destructive" onClick={handleBatchReplay}>
            <RefreshCw className="h-3.5 w-3.5" />
            Повторить все ошибки ({historyStats?.byStatus?.error ?? 0})
          </Button>
        )}

        <Button variant="outline" size="sm" className="h-9" onClick={() => { fetchHistory(); fetchHistoryStats() }}>
          <RefreshCw className={`h-3.5 w-3.5 mr-1.5 ${historyLoading ? "animate-spin" : ""}`} />
          Обновить
        </Button>
      </div>

      {/* ── Scrollable Table ──────────────────────────── */}
      <ScrollArea className="h-[500px] border rounded-lg overflow-hidden" viewportRef={historyScrollRef}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[145px]">Время</TableHead>
              <TableHead>Правило</TableHead>
              <TableHead className="w-[120px]">Канал</TableHead>
              <TableHead className="w-[120px]">Событие</TableHead>
              <TableHead className="w-[95px]">Результат</TableHead>
              <TableHead>Ошибка</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {historyLoading ? (
              <TableRow>
                <TableCell colSpan={6} className="text-center py-12">
                  <Loader2 className="h-6 w-6 animate-spin mx-auto text-muted-foreground" />
                </TableCell>
              </TableRow>
            ) : history.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="py-16">
                  <div className="flex flex-col items-center gap-2 text-muted-foreground">
                    <Clock className="h-10 w-10 opacity-40" />
                    <p className="text-sm">
                      {historyStatusFilter || historyRuleFilter || historyChannelFilter
                        ? "Нет записей по заданным фильтрам"
                        : "Журнал отправок пуст"}
                    </p>
                    {(historyStatusFilter || historyRuleFilter || historyChannelFilter) && (
                      <Button variant="outline" size="sm" onClick={() => {
                        setHistoryStatusFilter("")
                        setHistoryRuleFilter("")
                        setHistoryChannelFilter("")
                      }}>
                        Сбросить фильтры
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ) : (
              history.map((item) => {
                const st = HISTORY_STATUS_MAP[item.status] ?? HISTORY_STATUS_MAP.pending
                const isError = item.status === "error"
                return (
                  <TableRow
                    key={item.id}
                    className={`cursor-pointer hover:bg-muted/50 ${isError ? "border-l-2 border-l-red-500 bg-red-50/30 dark:bg-red-950/10" : ""}`}
                    onClick={() => setSelectedHistory(item)}
                  >
                    <TableCell className="font-mono text-xs">
                      {format(new Date(item.createdAt), "dd.MM.yy HH:mm:ss", { locale: ru })}
                    </TableCell>
                    <TableCell className="text-sm">
                      {item.ruleName || item.ruleId.substring(0, 8) + "…"}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {item.channelName || "—"}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {getActionLabel(item.eventType)}
                    </TableCell>
                    <TableCell>
                      <Badge variant={st.variant as "default" | "destructive" | "secondary" | "outline"} className="text-[10px]">
                        {item.status === "success" && <CheckCircle2 className="h-3 w-3 mr-1" />}
                        {isError && <XCircle className="h-3 w-3 mr-1" />}
                        {st.label}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground max-w-[200px] truncate">
                      {item.errorText ? (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span className="text-destructive cursor-help">{item.errorText}</span>
                          </TooltipTrigger>
                          <TooltipContent side="bottom" className="max-w-sm">
                            <p className="text-xs break-all">{item.errorText}</p>
                          </TooltipContent>
                        </Tooltip>
                      ) : (
                        "—"
                      )}
                    </TableCell>
                  </TableRow>
                )
              })
            )}
          </TableBody>
        </Table>
        {/* Infinite scroll sentinel */}
        <ScrollSentinel
          onIntersect={loadMoreHistory}
          loading={historyLoadingMore}
          enabled={historyHasMore && !historyLoading}
          scrollContainer={historyScrollRef}
        />
      </ScrollArea>

      {/* History Item Detail Dialog */}
      <Dialog open={selectedHistory !== null} onOpenChange={(open) => !open && setSelectedHistory(null)}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Детали отправки</DialogTitle>
            <DialogDescription>
              Результат выполнения правила автоматизации
            </DialogDescription>
          </DialogHeader>

          {selectedHistory && (
            <div className="space-y-4 py-2">
              <div className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <span className="text-muted-foreground block text-xs">Правило</span>
                  <span className="font-medium">{selectedHistory.ruleName || selectedHistory.ruleId}</span>
                </div>
                <div>
                  <span className="text-muted-foreground block text-xs">Событие</span>
                  <span>{getActionLabel(selectedHistory.eventType)}</span>
                </div>
                <div>
                  <span className="text-muted-foreground block text-xs">Канал</span>
                  <span>{selectedHistory.channelName || "Внутренний"}</span>
                </div>
                <div>
                  <span className="text-muted-foreground block text-xs">Статус</span>
                  <Badge variant={HISTORY_STATUS_MAP[selectedHistory.status]?.variant ?? "outline"}>
                    {HISTORY_STATUS_MAP[selectedHistory.status]?.label ?? selectedHistory.status}
                  </Badge>
                </div>
                <div>
                  <span className="text-muted-foreground block text-xs">Время</span>
                  <span>{new Date(selectedHistory.createdAt).toLocaleString("ru-RU")}</span>
                </div>
                {selectedHistory.durationMs !== undefined && (
                  <div>
                    <span className="text-muted-foreground block text-xs">Длительность</span>
                    <span>{selectedHistory.durationMs} мс</span>
                  </div>
                )}
              </div>

              {selectedHistory.errorText && (
                <div className="space-y-2 pt-2">
                  <Label className="text-destructive font-semibold">Текст ошибки</Label>
                  <div className="relative group">
                    <div className="bg-destructive/10 text-destructive text-xs p-3 rounded-md font-mono whitespace-pre-wrap max-h-[150px] overflow-auto border border-destructive/20">
                      {selectedHistory.errorText}
                    </div>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="absolute top-1 right-1 h-6 w-6 opacity-0 group-hover:opacity-100 transition-opacity bg-background/50 hover:bg-background"
                      onClick={() => {
                        navigator.clipboard.writeText(selectedHistory.errorText!)
                        toast.success("Текст ошибки скопирован")
                      }}
                    >
                      <Copy className="h-3 w-3" />
                    </Button>
                  </div>
                </div>
              )}

              {selectedHistory.renderedPayload && (
                <div className="space-y-2 pt-2">
                  <Label>Текст сообщения (Payload)</Label>
                  <div className="relative group">
                    <div className="bg-muted text-xs p-3 rounded-md font-mono whitespace-pre-wrap max-h-[250px] overflow-auto border">
                      {selectedHistory.renderedPayload}
                    </div>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="absolute top-1 right-1 h-6 w-6 opacity-0 group-hover:opacity-100 transition-opacity bg-background/50 hover:bg-background"
                      onClick={() => {
                        navigator.clipboard.writeText(selectedHistory.renderedPayload!)
                        toast.success("Текст скопирован")
                      }}
                    >
                      <Copy className="h-3 w-3" />
                    </Button>
                  </div>
                </div>
              )}
            </div>
          )}

          <DialogFooter className="flex items-center justify-between sm:justify-between w-full">
            <Button variant="outline" onClick={() => setSelectedHistory(null)}>
              Закрыть
            </Button>
            <Button
              variant="default"
              disabled={isReplaying || (selectedHistory?.status === "success" && !selectedHistory?.errorText)}
              onClick={() => {
                if (selectedHistory) {
                  handleReplay(selectedHistory.id)
                }
              }}
            >
              {isReplaying ? (
                <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
              ) : (
                <Send className="mr-2 h-4 w-4" />
              )}
              Повторить отправку
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
