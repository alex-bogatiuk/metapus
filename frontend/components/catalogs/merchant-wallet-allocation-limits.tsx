"use client"

import { useCallback, useEffect, useState } from "react"
import { LoaderCircle, Save } from "lucide-react"
import { toast } from "sonner"

import { api, ApiError } from "@/lib/api"
import type {
  MerchantWalletAllocationLimits,
  MerchantWalletAllocationLimitsUpdate,
  WalletPoolPolicy,
} from "@/types/wallet-pool"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"

interface MerchantWalletAllocationLimitsProps {
  merchantId: string
  isNew: boolean
}

interface AllocationRow {
  policy: WalletPoolPolicy
  limits: MerchantWalletAllocationLimits
}

export function MerchantWalletAllocationLimitsEditor({
  merchantId,
  isNew,
}: MerchantWalletAllocationLimitsProps) {
  const [rows, setRows] = useState<AllocationRow[]>([])
  const [loading, setLoading] = useState(!isNew)
  const [denied, setDenied] = useState(false)
  const [error, setError] = useState("")

  const load = useCallback(async () => {
    if (isNew) return
    setLoading(true)
    setError("")
    try {
      const pools = await api.walletPools.list()
      const loaded = await Promise.all(
        pools.items.map(async ({ policy }) => ({
          policy,
          limits: await api.walletPools.getMerchantOverride(
            policy.networkId,
            merchantId
          ),
        }))
      )
      setRows(loaded)
      setDenied(false)
    } catch (loadError) {
      if (loadError instanceof ApiError && loadError.status === 403) {
        setDenied(true)
      } else {
        setError(
          loadError instanceof Error
            ? loadError.message
            : "Не удалось загрузить лимиты адресов"
        )
      }
    } finally {
      setLoading(false)
    }
  }, [isNew, merchantId])

  useEffect(() => {
    void load()
  }, [load])

  if (isNew) {
    return (
      <p className="py-8 text-center text-sm text-muted-foreground">
        Сохраните мерчанта, чтобы настроить индивидуальные лимиты.
      </p>
    )
  }
  if (denied) {
    return (
      <p className="py-6 text-center text-sm text-muted-foreground">
        Для просмотра лимитов требуется право на управление wallet pool.
      </p>
    )
  }
  if (loading) return <Skeleton className="h-32 w-full" />
  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    )
  }

  return (
    <div className="flex flex-col gap-5">
      {rows.map((row) => (
        <AllocationNetworkRow
          key={row.policy.networkId}
          row={row}
          merchantId={merchantId}
          onChanged={(limits) =>
            setRows((current) =>
              current.map((candidate) =>
                candidate.policy.networkId === row.policy.networkId
                  ? { ...candidate, limits }
                  : candidate
              )
            )
          }
          onConflict={load}
        />
      ))}
    </div>
  )
}

