"use client"

import { useRef, useState } from "react"
import Link from "next/link"
import { Loader2 } from "lucide-react"
import { integrationPools } from "@/lib/api/integrations"
import { useLatestRequest } from "@/hooks/useLatestRequest"
import { ReferenceField } from "@/components/shared/reference-field"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog"
import { Field, FieldLabel, FieldDescription, FieldError } from "@/components/ui/field"

interface AccountPoolDialogProps {
  accountId: string
  accountName: string
  provider: string
  providerLabel: string
  disabled: boolean
}

export function AccountPoolDialog({ accountId, accountName, provider, providerLabel, disabled }: AccountPoolDialogProps) {
  const [open, setOpen] = useState(false)
  const [poolId, setPoolId] = useState("")
  const [poolName, setPoolName] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [linked, setLinked] = useState<{ id: string; name: string } | null>(null)
  const request = useLatestRequest()
  const inFlight = useRef(false)

  const add = async () => {
    if (!poolId || inFlight.current || disabled) return
    inFlight.current = true
    const current = request.begin()
    setBusy(true); setError("")
    try {
      // Read immediately before writing: preserve the order, settings and
      // optimistic version of the selected pool. Never overwrite a stale list.
      const pool = await integrationPools.get(poolId)
      if (!current()) return
      if (pool.provider !== provider || pool.deletionMark) {
        setError("Выберите действующий пул того же провайдера."); return
      }
      if (!pool.accountIds.includes(accountId)) {
        await integrationPools.update(poolId, {
          code: pool.code, name: pool.name, provider: pool.provider, isActive: pool.isActive,
          accountIds: [...pool.accountIds, accountId], version: pool.version,
        })
      }
      if (!current()) return
      setLinked({ id: pool.id, name: pool.name }); setOpen(false)
    } catch (err) {
      if (current()) setError(err instanceof Error ? err.message : "Не удалось добавить аккаунт в пул. Попробуйте ещё раз.")
    } finally {
      inFlight.current = false
      if (current()) setBusy(false)
    }
  }

  return <>
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg bg-muted/50 p-4">
      <div className="flex flex-col gap-1">
        <p className="text-sm font-medium">{linked ? `Аккаунт добавлен в «${linked.name}»` : "Добавьте аккаунт в пул"}</p>
        <p className="text-xs text-muted-foreground">{disabled ? "Сначала запишите изменения в аккаунте." : "Состав пула сохраняется отдельно от настроек аккаунта."}</p>
      </div>
      {linked ? disabled ? <Button type="button" variant="outline" size="sm" disabled>Открыть пул</Button> : <Button type="button" variant="outline" size="sm" asChild><Link href={`/catalogs/integration-pools/${linked.id}`}>Открыть пул</Link></Button> :
        <Button type="button" variant="outline" size="sm" disabled={disabled} onClick={() => { setError(""); setOpen(true) }}>Добавить в пул…</Button>}
    </div>
    <Dialog open={open} onOpenChange={value => { if (!busy) setOpen(value) }}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto">
        <DialogHeader><DialogTitle>Добавить аккаунт в пул</DialogTitle><DialogDescription>{accountName}. Доступны пулы провайдера {providerLabel}.</DialogDescription></DialogHeader>
        <Field aria-labelledby="integration-pool-label" data-invalid={!!error}>
          <FieldLabel id="integration-pool-label" htmlFor="integration-pool-choice">Пул аккаунтов</FieldLabel>
          <ReferenceField id="integration-pool-choice" value={poolId} displayName={poolName} apiEndpoint="/catalog/integration-pools"
            filter={[{ field: "provider", operator: "eq", value: provider }]}
            placeholder={`Выберите пул ${providerLabel}`} disabled={busy}
            onChange={(value, name) => { setPoolId(value); setPoolName(name); setError("") }} />
          <FieldDescription>Аккаунт будет добавлен в конец списка. Настройки и порядок остальных аккаунтов сохранятся.</FieldDescription>
          {error && <FieldError>{error}</FieldError>}
        </Field>
        <DialogFooter><Button type="button" variant="outline" disabled={busy} onClick={() => setOpen(false)}>Отмена</Button><Button type="button" disabled={!poolId || busy || disabled} onClick={() => void add()}>{busy && <Loader2 data-icon="inline-start" className="animate-spin"/>}{busy ? "Добавляем…" : "Добавить аккаунт"}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  </>
}
