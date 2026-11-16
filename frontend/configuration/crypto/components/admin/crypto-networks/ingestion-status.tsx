"use client"

import { useEffect, useState } from "react"
import { Check, Copy } from "lucide-react"
import { toast } from "sonner"

import { cn } from "@/lib/utils"
import type { CryptoIngestionStream, CryptoNetworkState } from "@/configuration/crypto/types/crypto-network"
import { Badge, type BadgeProps } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"

import { formatTimestamp, isRuntimeFresh } from "./types-and-helpers"

type Variant = NonNullable<BadgeProps["variant"]>

// States and reasons come from the worker's ingestion evaluation, the same one
// the ingestion monitor alerts on. The page only labels them.
const ingestionStateLabels: Record<string, string> = {
  ready: "Синхронизирована",
  catching_up: "Догоняет",
  degraded: "Отстаёт",
  paused: "Пауза",
}

const ingestionStateVariants: Record<string, Variant> = {
  ready: "success",
  catching_up: "warning",
  degraded: "destructive",
  paused: "secondary",
}

const ingestionReasonLabels: Record<string, string> = {
  "stale head": "RPC отдаёт устаревшую голову цепи",
  "watcher lease inactive": "Ни один воркер не читает стрим",
  "provider head behind checkpoint": "RPC отстаёт от сохранённой отметки",
  "checkpoint stalled": "Отметка стрима не движется",
  backlog: "Догоняет по блокам",
  "timestamp backlog": "Догоняет по времени блоков",
  "missing checkpoint": "Стрим ещё не начат",
  "ingestion RPC coverage unavailable": "RPC не отдаёт голову стрима",
  "operator paused": "Остановлен оператором",
  "driver unavailable": "Драйвер сети недоступен",
  "ingestion capability unavailable": "Сеть не поддерживает оценку синхронизации",
}

const fixedStreamLabels: Record<string, string> = {
  "tron:solidified": "Подтверждённые блоки",
  "tron:latest": "Голова цепи (JIT)",
  "tron:shadow": "Теневой аудит",
  native_eth: "Нативный токен",
  trc20_primary: "TRC-20",
}

const numberFormat = new Intl.NumberFormat("ru-RU")

export function ingestionStateLabel(state: string): string {
  return ingestionStateLabels[state] ?? state
}

export function ingestionReasonLabel(reason?: string): string {
  if (!reason) return ""
  return ingestionReasonLabels[reason] ?? reason
}

export function streamLabel(key: string): string {
  if (fixedStreamLabels[key]) return fixedStreamLabels[key]
  if (key.startsWith("erc20:")) return `ERC-20 ${shortHash(key.slice("erc20:".length), 6, 4)}`
  return key
}

export function formatBlock(value?: number): string {
  return value === undefined ? "—" : numberFormat.format(value)
}

export function shortHash(value: string, head = 8, tail = 4): string {
  return value.length <= head + tail + 1 ? value : `${value.slice(0, head)}…${value.slice(-tail)}`
}

export function formatDuration(seconds: number): string {
  if (seconds < 0) return "—"
  if (seconds < 60) return `${seconds} с`
  if (seconds < 3600) return `${Math.floor(seconds / 60)} мин ${seconds % 60} с`
  return `${Math.floor(seconds / 3600)} ч ${Math.floor((seconds % 3600) / 60)} мин`
}

/** One measure for the header: blocks when the chain reports height, else time. */
export function formatShortLag(stream: CryptoIngestionStream): string {
  if (stream.lagBlocks >= 0) return `${numberFormat.format(stream.lagBlocks)} бл.`
  return stream.lagSeconds >= 0 ? formatDuration(stream.lagSeconds) : ""
}

export function formatLag(stream: CryptoIngestionStream): string {
  const parts: string[] = []
  if (stream.lagBlocks >= 0) parts.push(`${numberFormat.format(stream.lagBlocks)} бл.`)
  if (stream.lagSeconds >= 0) parts.push(formatDuration(stream.lagSeconds))
  return parts.length > 0 ? parts.join(" · ") : "—"
}

