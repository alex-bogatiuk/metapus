"use client"

import { useEffect, useMemo, useState } from "react"
import Link from "next/link"
import { ArrowLeft, ArrowRight, ArrowDownLeft, ArrowUpRight, Copy, ExternalLink, RefreshCw } from "lucide-react"
import { toast } from "sonner"

import { copyToClipboard, formatDateTime, formatMinorUnits } from "@/lib/format"
import { useEnumFormatter } from "@/hooks/useEntityFiltersMeta"
import type { DocumentMovementsResponse, EntityDocumentMovement } from "@/types/generated/http-contracts"
import type { CryptoSweepDetailResponse, CryptoSweepTokenDisplay, CryptoSweepWalletDisplay } from "@/configuration/crypto/types/generated/http-contracts"
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

import { useTabTitle } from "@/hooks/useTabTitle"
import { useMetadataStore } from "@/stores/useMetadataStore"

import { cryptoApi } from "@/configuration/crypto/lib/api"
interface CryptoSweepCardProps {
    id?: string
    entityName?: string
}

interface AmountTotal {
    token?: CryptoSweepTokenDisplay
    amount: string
}

function readableAmount(amount: string | undefined, token: CryptoSweepTokenDisplay | undefined): string {
    if (amount === undefined || !token) return "—"
    return `${formatMinorUnits(amount, token.decimalPlaces)} ${token.symbol}`
}

function sumByToken(rows: { amount: string; token?: CryptoSweepTokenDisplay }[]): AmountTotal[] {
    const totals = new Map<string, { token?: CryptoSweepTokenDisplay; amount: bigint }>()
    for (const row of rows) {
        const key = row.token?.id ?? "unknown"
        const current = totals.get(key)
        totals.set(key, { token: row.token, amount: (current?.amount ?? 0n) + BigInt(row.amount) })
    }
    return [...totals.values()].map(({ token, amount }) => ({ token, amount: amount.toString() }))
}

function walletLabel(wallet: CryptoSweepWalletDisplay | undefined, id?: string): string {
    return wallet?.name || wallet?.address || (id ? `${id.slice(0, 8)}…` : "Недоступен")
}

function shortHash(hash: string): string {
    return hash.length > 22 ? `${hash.slice(0, 12)}…${hash.slice(-8)}` : hash
}

function txLink(template: string | undefined, hash: string): string | undefined {
    if (!template?.startsWith("https://") || !template.includes("{txHash}")) return undefined
    return template.replace("{txHash}", encodeURIComponent(hash))
}

function movementValue(movement: EntityDocumentMovement, key: string): string | undefined {
    const value = movement.data[key]
    if (typeof value === "string") return value
    if (value && typeof value === "object" && !Array.isArray(value)) {
        if (typeof value.name === "string") return value.name
        if (typeof value.id === "string") return value.id
    }
    return undefined
}

function statusVariant(status: string): "success" | "warning" | "destructive" | "secondary" {
    if (status === "confirmed") return "success"
    if (status === "held") return "warning"
    if (status === "partial_failed") return "destructive"
    return "secondary"
}

function settlementLabel(status: string): string {
    switch (status) {
        case "recorded": return "Расчёт записан"
        case "not_applicable": return "Расчёт не требуется"
        case "held": return "Требует сверки"
        case "repairing": return "Уточняется"
        default: return "Ожидает расчёта"
    }
}