function AllocationNetworkRow({
  row,
  merchantId,
  onChanged,
  onConflict,
}: {
  row: AllocationRow
  merchantId: string
  onChanged: (limits: MerchantWalletAllocationLimits) => void
  onConflict: () => Promise<void>
}) {
  const [form, setForm] = useState<MerchantWalletAllocationLimitsUpdate>(() =>
    toUpdate(row.limits)
  )
  const [saving, setSaving] = useState(false)

  useEffect(() => setForm(toUpdate(row.limits)), [row.limits])

  const errors = validate(form)
  const dirty = !updatesEqual(form, toUpdate(row.limits))

  const save = async () => {
    if (Object.keys(errors).length > 0) return
    setSaving(true)
    try {
      const updated = await api.walletPools.updateMerchantOverride(
        row.policy.networkId,
        merchantId,
        form
      )
      onChanged(updated)
      toast.success(`Лимиты ${row.policy.networkCode} сохранены`)
    } catch (saveError) {
      if (saveError instanceof ApiError && saveError.status === 409) {
        toast.error("Лимиты уже изменены. Загружаю актуальную версию.")
        await onConflict()
      } else {
        toast.error(
          saveError instanceof Error
            ? saveError.message
            : "Не удалось сохранить лимиты"
        )
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="flex flex-col gap-3 border-b pb-5 last:border-b-0 last:pb-0">
      <div>
        <p className="text-sm font-medium">{row.policy.networkName}</p>
        <p className="text-xs text-muted-foreground">{row.policy.networkCode}</p>
      </div>
      <div className="grid gap-3 md:grid-cols-3">
        <NullableLimitField
          id={`${merchantId}-${row.policy.networkId}-active`}
          label="Активные invoice lease"
          inherited={row.policy.maxActiveInvoiceLeasesPerMerchant}
          value={form.maxActiveInvoiceLeases}
          error={errors.maxActiveInvoiceLeases}
          onChange={(value) => setForm({ ...form, maxActiveInvoiceLeases: value })}
        />
        <NullableLimitField
          id={`${merchantId}-${row.policy.networkId}-persistent`}
          label="Постоянные адреса"
          inherited={row.policy.maxPersistentAddressesPerMerchant}
          value={form.maxPersistentAddresses}
          error={errors.maxPersistentAddresses}
          onChange={(value) => setForm({ ...form, maxPersistentAddresses: value })}
        />
        <NullableLimitField
          id={`${merchantId}-${row.policy.networkId}-rate`}
          label="Новые lease в минуту"
          inherited={row.policy.maxLeaseStartsPerMinutePerMerchant}
          value={form.maxLeaseStartsPerMinute}
          error={errors.maxLeaseStartsPerMinute}
          onChange={(value) => setForm({ ...form, maxLeaseStartsPerMinute: value })}
        />
      </div>
      <div className="flex justify-end">
        <Button size="sm" disabled={!dirty || saving || Object.keys(errors).length > 0} onClick={save}>
          {saving ? (
            <LoaderCircle data-icon="inline-start" className="animate-spin" />
          ) : (
            <Save data-icon="inline-start" />
          )}
          Сохранить
        </Button>
      </div>
    </section>
  )
}

function NullableLimitField({
  id,
  label,
  inherited,
  value,
  error,
  onChange,
}: {
  id: string
  label: string
  inherited: number
  value: number | null
  error?: string
  onChange: (value: number | null) => void
}) {
  return (
    <Field data-invalid={Boolean(error)}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type="number"
        min={1}
        step={1}
        value={value ?? ""}
        placeholder={String(inherited)}
        aria-invalid={Boolean(error)}
        onChange={(event) => {
          const raw = event.target.value
          onChange(raw === "" ? null : Math.trunc(Number(raw)))
        }}
      />
      <FieldDescription>
        Пусто — network-default {inherited}.
      </FieldDescription>
      <FieldError>{error}</FieldError>
    </Field>
  )
}

function toUpdate(
  limits: MerchantWalletAllocationLimits
): MerchantWalletAllocationLimitsUpdate {
  return {
    maxActiveInvoiceLeases: limits.maxActiveInvoiceLeases,
    maxPersistentAddresses: limits.maxPersistentAddresses,
    maxLeaseStartsPerMinute: limits.maxLeaseStartsPerMinute,
    version: limits.version,
  }
}

function validate(
  value: MerchantWalletAllocationLimitsUpdate
): Record<string, string> {
  const errors: Record<string, string> = {}
  for (const field of [
    "maxActiveInvoiceLeases",
    "maxPersistentAddresses",
    "maxLeaseStartsPerMinute",
  ] as const) {
    const limit = value[field]
    if (limit !== null && (!Number.isSafeInteger(limit) || limit <= 0)) {
      errors[field] = "Введите положительное целое число или оставьте поле пустым."
    }
  }
  return errors
}

function updatesEqual(
  left: MerchantWalletAllocationLimitsUpdate,
  right: MerchantWalletAllocationLimitsUpdate
) {
  return (
    left.maxActiveInvoiceLeases === right.maxActiveInvoiceLeases &&
    left.maxPersistentAddresses === right.maxPersistentAddresses &&
    left.maxLeaseStartsPerMinute === right.maxLeaseStartsPerMinute &&
    left.version === right.version
  )
}
