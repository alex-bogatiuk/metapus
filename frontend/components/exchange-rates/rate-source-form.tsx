"use client"
import { useEffect, useState } from "react"
import useSWR from "swr"
import AutoForm from "@/components/shared/auto-form"
import { ReferenceField } from "@/components/shared/reference-field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table"
import { usePermissions } from "@/hooks/use-permissions"
import { useTabDirty } from "@/hooks/useTabDirty"
import { ApiError } from "@/lib/api"
import { exchangeRatesApi, rateTime } from "@/lib/exchange-rates"
import type { EntityFormProps } from "@/lib/entity-registry"
import type { RateMapping } from "@/types/exchange-rates"

export default function RateSourceForm({ id, entityName }: EntityFormProps) {
  const { has } = usePermissions()
  const canRead = has("catalog:rate_source:read")
  const canWrite = has("catalog:rate_source:update")
  return (
    <AutoForm
      id={id}
      entityName={entityName}
      entityType="catalog"
      routePrefix="rate-sources"
      hiddenFields={["baseUrl", "priority", "apiKey"]}
      readOnly={!canWrite}
      appendContent={
        <div className="space-y-4">
          <p className="text-sm text-muted-foreground">
            CoinGecko Demo · Оценка в USD · Получение каждые 5 минут · Актуальность 15 минут.
            Доступ к API задаётся пулом аккаунтов интеграции. Изменения применяются без перезапуска.
          </p>
          {id && canRead ? (
            <MappingsEditor sourceID={id} canWrite={canWrite} />
          ) : (
            <p className="text-sm">Сохраните источник, чтобы настроить соответствия.</p>
          )}
        </div>
      }
    />
  )
}
type Draft = {
  currencyID: string
  currencyName: string
  externalID: string
  active: boolean
  expectedVersion: number
}
function MappingsEditor({ sourceID, canWrite }: { sourceID: string; canWrite: boolean }) {
  const mappings = useSWR(["rate-mappings", sourceID], () => exchangeRatesApi.mappings(sourceID))
  const feed = useSWR(["rate-feed", sourceID], () => exchangeRatesApi.feedState(sourceID), {
    refreshInterval: 30000
  })
  const [draft, setDraft] = useState<Draft | null>(null)
  const [dirty, setDirty] = useState(false)
  const [wasDirty, setWasDirty] = useState(false)
  const [confirmClose, setConfirmClose] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { isDirty, markDirty, markClean } = useTabDirty()
  useEffect(() => {
    if (!dirty) return
    const prevent = (event: BeforeUnloadEvent) => {
      event.preventDefault()
    }
    window.addEventListener("beforeunload", prevent)
    return () => window.removeEventListener("beforeunload", prevent)
  }, [dirty])
  const open = (mapping?: RateMapping) => {
    setWasDirty(isDirty)
    setDirty(false)
    setError(null)
    setDraft({
      currencyID: mapping?.currency.id ?? "",
      currencyName: mapping?.currency.name ?? "",
      externalID: mapping?.externalId ?? "",
      active: mapping?.isActive ?? true,
      expectedVersion: mapping?.version ?? 0
    })
  }
  const change = (patch: Partial<Draft>) => {
    setDraft((current) => (current ? { ...current, ...patch } : current))
    setDirty(true)
    markDirty()
  }
  const close = () => {
    setDraft(null)
    setDirty(false)
    setConfirmClose(false)
    if (!wasDirty) markClean()
  }
  const save = async () => {
    if (!draft || !mappings.data) return
    setSaving(true)
    setError(null)
    try {
      await exchangeRatesApi.putMapping(sourceID, draft.currencyID, mappings.data.baseCurrency.id, {
        externalId: draft.externalID.trim(),
        isActive: draft.active,
        expectedVersion: draft.expectedVersion
      })
      await mappings.mutate()
      close()
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        const latest = await mappings.mutate().catch(() => undefined)
        const version = latest?.items.find((item) => item.currency.id === draft.currencyID)?.version
        if (version !== undefined)
          setDraft((current) => (current ? { ...current, expectedVersion: version } : null))
        setError(
          `Соответствие изменено другим пользователем. Введённые данные сохранены.${version !== undefined ? ` Повторное сохранение применит их к версии ${version}.` : " Закройте форму и перечитайте список перед повторной попыткой."}`
        )
      } else setError(err instanceof Error ? err.message : "Не удалось сохранить соответствие")
    } finally {
      setSaving(false)
    }
  }
  const feedLabels: Record<string, string> = {
    idle: "Ожидает запуска",
    running: "Получение курсов",
    succeeded: "Загрузка завершена",
    partial: "Получена часть курсов",
    failed: "Ошибка загрузки"
  }
  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-lg font-medium">Соответствия валют</h2>
        {canWrite && (
          <Button type="button" variant="outline" disabled={!mappings.data} onClick={() => open()}>
            Добавить соответствие
          </Button>
        )}
      </div>
      {mappings.error && <p role="alert">Не удалось загрузить соответствия</p>}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Валюта</TableHead>
            <TableHead>CoinGecko ID</TableHead>
            <TableHead>Состояние</TableHead>
            <TableHead>Версия</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {mappings.data?.items.map((item) => (
            <TableRow key={item.currency.id}>
              <TableCell>
                {item.currency.code} → {item.baseCurrency.code}
              </TableCell>
              <TableCell>{item.externalId}</TableCell>
              <TableCell>{item.isActive ? "Включено" : "Выключено"}</TableCell>
              <TableCell>{item.version}</TableCell>
              <TableCell>
                {canWrite && (
                  <Button type="button" variant="ghost" onClick={() => open(item)}>
                    Изменить
                  </Button>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {mappings.data?.items.length === 0 && (
        <p className="text-sm text-muted-foreground">Соответствия ещё не настроены</p>
      )}
      <div className="space-y-2 rounded-md border p-4 text-sm">
        <h3 className="font-medium">Периодическая загрузка</h3>
        {feed.error && <p role="alert">Не удалось получить состояние загрузки</p>}
        {feed.data && (
          <>
            <p>{feedLabels[feed.data.status] ?? feed.data.status}</p>
            <p>
              Последняя попытка: {rateTime(feed.data.lastAttemptAt)} · Последний пригодный
              результат: {rateTime(feed.data.lastSuccessAt)}
            </p>
            <p>
              Запрошено: {feed.data.requested} · Пригодно: {feed.data.received} · Новых:{" "}
              {feed.data.inserted} · Повторов: {feed.data.repeated} · Отклонено:{" "}
              {feed.data.rejected}
            </p>
            {feed.data.problems.map((problem, index) => (
              <p key={index} role="status" className="text-amber-700 dark:text-amber-400">
                {problem.message}
              </p>
            ))}
          </>
        )}
      </div>
      <Dialog
        open={draft !== null}
        onOpenChange={(value) => {
          if (!value && !saving) {
            if (dirty) setConfirmClose(true)
            else close()
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Соответствие CoinGecko</DialogTitle>
            <DialogDescription>
              Валюта пересчёта: {mappings.data?.baseCurrency.code}. Изменение соответствия потребует
              нового получения курса.
            </DialogDescription>
          </DialogHeader>
          {draft && (
            <form
              className="space-y-4"
              onSubmit={(event) => {
                event.preventDefault()
                void save()
              }}
            >
              <div className="space-y-2">
                <Label>Валюта</Label>
                <ReferenceField
                  value={draft.currencyID}
                  displayName={draft.currencyName}
                  apiEndpoint="/catalog/currencies"
                  disabled={draft.expectedVersion > 0 || saving}
                  onChange={(currencyID, currencyName) => change({ currencyID, currencyName })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="coingecko-id">CoinGecko ID</Label>
                <Input
                  id="coingecko-id"
                  required
                  maxLength={100}
                  pattern="[a-z0-9][a-z0-9._\-]*"
                  placeholder="Например, tether"
                  value={draft.externalID}
                  disabled={saving}
                  onChange={(event) => change({ externalID: event.target.value })}
                />
              </div>
              <div className="flex items-center gap-3">
                <Switch
                  id="mapping-active"
                  checked={draft.active}
                  disabled={saving}
                  onCheckedChange={(active) => change({ active })}
                />
                <Label htmlFor="mapping-active">Использовать соответствие</Label>
              </div>
              {error && (
                <p role="alert" className="text-sm text-destructive">
                  {error}
                </p>
              )}
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  disabled={saving}
                  onClick={() => (dirty ? setConfirmClose(true) : close())}
                >
                  Отмена
                </Button>
                <Button type="submit" disabled={saving || !draft.currencyID || !dirty}>
                  {saving ? "Сохранение…" : "Сохранить соответствие"}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
      <AlertDialog open={confirmClose} onOpenChange={setConfirmClose}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Закрыть без сохранения?</AlertDialogTitle>
            <AlertDialogDescription>Изменения соответствия будут потеряны.</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Продолжить редактирование</AlertDialogCancel>
            <AlertDialogAction onClick={close}>Закрыть без сохранения</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  )
}