export default function CryptoSweepCard({ id, entityName = "CryptoSweep" }: CryptoSweepCardProps) {
    const formatEnum = useEnumFormatter("CryptoSweep")
    const [detail, setDetail] = useState<CryptoSweepDetailResponse | null>(null)
    const [movements, setMovements] = useState<DocumentMovementsResponse | null>(null)
    const [movementError, setMovementError] = useState(false)
    const [loading, setLoading] = useState(true)
    const [retryingSweep, setRetryingSweep] = useState(false)
    const [error, setError] = useState("")
    const [revision, setRevision] = useState(0)

    const entityMeta = useMetadataStore((s) => s.getEntityByName(entityName) ?? s.byKey["crypto_sweep"])
    const entityLabel = entityMeta?.presentation?.singular ?? "Крипто-свип"
    const displayNumber = detail?.number || (detail ? "Свип" : undefined)
    useTabTitle(displayNumber, entityLabel)

    useEffect(() => {
        if (!id) return
        let active = true
        Promise.allSettled([cryptoApi.cryptoSweeps.get(id), cryptoApi.cryptoSweeps.movements(id)]).then(([docResult, movementResult]) => {
            if (!active) return
            if (docResult.status === "fulfilled") {
                setDetail(docResult.value)
                setError("")
            } else {
                setError(docResult.reason instanceof Error ? docResult.reason.message : "Не удалось загрузить свип")
            }
            if (movementResult.status === "fulfilled") {
                setMovements(movementResult.value)
                setMovementError(false)
            } else {
                setMovements(null)
                setMovementError(true)
            }
            setLoading(false)
        })
        return () => { active = false }
    }, [id, revision])

    const feeTotals = useMemo(() => sumByToken(detail?.networkFees ?? []), [detail])
    const rentalTotals = useMemo(() => sumByToken((detail?.resourceCosts ?? []).filter((cost) => BigInt(cost.amount) > 0n)), [detail])
    const retry = () => { setLoading(true); setRevision((value) => value + 1) }
    const handleRetrySweep = async () => {
        if (!id) return
        setRetryingSweep(true)
        try {
            await cryptoApi.cryptoSweeps.retry(id)
            toast.success("Свип поставлен в очередь на повторное выполнение")
            retry()
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Не удалось перезапустить свип")
        } finally {
            setRetryingSweep(false)
        }
    }
    const copy = (value: string) => { copyToClipboard(value); toast.success("Скопировано") }

    if (!id) return <div className="p-8 text-sm text-muted-foreground">Крипто-свип создаётся автоматически. Откройте существующий документ из списка.</div>

    if (loading && !detail) {
        return <div className="mx-auto max-w-6xl space-y-5 p-4 md:p-8" aria-busy="true">
            <Skeleton className="h-10 w-64" /><Skeleton className="h-40 w-full" /><Skeleton className="h-72 w-full" />
        </div>
    }
    if (!detail) {
        return <div className="mx-auto max-w-6xl space-y-4 p-4 md:p-8">
            <p role="alert" className="text-sm text-destructive">{error}</p>
            <Button variant="outline" onClick={retry}><RefreshCw />Повторить</Button>
        </div>
    }

    const asset = detail.asset
    const hasRentalPending = detail.resourceCosts.some((cost) => cost.amount === "0" && ["reserved", "broadcast"].includes(cost.status))
    const linkedFeePending = detail.networkFees.length === 0 && detail.lines.some((line) => line.settlementStatus !== "not_applicable")
    const lineErrors = detail.lines.filter((line) => Boolean(line.settlementError))

    return <div className="h-full overflow-y-auto bg-workspace-surface">
        <div className="mx-auto max-w-6xl space-y-6 px-4 py-5 pb-12 md:px-8 md:py-8">
            <header className="flex flex-wrap items-start justify-between gap-4">
                <div className="min-w-0 space-y-2">
                    <Link href="/documents/crypto-sweep" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft className="size-4" />Крипто-свипы</Link>
                    <div className="flex flex-wrap items-center gap-3">
                        <h1 className="text-2xl font-semibold tracking-tight md:text-3xl">Свип {detail.number || "без номера"}</h1>
                        <Badge variant={statusVariant(detail.status ?? "")}>{formatEnum("status", detail.status ?? "") || detail.status}</Badge>
                        <Badge variant="outline">Учёт по подтверждённым переводам</Badge>
                    </div>
                    <p className="text-sm text-muted-foreground">{detail.date ? formatDateTime(detail.date) : "Дата недоступна"}{asset?.networkName ? ` · ${asset.networkName}` : ""}</p>
                </div>
                <div className="flex items-center gap-2">
                    {detail.canRetry && (
                        <Button size="sm" onClick={handleRetrySweep} disabled={retryingSweep || loading}>
                            <RefreshCw className={retryingSweep ? "animate-spin" : ""} />Повторить свип
                        </Button>
                    )}
                    <Button variant="outline" size="sm" onClick={() => copy(window.location.href)}><Copy />Ссылка</Button>
                    <Button variant="outline" size="sm" onClick={retry} disabled={loading}><RefreshCw className={loading ? "animate-spin" : ""} />Обновить</Button>
                </div>
            </header>

            {lineErrors.length > 0 && (
                <Card className="border-destructive/40 bg-destructive/5">
                    <CardContent className="space-y-1 pt-6 text-sm">
                        <p className="font-medium text-destructive">Причина остановки выполнения</p>
                        {lineErrors.map((line, index) => (
                            <p key={line.lineId || index} className="break-words font-mono text-xs text-destructive">
                                Строка {line.lineNo}: {line.settlementError}
                            </p>
                        ))}
                    </CardContent>
                </Card>
            )}

            <Card>
                <CardContent className="grid gap-6 pt-6 lg:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] lg:items-center">
                    <div className="min-w-0">
                        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Откуда · пул</p>
                        <p className="mt-2 text-lg font-medium">{detail.lines.length === 1 ? walletLabel(detail.lines[0].wallet, detail.lines[0].walletId) : `${detail.lines.length} кошельков`}</p>
                        {detail.lines.length === 1 && detail.lines[0].wallet?.address && <p className="truncate font-mono text-xs text-muted-foreground" title={detail.lines[0].wallet.address}>{detail.lines[0].wallet.address}</p>}
                    </div>
                    <ArrowRight className="hidden size-5 text-muted-foreground lg:block" aria-hidden="true" />
                    <div className="min-w-0 border-t pt-5 lg:border-l lg:border-t-0 lg:pl-6 lg:pt-0">
                        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Куда · горячий кошелёк</p>
                        <p className="mt-2 text-lg font-medium">{walletLabel(detail.hotWallet, detail.hotWalletId)}</p>
                        {detail.hotWallet?.address && <p className="truncate font-mono text-xs text-muted-foreground" title={detail.hotWallet.address}>{detail.hotWallet.address}</p>}
                    </div>
                </CardContent>
            </Card>

            <div className="grid gap-4 md:grid-cols-3">
                <Card><CardContent className="pt-6"><p className="text-sm text-muted-foreground">Переведено</p><p className="mt-2 break-all text-xl font-semibold tabular-nums md:text-2xl">{readableAmount(detail.totalAmount, asset)}</p><p className="mt-2 text-xs text-muted-foreground">{detail.lineCount ?? detail.lines.length} строк</p></CardContent></Card>
                <Card><CardContent className="pt-6"><p className="text-sm text-muted-foreground">Сетевая комиссия · факт</p>{feeTotals.length ? feeTotals.map((total, i) => <p key={total.token?.id ?? i} className="mt-2 break-all text-xl font-semibold tabular-nums md:text-2xl">{readableAmount(total.amount, total.token)}</p>) : <p className="mt-2 text-sm text-muted-foreground">{linkedFeePending ? "Ожидает записи комиссии" : "Нет зарегистрированной комиссии"}</p>}<p className="mt-2 text-xs text-muted-foreground">Расходы ChainSettlement</p></CardContent></Card>
                <Card><CardContent className="pt-6"><p className="text-sm text-muted-foreground">Аренда ресурсов · распределено</p>{rentalTotals.length ? rentalTotals.map((total, i) => <p key={total.token?.id ?? i} className="mt-2 break-all text-xl font-semibold tabular-nums md:text-2xl">{readableAmount(total.amount, total.token)}</p>) : <p className="mt-2 text-sm text-muted-foreground">{hasRentalPending || !detail.posted ? "Ожидает распределения" : "Нет распределённых затрат"}</p>}<p className="mt-2 text-xs text-muted-foreground">Только доля этого свипа</p></CardContent></Card>
            </div>

            <Card>
                <CardHeader><CardTitle>Операции по кошелькам</CardTitle><p className="text-sm text-muted-foreground">Транзакции и расчёт по каждой строке свипа</p></CardHeader>
                <CardContent>
                    {detail.lines.length === 0 ? <p className="text-sm text-muted-foreground">Ожидаются строки свипа.</p> : <>
                        <div className="hidden md:block">
                            <Table><TableHeader><TableRow><TableHead className="w-12">№</TableHead><TableHead>Кошелёк пула</TableHead><TableHead className="text-right">Сумма</TableHead><TableHead>Транзакция</TableHead><TableHead>Расчёт</TableHead></TableRow></TableHeader>
                                <TableBody>{detail.lines.map((line, index) => <TableRow key={line.lineId || index}><TableCell className="text-muted-foreground">{line.lineNo}</TableCell><TableCell className="max-w-48 truncate" title={line.wallet?.address}>{walletLabel(line.wallet, line.walletId)}</TableCell><TableCell className="text-right font-medium tabular-nums">{readableAmount(line.amount, asset)}</TableCell><TableCell>{line.txHash ? <span className="inline-flex max-w-52 items-center gap-1 font-mono text-xs"><span className="truncate" title={line.txHash}>{shortHash(line.txHash)}</span><Button variant="ghost" size="icon" className="size-7" aria-label="Скопировать хеш транзакции" onClick={() => copy(line.txHash!)}><Copy /></Button>{txLink(asset?.explorerUrl, line.txHash) && <a href={txLink(asset?.explorerUrl, line.txHash)} target="_blank" rel="noopener noreferrer" aria-label="Открыть транзакцию в обозревателе"><ExternalLink className="size-4" /></a>}</span> : <span className="text-muted-foreground">Ожидается</span>}</TableCell><TableCell><Badge variant={line.settlementStatus === "recorded" ? "success" : line.settlementStatus === "held" ? "warning" : "secondary"}>{settlementLabel(line.settlementStatus ?? "")}</Badge></TableCell></TableRow>)}</TableBody>
                            </Table>
                        </div>
                        <div className="space-y-3 md:hidden">{detail.lines.map((line, index) => <div key={line.lineId || index} className="space-y-2 rounded-lg border p-4"><div className="flex justify-between gap-3"><span className="font-medium">{walletLabel(line.wallet, line.walletId)}</span><Badge variant={line.settlementStatus === "recorded" ? "success" : "secondary"}>{settlementLabel(line.settlementStatus ?? "")}</Badge></div><p className="font-semibold tabular-nums">{readableAmount(line.amount, asset)}</p><p className="truncate font-mono text-xs text-muted-foreground" title={line.txHash}>{line.txHash || "Транзакция ожидается"}</p></div>)}</div>
                        <Accordion type="single" collapsible className="mt-5 border-t">
                            {detail.lines.map((line, index) => <AccordionItem key={line.lineId || index} value={line.lineId || String(index)}><AccordionTrigger className="text-sm">Технические данные · строка {line.lineNo}</AccordionTrigger><AccordionContent><dl className="grid gap-3 text-sm sm:grid-cols-2"><div><dt className="text-muted-foreground">Результат в сети</dt><dd>{line.chainResult || (line.confirmed ? "Подтверждена" : "Ожидается")}</dd></div><div><dt className="text-muted-foreground">Проверено</dt><dd>{line.settlementCheckedAt ? formatDateTime(line.settlementCheckedAt) : "—"}</dd></div><div><dt className="text-muted-foreground">Energy</dt><dd className="tabular-nums">{line.energyUsed ?? "—"}</dd></div><div><dt className="text-muted-foreground">Bandwidth</dt><dd className="tabular-nums">{line.netUsage ?? "—"}</dd></div>{line.settlementError && <div className="sm:col-span-2"><dt className="text-muted-foreground">Ошибка расчёта</dt><dd className="text-destructive">{line.settlementError}</dd></div>}</dl></AccordionContent></AccordionItem>)}
                        </Accordion>
                    </>}
                </CardContent>
            </Card>

            <Card>
                <CardHeader><CardTitle>Учёт и связанные расходы</CardTitle><p className="text-sm text-muted-foreground">Прямые движения документа отделены от расходов, записанных связанными регистраторами.</p></CardHeader>
                <CardContent className="space-y-6">
                    <section className="space-y-3" aria-label="Прямые движения CryptoSweep">
                        <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="font-medium">Прямые движения CryptoSweep</h3><Button variant="link" size="sm" asChild><Link href={`/documents/crypto-sweeps/${id}/movements`}>Все движения <ArrowRight /></Link></Button></div>
                        {movementError ? <p className="text-sm text-muted-foreground">Движения временно недоступны. Обновите карточку.</p> : movements?.movements.length ? <div className="divide-y rounded-lg border">{movements.movements.map((movement, index) => <div key={index} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-sm"><span className="inline-flex items-center gap-2">{movement.recordType === "receipt" ? <ArrowDownLeft className="size-4 text-success" /> : <ArrowUpRight className="size-4 text-muted-foreground" />}{movement.recordType === "receipt" ? "Приход" : "Расход"} · {movementValue(movement, "wallet") || "Кошелёк скрыт"}</span><span className="font-medium tabular-nums">{readableAmount(movementValue(movement, "amount"), asset)}</span></div>)}</div> : <p className="text-sm text-muted-foreground">Приход и расход появятся после независимого подтверждения перевода. Отклонённая транзакция не создаёт движения основной суммы.</p>}
                    </section>
                    <section className="space-y-3 border-t pt-5" aria-label="Сетевая комиссия ChainSettlement">
                        <h3 className="font-medium">Сетевая комиссия · ChainSettlement</h3>
                        {detail.networkFees.length ? <div className="divide-y rounded-lg border">{detail.networkFees.map((fee, index) => <div key={fee.settlementId || index} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-sm"><div><p>Строка {detail.lines.find((line) => line.lineId === fee.lineId)?.lineNo ?? "—"} · сетевой расход</p><p className="font-mono text-xs text-muted-foreground" title={fee.txHash}>{shortHash(fee.txHash)}</p></div><span className="font-medium tabular-nums">{readableAmount(fee.amount, fee.token)}</span></div>)}</div> : <p className="text-sm text-muted-foreground">{linkedFeePending ? "Сетевая комиссия ещё не записана в регистр." : "Зарегистрированных сетевых расходов нет."}</p>}
                    </section>
                    <section className="space-y-3 border-t pt-5" aria-label="Распределение аренды ресурсов">
                        <h3 className="font-medium">Аренда ресурсов · распределение</h3>
                        {detail.resourceCosts.length ? <div className="divide-y rounded-lg border">{detail.resourceCosts.map((cost, index) => <div key={`${cost.rentalId}-${cost.lineId}-${index}`} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-sm"><div><Link href={`/documents/crypto-resource-rental/${cost.rentalId}`} className="font-medium text-primary hover:underline">Аренда {cost.rentalNumber || shortHash(cost.rentalId)} <ExternalLink className="inline size-3" /></Link><p className="text-xs text-muted-foreground">Строка {detail.lines.find((line) => line.lineId === cost.lineId)?.lineNo ?? "—"} · {cost.status}</p></div><span className="font-medium tabular-nums">{cost.amount === "0" && ["reserved", "broadcast"].includes(cost.status) ? "Ожидает распределения" : readableAmount(cost.amount, cost.token)}</span></div>)}</div> : <p className="text-sm text-muted-foreground">{detail.posted ? "Для этого свипа нет распределённых затрат." : "Расчёт затрат ожидается."}</p>}
                    </section>
                </CardContent>
            </Card>
        </div>
    </div>
}
