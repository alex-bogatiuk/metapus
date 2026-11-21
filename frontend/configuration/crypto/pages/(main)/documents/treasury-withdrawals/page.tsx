"use client"

import { AllowlistTable } from "@/configuration/crypto/components/documents/treasury-withdrawals/allowlist-table"
import { OutflowDetailSheet } from "@/configuration/crypto/components/documents/treasury-withdrawals/outflow-detail-sheet"
import { OutflowsTable } from "@/configuration/crypto/components/documents/treasury-withdrawals/outflows-table"
import { FormField, Metric } from "@/configuration/crypto/components/documents/treasury-withdrawals/withdrawal-components"
import { WithdrawalsTable } from "@/configuration/crypto/components/documents/treasury-withdrawals/withdrawals-table"
import { FormToolbar } from "@/components/shared/form-toolbar"
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { useTreasuryWithdrawals, type StatusFilter } from "@/configuration/crypto/hooks/useTreasuryWithdrawals"
import { truncateAddress } from "@/configuration/crypto/lib/blockchain"
import { formatDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import {
  Loader2,
  RefreshCw,
  ShieldCheck
} from "lucide-react"

export default function TreasuryWithdrawalsPage() {
  const { changeIncident, closeOutflow, changeField, router, compact, title, canCreateTreasuryWithdrawal, approveActionLabel, rejectActionLabel, activeTab, setActiveTab, tokens, sourceWallets, withdrawals, outflows, availability, statusFilter, setStatusFilter, tokenId, sourceWalletId, addressId, amount, description, newAddress, newAddressLabel, loading, saving, actionID, signingJobs, signingJobLoadingID, selectedOutflowID, outflowDetail, outflowDetailLoading, incidentAction, classification, incidentReason, matchedBasisType, matchedBasisId, evidence, filteredAddresses, selectedWallet, normalizedAmount, amountExceedsWallet, amountExceedsRevenue, canCreateWithdrawal, canCreateAddress, formatTokenAmount, loadLists, handleCreateAddress, handleCreateWithdrawal, handleApprove, handleLoadSigningJob, handleRetrySigning, handleReject, copyAddress, openOutflow, handleClassifyOutflow, handleAdjustmentDecision, handleResolveOutflow } = useTreasuryWithdrawals()

  return (
    <div className="flex h-full flex-col">
      <FormToolbar
        title={title}
        primaryAction={canCreateTreasuryWithdrawal ? {
          label: saving ? "Создание..." : "Создать вывод",
          onClick: handleCreateWithdrawal,
          disabled: !canCreateWithdrawal,
        } : undefined}
        secondaryActions={[
          { label: "Обновить", onClick: loadLists, disabled: loading },
        ]}
        extraMenuItems={[
          { label: "Открыть журнал документов", onClick: () => router.push("/documents/treasury-withdrawals") },
        ]}
        backHref="/documents/treasury-withdrawals"
        onClose={() => router.push("/documents/treasury-withdrawals")}
      >
        <Select value={statusFilter} onValueChange={(value) => setStatusFilter(value as StatusFilter)}>
          <SelectTrigger className="h-8 w-[180px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="pending_signature">Ожидают подписи</SelectItem>
            <SelectItem value="pending_approval">На одобрении</SelectItem>
            <SelectItem value="all">Все статусы</SelectItem>
            <SelectItem value="approved">Одобрены</SelectItem>
            <SelectItem value="signing">Подписание</SelectItem>
            <SelectItem value="broadcast">Отправлены</SelectItem>
            <SelectItem value="held">На сверке</SelectItem>
          </SelectContent>
        </Select>
      </FormToolbar>

      <div className="flex flex-1 overflow-hidden">
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
          <div className={cn("shrink-0 border-b bg-card", compact ? "p-2" : "p-4")}>
            <div className="grid gap-3 lg:grid-cols-3">
              <Metric
                label="Platform revenue"
                value={availability ? formatTokenAmount(availability.available) : "—"}
                detail={availability ? `${formatTokenAmount(availability.amount)} всего; ${formatTokenAmount(availability.reserved)} зарезервировано` : "Нет данных по токену"}
                loading={loading && !availability}
              />
              <Metric
                label="Hot wallet"
                value={selectedWallet ? formatTokenAmount(selectedWallet.availableAmount) : "—"}
                detail={selectedWallet ? `${truncateAddress(selectedWallet.address)}; ${formatTokenAmount(selectedWallet.reservedAmount)} зарезервировано` : "Выберите исходный кошелёк"}
                loading={loading && sourceWallets.length === 0}
              />
              <Metric
                label="Сверка с сетью"
                value={selectedWallet?.lastChainAmount !== undefined ? formatTokenAmount(selectedWallet.lastChainAmount) : "—"}
                detail={selectedWallet?.lastReconciliationAt ? formatDateTime(selectedWallet.lastReconciliationAt) : "Открытых расхождений нет"}
                loading={loading && sourceWallets.length === 0}
              />
            </div>

            <div className={cn("grid gap-x-6 md:grid-cols-2 lg:grid-cols-4", compact ? "mt-3 gap-y-2" : "mt-4 gap-y-3")}>
              <FormField label="Токен">
                <Select value={tokenId} onValueChange={(value) => changeField("tokenId", value)}>
                  <SelectTrigger className={compact ? "h-8" : undefined}>
                    <SelectValue placeholder="Выберите токен" />
                  </SelectTrigger>
                  <SelectContent>
                    {tokens.map((token) => (
                      <SelectItem key={token.id} value={token.id}>
                        {token.symbol || token.code} · {token.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </FormField>

              <FormField label="Исходный кошелёк">
                <Select value={sourceWalletId} onValueChange={(value) => changeField("sourceWalletId", value)}>
                  <SelectTrigger className={compact ? "h-8" : undefined}>
                    <SelectValue placeholder="Выберите кошелёк" />
                  </SelectTrigger>
                  <SelectContent>
                    {sourceWallets.map((wallet) => (
                      <SelectItem key={wallet.id} value={wallet.id}>
                        {wallet.name} · {truncateAddress(wallet.address)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </FormField>

              <FormField label="Адрес назначения">
                <Select value={addressId} onValueChange={(value) => changeField("addressId", value)}>
                  <SelectTrigger className={compact ? "h-8" : undefined}>
                    <SelectValue placeholder="Выберите адрес" />
                  </SelectTrigger>
                  <SelectContent>
                    {filteredAddresses.map((address) => (
                      <SelectItem key={address.id} value={address.id}>
                        {address.label || truncateAddress(address.address)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </FormField>

              <FormField label="Сумма">
                <div className="flex flex-col gap-1">
                  <Input
                    className={cn("text-right font-mono tabular-nums", compact && "h-8")}
                    inputMode="decimal"
                    value={amount}
                    onChange={(event) => changeField("amount", event.target.value)}
                    onBlur={() => changeField("amount", normalizedAmount)}
                  />
                  {(amountExceedsWallet || amountExceedsRevenue) && (
                    <div className="text-xs text-destructive">
                      {amountExceedsWallet ? "Больше остатка hot wallet" : "Больше доступной revenue"}
                    </div>
                  )}
                </div>
              </FormField>

              <FormField label="Комментарий" className="md:col-span-2 lg:col-span-4">
                <Textarea
                  className="min-h-16"
                  value={description}
                  onChange={(event) => changeField("description", event.target.value)}
                />
              </FormField>
            </div>
          </div>

          <Tabs value={activeTab} onValueChange={setActiveTab} className="flex min-h-0 flex-1 flex-col">
            <div className="flex items-center border-b bg-card px-4">
              <TabsList variant="line" className={cn("border-b-0", compact ? "h-9" : "h-11")}>
                <TabsTrigger value="withdrawals" variant="line" className="text-xs">
                  Выводы ({withdrawals.length})
                </TabsTrigger>
                <TabsTrigger value="allowlist" variant="line" className="text-xs">
                  Allowlist ({filteredAddresses.length})
                </TabsTrigger>
                <TabsTrigger value="reconciliation" variant="line" className="text-xs">
                  Сверка ({outflows.length})
                </TabsTrigger>
              </TabsList>
              <Button variant="ghost" size="icon" className="ml-auto h-8 w-8" onClick={loadLists} disabled={loading} title="Обновить">
                <RefreshCw className={cn(loading && "animate-spin")} />
              </Button>
            </div>

            <TabsContent value="withdrawals" className="m-0 min-h-0 flex-1 overflow-hidden">
              <ScrollArea className="h-full">
                <WithdrawalsTable
                  items={withdrawals}
                  loading={loading}
                  actionID={actionID}
                  signingJobs={signingJobs}
                  signingJobLoadingID={signingJobLoadingID}
                  formatTokenAmount={formatTokenAmount}
                  onApprove={handleApprove}
                  onReject={handleReject}
                  onLoadSigningJob={handleLoadSigningJob}
                  onRetrySigning={handleRetrySigning}
                  onCopy={copyAddress}
                  approveLabel={approveActionLabel}
                  rejectLabel={rejectActionLabel}
                />
              </ScrollArea>
            </TabsContent>

            <TabsContent value="allowlist" className="m-0 min-h-0 flex-1 overflow-hidden">
              <ScrollArea className="h-full">
                <div className={cn("border-b bg-card", compact ? "p-2" : "p-4")}>
                  <div className="grid gap-x-4 gap-y-3 lg:grid-cols-[minmax(320px,1fr)_minmax(220px,360px)_auto]">
                    <FormField label="Адрес">
                      <Input
                        className={compact ? "h-8" : undefined}
                        value={newAddress}
                        onChange={(event) => changeField("newAddress", event.target.value)}
                      />
                    </FormField>
                    <FormField label="Метка">
                      <Input
                        className={compact ? "h-8" : undefined}
                        value={newAddressLabel}
                        onChange={(event) => changeField("newAddressLabel", event.target.value)}
                      />
                    </FormField>
                    <div className="flex items-end">
                      <Button variant="outline" size="sm" onClick={handleCreateAddress} disabled={!canCreateAddress}>
                        {saving ? <Loader2 className="animate-spin" /> : <ShieldCheck />}
                        Добавить
                      </Button>
                    </div>
                  </div>
                </div>
                <AllowlistTable items={filteredAddresses} onCopy={copyAddress} />
              </ScrollArea>
            </TabsContent>

            <TabsContent value="reconciliation" className="m-0 min-h-0 flex-1 overflow-hidden">
              <ScrollArea className="h-full">
                <OutflowsTable
                  items={outflows}
                  tokenId={tokenId}
                  formatTokenAmount={formatTokenAmount}
                  onCopy={copyAddress}
                  onSelect={openOutflow}
                />
              </ScrollArea>
            </TabsContent>
          </Tabs>
        </div>
      </div>
      <OutflowDetailSheet
        open={selectedOutflowID !== null}
        loading={outflowDetailLoading}
        detail={outflowDetail}
        classification={classification}
        reason={incidentReason}
        matchedBasisType={matchedBasisType}
        matchedBasisId={matchedBasisId}
        evidence={evidence}
        actionPending={incidentAction}
        formatTokenAmount={formatTokenAmount}
        onOpenChange={(open) => { if (!open) closeOutflow() }}
        onClassificationChange={(value) => changeIncident({ classification: value })}
        onReasonChange={(value) => changeIncident({ incidentReason: value })}
        onMatchedBasisTypeChange={(value) => changeIncident({ matchedBasisType: value })}
        onMatchedBasisIdChange={(value) => changeIncident({ matchedBasisId: value })}
        onEvidenceChange={(value) => changeIncident({ evidence: value })}
        onClassify={handleClassifyOutflow}
        onApproveAdjustment={() => handleAdjustmentDecision("approve")}
        onRejectAdjustment={() => handleAdjustmentDecision("reject")}
        onResolve={handleResolveOutflow}
      />
    </div>
  )
}
