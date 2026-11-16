"use client"

import Link from "next/link"
import useSWR from "swr"
import { Copy, RefreshCw } from "lucide-react"
import { toast } from "sonner"

import { api } from "@/lib/api"
import { copyToClipboard, formatDateTime, formatMinorUnits } from "@/lib/format"
import { usePermissions } from "@/hooks/use-permissions"
import { useTabTitle } from "@/hooks/useTabTitle"
import type { CustodyReceiptAttribution, CustodyReceiptDetailResponse } from "@/types/generated/http-contracts"
import {
    receiptAttributionStatuses,
    receiptAttributionTypes,
    receiptAuditStates,
    receiptReasons,
    receiptRowStateHints,
    receiptRowStates,
} from "@/configuration/crypto/types/unidentified-receipt"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

interface CustodyReceiptCardProps {
    id?: string
    entityName?: string
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
    return <div className="flex flex-col gap-1">
        <dt className="text-xs text-muted-foreground">{label}</dt>
        <dd className="text-sm">{children}</dd>
    </div>
}

function Hash({ value }: { value: string }) {
    if (!value) return <span className="text-muted-foreground">—</span>
    return <span className="flex items-center gap-1">
        <span className="break-all font-mono text-xs">{value}</span>
        <Button variant="ghost" size="icon" className="size-6 shrink-0" aria-label="Скопировать"
            onClick={() => { copyToClipboard(value); toast.success("Скопировано") }}>
            <Copy className="size-3" />
        </Button>
    </span>
}

const attributionRoutes: Record<string, string> = {
    CryptoPayment: "crypto-payment",
    CryptoMerchantCredit: "crypto-merchant-credit",
    CryptoPlatformFunding: "crypto-platform-funding",
}

function attributionHref(item: CustodyReceiptAttribution): string | undefined {
    const route = attributionRoutes[item.documentType]
    return route ? `/documents/${route}/${item.documentId}` : undefined
}

/**
 * Card of a custody receipt: the independent proof the receipt rests on, its
 * canonicality audit, the unattributed remainder and the documents (invoice
 * payments, manual merchant credits) that distributed it.
 */