/** The stream that decides deposits: the first required one with a checkpoint. */
export function primaryStream(streams: CryptoIngestionStream[]): CryptoIngestionStream | undefined {
  return streams.find((s) => !s.optional && s.lastBlock !== undefined)
    ?? streams.find((s) => !s.optional)
    ?? streams[0]
}

type Summary =
  | { kind: "unwatched" }
  | { kind: "unknown"; reason: string }
  | { kind: "report"; state: string; reason: string; primary?: CryptoIngestionStream }

export function ingestionSummary(state: CryptoNetworkState): Summary {
  if (!state.watching) return { kind: "unwatched" }
  const ingestion = state.runtime.ingestion
  if (!ingestion) return { kind: "unknown", reason: "Воркер ещё не опубликовал состояние стримов" }
  if (!isRuntimeFresh(state.runtime.observedAt)) {
    return { kind: "unknown", reason: `Последние данные воркера от ${formatTimestamp(state.runtime.observedAt)}` }
  }
  const primary = primaryStream(ingestion.streams)
  return {
    kind: "report",
    state: ingestion.state,
    reason: ingestionReasonLabel(ingestion.reason || primary?.reason),
    primary,
  }
}

/** Compact sync state for the collapsed card header. */
export function IngestionPill({ state }: { state: CryptoNetworkState }) {
  const summary = ingestionSummary(state)
  if (summary.kind === "unwatched") return null
  if (summary.kind === "unknown") {
    return (
      <TooltipProvider delayDuration={200}>
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="outline" tabIndex={0} aria-label={`Синхронизация: нет данных. ${summary.reason}`}>
              Синхронизация: нет данных
            </Badge>
          </TooltipTrigger>
          <TooltipContent>{summary.reason}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    )
  }
  const { primary } = summary
  const behind = primary && summary.state !== "ready" && summary.state !== "paused"
  const lag = behind ? formatLag(primary) : ""
  const shortLag = behind ? formatShortLag(primary) : ""
  const label = ingestionStateLabel(summary.state)
  const detail = [
    primary ? `${streamLabel(primary.watcherKey)}: блок ${formatBlock(primary.lastBlock)}` : "",
    primary?.headHeight !== undefined ? `голова ${formatBlock(primary.headHeight)}` : "",
    lag ? `отставание ${lag}` : "",
    summary.reason,
  ].filter(Boolean).join(" · ")
  return (
    <TooltipProvider delayDuration={200}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Badge
            variant={ingestionStateVariants[summary.state] ?? "outline"}
            tabIndex={0}
            aria-label={`${label}. ${detail}`}
            className="gap-1.5"
          >
            <span>{label}</span>
            {primary?.lastBlock !== undefined ? (
              <span className="font-mono tabular-nums">#{formatBlock(primary.lastBlock)}</span>
            ) : null}
            {shortLag ? <span className="font-mono tabular-nums opacity-90">−{shortLag}</span> : null}
          </Badge>
        </TooltipTrigger>
        <TooltipContent className="max-w-sm">{detail || label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

function CopyHash({ hash }: { hash: string }) {
  const [copied, setCopied] = useState(false)
  useEffect(() => {
    if (!copied) return
    const timer = window.setTimeout(() => setCopied(false), 1500)
    return () => window.clearTimeout(timer)
  }, [copied])
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(hash)
      setCopied(true)
    } catch {
      toast.error("Не удалось скопировать хеш")
    }
  }
  return (
    <span className="inline-flex items-center gap-1">
      <span className="font-mono" title={hash}>{shortHash(hash)}</span>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="size-6"
        onClick={copy}
        aria-label={copied ? "Хеш скопирован" : "Скопировать хеш блока"}
        title={copied ? "Скопировано" : "Скопировать хеш"}
      >
        {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
      </Button>
    </span>
  )
}

/** Per-stream sync table for the expanded card. */
export function IngestionStreamsSection({ state }: { state: CryptoNetworkState }) {
  const summary = ingestionSummary(state)
  const ingestion = state.runtime.ingestion
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <p className="text-sm font-medium">Синхронизация блоков</p>
          <p className="text-sm text-muted-foreground">
            Сохранённая отметка каждого стрима против головы цепи. Воркер обновляет оценку раз в 30 секунд
            {state.runtime.observedAt && ingestion ? `, последняя — ${formatTimestamp(state.runtime.observedAt)}` : ""}.
          </p>
        </div>
        {summary.kind === "report" ? (
          <Badge variant={ingestionStateVariants[summary.state] ?? "outline"}>{ingestionStateLabel(summary.state)}</Badge>
        ) : null}
      </div>
      {summary.kind === "unwatched" ? (
        <p className="text-sm text-muted-foreground">Сеть не читается: стримы стоят на последней отметке.</p>
      ) : !ingestion ? (
        <p className="text-sm text-muted-foreground">Воркер ещё не опубликовал состояние стримов.</p>
      ) : (
        <div className="overflow-x-auto rounded-md border" aria-label="Стримы синхронизации блоков">
          <Table className="text-xs">
            <TableHeader>
              <TableRow className="h-9">
                <TableHead>Стрим</TableHead>
                <TableHead className="whitespace-nowrap text-right">Блок / голова</TableHead>
                <TableHead>Хеш блока</TableHead>
                <TableHead className="text-right">Отставание</TableHead>
                <TableHead className="text-right">Прогресс</TableHead>
                <TableHead>Чтение</TableHead>
                <TableHead>Состояние</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {ingestion.streams.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="text-center text-muted-foreground">
                    {ingestionReasonLabel(ingestion.reason) || "Нет стримов"}
                  </TableCell>
                </TableRow>
              ) : ingestion.streams.map((stream) => (
                <TableRow key={stream.watcherKey} className="h-9">
                  <TableCell>
                    <div className="flex flex-col whitespace-nowrap">
                      <span className="font-medium">{streamLabel(stream.watcherKey)}</span>
                      <span className="text-[11px] text-muted-foreground">
                        <span className="font-mono">{stream.watcherKey}</span>
                        {stream.optional ? " · необязательный" : ""}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-right font-mono tabular-nums">
                    <div className="flex flex-col">
                      <span>{formatBlock(stream.lastBlock)}</span>
                      <span className="text-[11px] text-muted-foreground">{formatBlock(stream.headHeight)}</span>
                    </div>
                  </TableCell>
                  <TableCell className="whitespace-nowrap">{stream.blockHash ? <CopyHash hash={stream.blockHash} /> : <span className="text-muted-foreground">—</span>}</TableCell>
                  <TableCell className="whitespace-nowrap text-right font-mono tabular-nums">{formatLag(stream)}</TableCell>
                  <TableCell
                    className="whitespace-nowrap text-right font-mono tabular-nums"
                    title="Сколько прошло с последнего сдвига отметки на момент оценки"
                  >
                    {stream.progressAgeSeconds >= 0 ? `${formatDuration(stream.progressAgeSeconds)} назад` : "—"}
                  </TableCell>
                  <TableCell className={cn("whitespace-nowrap", !stream.leaseActive && "text-destructive")}>
                    {stream.leaseActive ? "Идёт" : "Нет воркера"}
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap items-center gap-1.5">
                      <Badge variant={ingestionStateVariants[stream.state] ?? "outline"}>{ingestionStateLabel(stream.state)}</Badge>
                      {stream.reason ? <span className="text-muted-foreground">{ingestionReasonLabel(stream.reason)}</span> : null}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      {ingestion?.rpcErrorCode ? (
        <p className="text-xs text-muted-foreground">Код ошибки RPC при чтении головы: <span className="font-mono">{ingestion.rpcErrorCode}</span></p>
      ) : null}
    </div>
  )
}
