"use client"

import { Loader2, ShieldCheck } from "lucide-react"

import { truncateAddress } from "@/configuration/crypto/lib/blockchain"
import { formatDateTime } from "@/lib/format"
import type {
  ExternalOutflowClassification,
  ExternalOutflowDetail,
} from "@/configuration/crypto/types/treasury"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"

import { DetailValue, FormField } from "./withdrawal-components"

export function OutflowDetailSheet({
  open,
  loading,
  detail,
  classification,
  reason,
  matchedBasisType,
  matchedBasisId,
  evidence,
  actionPending,
  formatTokenAmount,
  onOpenChange,
  onClassificationChange,
  onReasonChange,
  onMatchedBasisTypeChange,
  onMatchedBasisIdChange,
  onEvidenceChange,
  onClassify,
  onApproveAdjustment,
  onRejectAdjustment,
  onResolve,
}: {
  open: boolean
  loading: boolean
  detail: ExternalOutflowDetail | null
  classification: ExternalOutflowClassification
  reason: string
  matchedBasisType: string
  matchedBasisId: string
  evidence: string
  actionPending: boolean
  formatTokenAmount: (minor: string, tokenId?: string) => string
  onOpenChange: (open: boolean) => void
  onClassificationChange: (value: ExternalOutflowClassification) => void
  onReasonChange: (value: string) => void
  onMatchedBasisTypeChange: (value: string) => void
  onMatchedBasisIdChange: (value: string) => void
  onEvidenceChange: (value: string) => void
  onClassify: () => void
  onApproveAdjustment: () => void
  onRejectAdjustment: () => void
  onResolve: () => void
}) {
  const outflow = detail?.outflow
  const canReclassify = outflow?.status === "classified" && detail?.adjustment?.status === "rejected"

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="flex w-full flex-col p-0 sm:max-w-2xl">
        <SheetHeader className="border-b px-6 py-5 pr-12">
          <div className="flex items-center gap-2">
            <SheetTitle>Внешнее списание</SheetTitle>
            {outflow && <Badge variant={outflow.status === "resolved" ? "secondary" : "destructive"}>{outflow.status}</Badge>}
          </div>
          <SheetDescription>
            Exact chain effect, доказательства, независимые holds и maker-checker решения.
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          {loading || !outflow || !detail ? (
            <div className="space-y-3 p-6">
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-40 w-full" />
              <Skeleton className="h-28 w-full" />
            </div>
          ) : (
            <div className="space-y-6 p-6">
              <section className="grid grid-cols-2 gap-x-4 gap-y-3 rounded-lg border bg-muted/20 p-4">
                <DetailValue label="Сумма" value={formatTokenAmount(outflow.amount, outflow.tokenId)} mono />
                <DetailValue label="Finality" value={`${outflow.finalityStatus} · ${outflow.confirmations}/${outflow.requiredConfirmations}`} />
                <DetailValue label="Event" value={`${outflow.eventKind} #${outflow.eventIndex}`} mono />
                <DetailValue label="Block" value={`${outflow.blockNumber} · ${truncateAddress(outflow.blockHash)}`} mono />
                <DetailValue label="From" value={outflow.fromAddress} mono wide />
                <DetailValue label="To" value={outflow.toAddress} mono wide />
                <DetailValue label="Contract" value={outflow.contractAddress || "native"} mono wide />
                <DetailValue label="TX" value={outflow.txHash} mono wide />
              </section>

              {detail.openHolds.length > 0 && (
                <section className="rounded-lg border border-destructive/30 bg-destructive/5 p-4">
                  <div className="flex items-center gap-2 text-sm font-medium text-destructive">
                    <ShieldCheck className="h-4 w-4" />
                    Открытые operational holds: {detail.openHolds.length}
                  </div>
                  <div className="mt-3 space-y-2">
                    {detail.openHolds.map((hold) => (
                      <div key={`${hold.scope}:${hold.ownerType}:${hold.ownerId}`} className="text-xs">
                        <span className="font-medium">{hold.scope}</span> · {hold.reason}
                        <div className="text-muted-foreground">owner: {hold.ownerType}/{hold.ownerId}</div>
                      </div>
                    ))}
                  </div>
                </section>
              )}

              {(outflow.status === "pending_review" || canReclassify) && (
                <section className="space-y-4">
                  <div>
                    <h3 className="text-sm font-semibold">{canReclassify ? "Повторная классификация" : "Классификация"}</h3>
                    <p className="mt-1 text-xs text-muted-foreground">
                      Требует свежий object-bound step-up. Причина и evidence сохраняются append-only.
                    </p>
                  </div>
                  <FormField label="Результат расследования">
                    <Select value={classification} onValueChange={(value) => onClassificationChange(value as ExternalOutflowClassification)}>
                      <SelectTrigger><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="unauthorized_loss">Несанкционированная custody loss</SelectItem>
                        <SelectItem value="matched_basis">Точное совпадение с основанием</SelectItem>
                        <SelectItem value="false_positive_or_reorg">False positive / reorg</SelectItem>
                      </SelectContent>
                    </Select>
                  </FormField>
                  {classification === "matched_basis" && (
                    <div className="grid gap-3 sm:grid-cols-2">
                      <FormField label="Тип основания"><Input value={matchedBasisType} onChange={(event) => onMatchedBasisTypeChange(event.target.value)} /></FormField>
                      <FormField label="ID основания"><Input value={matchedBasisId} onChange={(event) => onMatchedBasisIdChange(event.target.value)} /></FormField>
                    </div>
                  )}
                  <FormField label="Причина"><Textarea value={reason} onChange={(event) => onReasonChange(event.target.value)} /></FormField>
                  <FormField label="Evidence (JSON)"><Textarea className="font-mono text-xs" placeholder='{"source":"rpc","proof":"..."}' value={evidence} onChange={(event) => onEvidenceChange(event.target.value)} /></FormField>
                  <Button onClick={onClassify} disabled={actionPending || !reason.trim()}>
                    {actionPending && <Loader2 className="animate-spin" />}
                    Сохранить классификацию
                  </Button>
                </section>
              )}

              {outflow.status !== "pending_review" && (
                <section className="space-y-3">
                  <h3 className="text-sm font-semibold">Зафиксированная классификация</h3>
                  <div className="rounded-lg border p-4 text-sm">
                    <Badge variant="outline">{outflow.classification}</Badge>
                    <p className="mt-3">{detail.classificationReason || "Причина не указана"}</p>
                    {detail.matchedBasisId && <p className="mt-2 font-mono text-xs text-muted-foreground">{detail.matchedBasisType}/{detail.matchedBasisId}</p>}
                    {detail.classificationEvidence && (
                      <pre className="mt-3 overflow-auto rounded bg-muted p-3 text-xs">{JSON.stringify(detail.classificationEvidence, null, 2)}</pre>
                    )}
                  </div>
                </section>
              )}

              {detail.adjustment && (
                <section className="space-y-3">
                  <div>
                    <h3 className="text-sm font-semibold">CryptoCustodyAdjustment</h3>
                    <p className="mt-1 text-xs text-muted-foreground">Merchant balance не затрагивается. Maker не может быть checker.</p>
                    <p className="mt-1 text-xs text-muted-foreground">Списание потери требует независимого подтверждения перевода в сети.</p>
                  </div>
                  <div className="rounded-lg border p-4 text-sm">
                    <div className="flex items-center justify-between gap-3">
                      <span className="font-mono text-xs">{detail.adjustment.id}</span>
                      <Badge variant={detail.adjustment.status === "posted" ? "secondary" : detail.adjustment.status === "rejected" ? "destructive" : "outline"}>
                        {detail.adjustment.status}
                      </Badge>
                    </div>
                    <p className="mt-3 font-mono tabular-nums">{formatTokenAmount(detail.adjustment.amount, detail.adjustment.tokenId)}</p>
                    <p className="mt-2 text-xs text-muted-foreground">maker: {detail.adjustment.makerId}</p>
                  </div>
                  {detail.adjustment.status === "pending_approval" && (
                    <>
                      <FormField label="Причина решения"><Textarea value={reason} onChange={(event) => onReasonChange(event.target.value)} /></FormField>
                      <div className="flex flex-wrap gap-2">
                        <Button onClick={onApproveAdjustment} disabled={actionPending || !reason.trim()}>
                          {actionPending && <Loader2 className="animate-spin" />} Одобрить и провести
                        </Button>
                        <Button variant="destructive" onClick={onRejectAdjustment} disabled={actionPending || !reason.trim()}>Отклонить</Button>
                      </div>
                    </>
                  )}
                </section>
              )}

              {outflow.status === "classified" && !canReclassify && (
                <section className="space-y-3">
                  <div>
                    <h3 className="text-sm font-semibold">Resolution</h3>
                    <p className="mt-1 text-xs text-muted-foreground">Resolver должен отличаться от classifier; holds снимаются атомарно только этим workflow.</p>
                  </div>
                  {detail.resolutionBlockers.length > 0 && (
                    <div className="rounded-lg border border-amber-500/30 bg-amber-500/5 p-4 text-xs">
                      <div className="font-medium">Пока закрыть нельзя:</div>
                      <ul className="mt-2 list-disc space-y-1 pl-4 text-muted-foreground">
                        {detail.resolutionBlockers.map((blocker) => <li key={blocker}>{blocker}</li>)}
                      </ul>
                    </div>
                  )}
                  <FormField label="Причина закрытия"><Textarea value={reason} onChange={(event) => onReasonChange(event.target.value)} /></FormField>
                  <Button onClick={onResolve} disabled={actionPending || !detail.canResolve || !reason.trim()}>
                    {actionPending && <Loader2 className="animate-spin" />} Закрыть инцидент
                  </Button>
                </section>
              )}

              <Separator />
              <section className="space-y-3">
                <h3 className="text-sm font-semibold">История действий</h3>
                {detail.actions.length === 0 ? (
                  <p className="text-xs text-muted-foreground">Действия ещё не зафиксированы</p>
                ) : (
                  <div className="space-y-3">
                    {detail.actions.map((action, index) => (
                      <div key={`${action.createdAt}:${index}`} className="border-l-2 pl-3 text-xs">
                        <div className="flex items-center justify-between gap-3">
                          <span className="font-medium">{action.action}</span>
                          <span className="text-muted-foreground">{formatDateTime(action.createdAt)}</span>
                        </div>
                        <p className="mt-1">{action.oldState} → {action.newState}</p>
                        <p className="mt-1 text-muted-foreground">{action.reason}</p>
                      </div>
                    ))}
                  </div>
                )}
              </section>
            </div>
          )}
        </ScrollArea>
        <SheetFooter className="border-t px-6 py-4">
          <Button variant="outline" onClick={() => onOpenChange(false)}>Закрыть</Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