export default function CustodyReceiptCard({ id }: CustodyReceiptCardProps) {
    const permissions = usePermissions()
    const canRead = permissions.has("crypto_receipt:read")
    const { data, error, isLoading, mutate } = useSWR<CustodyReceiptDetailResponse>(
        canRead && id ? ["custody-receipt", id] : null,
        () => api.unidentifiedReceipts.detail(id!),
    )
    useTabTitle(data?.receipt.number, "Поступление")

    if (!canRead) return <p className="p-6 text-muted-foreground">Нет доступа к поступлениям.</p>
    if (!id) return <p className="p-6 text-muted-foreground">Поступления создаются системой по подтверждённым переводам.</p>
    if (error) return <p role="alert" className="p-6 text-destructive">Не удалось загрузить поступление.</p>
    if (isLoading || !data) return <div className="flex flex-col gap-4 p-6"><Skeleton className="h-8 w-64" /><Skeleton className="h-48 w-full" /></div>

    const { receipt, proof, audit, attributions } = data
    const amount = (raw: string) => `${formatMinorUnits(raw, receipt.decimalPlaces)} ${receipt.tokenSymbol}`
    const hint = receiptRowStateHints[receipt.state]

    return <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto p-6 [&>*]:shrink-0">
        <div className="flex flex-wrap items-start justify-between gap-4">
            <div className="flex flex-col gap-1">
                <h1 className="text-2xl font-semibold">Поступление {receipt.number}</h1>
                <p className="text-sm text-muted-foreground">
                    Подтверждённый перевод на управляемый кошелёк. Учтён на кошельке и в невыясненных;
                    мерчанту зачисляется платежом по счёту или ручным зачислением.
                </p>
            </div>
            <div className="flex items-center gap-2">
                <Button variant="outline" asChild><Link href={`/documents/crypto-custody-receipt/${id}/movements`}>Движения</Link></Button>
                <Button variant="outline" asChild><Link href="/crypto/unidentified-receipts">Журнал невыясненных</Link></Button>
                <Button variant="outline" size="icon" aria-label="Обновить" onClick={() => mutate()}><RefreshCw /></Button>
            </div>
        </div>

        <Card>
            <CardHeader>
                <CardTitle className="flex flex-wrap items-center gap-2">
                    {amount(receipt.amount)}
                    <Badge variant={receipt.state === "orphaned" || receipt.state === "payment_reorged" ? "destructive" : "secondary"}>
                        {receiptRowStates[receipt.state] ?? receipt.state}
                    </Badge>
                </CardTitle>
                <CardDescription>Остаток в невыясненных: {amount(receipt.remainder)}</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
                {hint && <p className="text-sm text-muted-foreground">{hint}</p>}
                <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                    <Field label="Получено">{formatDateTime(receipt.receivedAt)}</Field>
                    <Field label="Причина">{receiptReasons[receipt.reason] ?? receipt.reason}</Field>
                    <Field label="Сеть">{receipt.networkName}</Field>
                    <Field label="Кошелёк"><Hash value={receipt.toAddress} /></Field>
                    <Field label="Отправитель"><Hash value={receipt.fromAddress} /></Field>
                    {receipt.paymentId && <Field label="Платёж по счёту">
                        <Link className="text-primary hover:underline" href={`/documents/crypto-payment/${receipt.paymentId}`}>
                            {receipt.paymentNumber || receipt.paymentId}
                        </Link>
                        {receipt.paymentStatus && <span className="text-muted-foreground"> · {receiptAttributionStatuses[receipt.paymentStatus] ?? receipt.paymentStatus}</span>}
                    </Field>}
                </dl>
            </CardContent>
        </Card>

        <Card>
            <CardHeader>
                <CardTitle>Доказательство перевода</CardTitle>
                <CardDescription>
                    {proof.original
                        ? "Независимое подтверждение, по которому учтено поступление."
                        : "Перевод повторно включён в другой блок: аудит проверяет новое подтверждение."}
                </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
                <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                    <Field label="Транзакция"><Hash value={proof.txHash} /></Field>
                    <Field label="Событие">{proof.eventKind} #{proof.eventIndex}</Field>
                    <Field label="Блок">{proof.blockNumber}</Field>
                    <Field label="Хеш блока"><Hash value={proof.blockHash} /></Field>
                    <Field label="Подтверждений">{proof.confirmations}</Field>
                    <Field label="Проверено">{formatDateTime(proof.verifiedAt)}</Field>
                </dl>
                <Table>
                    <TableHeader><TableRow>
                        <TableHead>Провайдер</TableHead><TableHead>Группа доверия</TableHead><TableHead>Высота головы</TableHead>
                    </TableRow></TableHeader>
                    <TableBody>
                        {proof.sources.map((source) => <TableRow key={`${source.providerId}-${source.trustGroup}`}>
                            <TableCell>{source.providerId}</TableCell>
                            <TableCell>{source.trustGroup}</TableCell>
                            <TableCell className="font-mono">{source.headHeight}</TableCell>
                        </TableRow>)}
                    </TableBody>
                </Table>
            </CardContent>
        </Card>

        <Card>
            <CardHeader>
                <CardTitle>Аудит каноничности</CardTitle>
                <CardDescription>Каждые 15 минут перевод проверяется заново. Доказанный откат блока сторнирует платёж по счёту и ставит сеть на hold.</CardDescription>
            </CardHeader>
            <CardContent>
                <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                    <Field label="Состояние">
                        <Badge variant={audit.status === "orphaned" ? "destructive" : "secondary"}>{receiptAuditStates[audit.status] ?? audit.status}</Badge>
                    </Field>
                    <Field label="Последняя проверка">{audit.lastCheckedAt ? formatDateTime(audit.lastCheckedAt) : "—"}</Field>
                    {audit.status === "canonical" && <Field label="Следующая проверка">{formatDateTime(audit.nextCheckAt)}</Field>}
                    {audit.orphanedAt && <Field label="Откат доказан">{formatDateTime(audit.orphanedAt)}</Field>}
                    {audit.incidentId && <Field label="Инцидент"><Hash value={audit.incidentId} /></Field>}
                    {audit.lastError && <Field label={`Ошибка проверки (попыток: ${audit.attempts})`}>
                        <span className="text-destructive">{audit.lastError}</span>
                    </Field>}
                </dl>
            </CardContent>
        </Card>

        <Card>
            <CardHeader>
                <CardTitle>Распределение</CardTitle>
                <CardDescription>Документы, которые списали поступление из невыясненных. «Удерживает» — сумма после сторно.</CardDescription>
            </CardHeader>
            <CardContent>
                <Table>
                    <TableHeader><TableRow>
                        <TableHead>Документ</TableHead><TableHead>Мерчант</TableHead><TableHead>Статус</TableHead>
                        <TableHead className="text-right">Сумма</TableHead><TableHead className="text-right">Мерчанту</TableHead>
                        <TableHead className="text-right">Комиссия</TableHead><TableHead className="text-right">Удерживает</TableHead>
                    </TableRow></TableHeader>
                    <TableBody>
                        {attributions.map((item) => {
                            const href = attributionHref(item)
                            return <TableRow key={item.documentId}>
                                <TableCell>
                                    <div>{href ? <Link className="text-primary hover:underline" href={href}>{item.number}</Link> : item.number}</div>
                                    <div className="text-xs text-muted-foreground">{receiptAttributionTypes[item.documentType] ?? item.documentType} · {formatDateTime(item.date)}</div>
                                </TableCell>
                                <TableCell>{item.merchantName}</TableCell>
                                <TableCell>{receiptAttributionStatuses[item.status] ?? item.status}</TableCell>
                                <TableCell className="text-right font-mono">{amount(item.amount)}</TableCell>
                                <TableCell className="text-right font-mono">{amount(item.netAmount)}</TableCell>
                                <TableCell className="text-right font-mono">{amount(item.processingFee)}</TableCell>
                                <TableCell className="text-right font-mono">{amount(item.outstanding)}</TableCell>
                            </TableRow>
                        })}
                        {attributions.length === 0 && <TableRow>
                            <TableCell colSpan={7} className="text-center text-muted-foreground">Поступление ещё не распределено</TableCell>
                        </TableRow>}
                    </TableBody>
                </Table>
            </CardContent>
        </Card>
    </div>
}
