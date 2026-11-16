"use client"

import { type SigningJobStatus } from "@/configuration/crypto/components/documents/treasury-withdrawals/withdrawals-table"
import { useCompactMode } from "@/hooks/useCompactMode"
import { useFormDraft } from "@/hooks/useFormDraft"
import { useLatestRequest } from "@/hooks/useLatestRequest"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useTreasuryReconciliation } from "@/configuration/crypto/hooks/useTreasuryReconciliation"

import { compareCryptoAmounts, copyToClipboard, formatMinorUnits, toCryptoMinorUnits } from "@/lib/format"
import { createFormDraftKey } from "@/stores/useFormDraftStore"
import { useMetadataStore } from "@/stores/useMetadataStore"
import type { AdvancedFilterItem } from "@/types/common"
import type {
  ExternalOutflow,
  TreasuryAddress,
  TreasuryAvailability,
  TreasurySourceWallet,
  TreasuryTokenOption,
  TreasuryWithdrawal,
  TreasuryWithdrawalStatus
} from "@/configuration/crypto/types/treasury"
import { usePathname, useRouter } from "next/navigation"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { toast } from "sonner"

import { cryptoApi } from "@/configuration/crypto/lib/api"
export type StatusFilter = "all" | TreasuryWithdrawalStatus

interface WithdrawalDraft {
  tokenId: string
  sourceWalletId: string
  addressId: string
  amount: string
  description: string
  newAddress: string
  newAddressLabel: string
}
const withdrawalDraftKey = createFormDraftKey<WithdrawalDraft>("treasury.withdrawal")
const EMPTY_DRAFT: WithdrawalDraft = {
  tokenId: "", sourceWalletId: "", addressId: "", amount: "", description: "", newAddress: "", newAddressLabel: "",
}

export function useTreasuryWithdrawals() {
  const { state: draft, setField } = useFormDraft(usePathname(), withdrawalDraftKey, EMPTY_DRAFT)
  const { tokenId, sourceWalletId, addressId, amount, description, newAddress, newAddressLabel } = draft
  const { markDirty, markClean } = useTabDirty()
  const formRevision = useRef(0)
  const changeField = <K extends keyof WithdrawalDraft>(field: K, value: WithdrawalDraft[K]) => {
    formRevision.current++
    setField(field, value)
    markDirty()
  }
  const listRequests = useLatestRequest()
  const tokenRequests = useLatestRequest()
  const router = useRouter()
  const compact = useCompactMode()
  const entityMeta = useMetadataStore((s) => s.getEntity("treasury_withdrawal"))
  const title = entityMeta?.presentation.plural ?? "Казначейские выводы"
  const canCreateTreasuryWithdrawal = entityMeta?.capabilities?.create !== false
  const approveActionLabel = entityMeta?.actions?.find((action) => action.key === "approve")?.label ?? "Одобрить"
  const rejectActionLabel = entityMeta?.actions?.find((action) => action.key === "reject")?.label ?? "Отклонить"
  const [activeTab, setActiveTab] = useState("withdrawals")
  useEffect(() => {
    const query = new URLSearchParams(window.location.search)
    if (query.get("tab") === "reconciliation" || query.get("outflow")) setActiveTab("reconciliation")
  }, [])

  const [tokens, setTokens] = useState<TreasuryTokenOption[]>([])
  const [addresses, setAddresses] = useState<TreasuryAddress[]>([])
  const [sourceWallets, setSourceWallets] = useState<TreasurySourceWallet[]>([])
  const [withdrawals, setWithdrawals] = useState<TreasuryWithdrawal[]>([])
  const [outflows, setOutflows] = useState<ExternalOutflow[]>([])
  const [availability, setAvailability] = useState<TreasuryAvailability | null>(null)
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all")
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [actionID, setActionID] = useState<string | null>(null)
  const [signingJobs, setSigningJobs] = useState<Record<string, SigningJobStatus>>({})
  const [signingJobLoadingID, setSigningJobLoadingID] = useState<string | null>(null)
  const tokenByID = useMemo(() => new Map(tokens.map((token) => [token.id, token])), [tokens])
  const selectedToken = tokenByID.get(tokenId)
  const filteredAddresses = useMemo(
    () => addresses.filter((address) => address.isActive && (!selectedToken || address.networkId === selectedToken.networkId)),
    [addresses, selectedToken],
  )

  const selectedWallet = sourceWallets.find((wallet) => wallet.id === sourceWalletId)
  const selectedAddress = filteredAddresses.find((address) => address.id === addressId)
  const normalizedAmount = amount.trim().replace(",", ".")
  const amountMinor = selectedToken
    ? toCryptoMinorUnits(normalizedAmount, selectedToken.decimalPlaces)
    : null

  const amountExceedsWallet = !!selectedWallet && amountMinor !== null && compareCryptoAmounts(amountMinor, selectedWallet.availableAmount) > 0
  const amountExceedsRevenue = !!availability && amountMinor !== null && compareCryptoAmounts(amountMinor, availability.available) > 0
  const canCreateWithdrawal = !!selectedToken
    && !!sourceWalletId
    && !!selectedAddress
    && amountMinor !== null
    && amountMinor !== "0"
    && !amountExceedsWallet
    && !amountExceedsRevenue
    && availability?.tokenId === tokenId
    && canCreateTreasuryWithdrawal
    && !saving

  const canCreateAddress = !!selectedToken && !!newAddress.trim() && !saving
  const formatTokenAmount = useCallback(
    (minor: string, itemTokenId = tokenId) => {
      const token = tokenByID.get(itemTokenId)
      return `${formatMinorUnits(String(minor), token?.decimalPlaces ?? 6)} ${token?.symbol || token?.code || ""}`.trim()
    },
    [tokenByID, tokenId],
  )

  const loadLists = useCallback(async () => {
    const isCurrent = listRequests.begin()
    setLoading(true)
    try {
      const withdrawalFilters: AdvancedFilterItem[] = statusFilter === "all"
        ? []
        : [{ field: "status", fieldType: "string", operator: "eq", value: statusFilter }]
      const [tokenRes, addressRes, withdrawalRes, outflowRes] = await Promise.all([
        cryptoApi.tokens.list({ limit: 500 }),
        cryptoApi.treasuryWithdrawals.addresses.list(),
        cryptoApi.treasuryWithdrawals.list({ limit: 200, orderBy: "-date", filter: withdrawalFilters }),
        cryptoApi.treasuryWithdrawals.reconciliation.outflows({ limit: 100 }),
      ])
      if (!isCurrent()) return
      const activeTokens = tokenRes.items.filter((token) => !token.deletionMark)
      setTokens(activeTokens)
      setAddresses(addressRes.items)
      setWithdrawals(withdrawalRes.items)
      setSigningJobs({})
      setOutflows(outflowRes.items)
      setField("tokenId", (current) => current || activeTokens[0]?.id || "")
    } catch (err) {
      if (isCurrent()) toast.error(err instanceof Error ? err.message : "Не удалось загрузить казначейские данные")
    } finally {
      if (isCurrent()) setLoading(false)
    }
  }, [statusFilter, listRequests, setField])

  const loadTokenState = useCallback(async () => {
    const isCurrent = tokenRequests.begin()
    setAvailability(null)
    setSourceWallets([])
    if (!tokenId) {
      setAvailability(null)
      setSourceWallets([])
      return
    }
    try {
      const [availabilityRes, walletsRes] = await Promise.all([
        cryptoApi.treasuryWithdrawals.availability(tokenId),
        cryptoApi.treasuryWithdrawals.sourceWallets(tokenId),
      ])
      if (!isCurrent()) return
      setAvailability(availabilityRes)
      setSourceWallets(walletsRes.items)
      setField("sourceWalletId", (current) => walletsRes.items.some((wallet) => wallet.id === current) ? current : walletsRes.items[0]?.id || "")

    } catch (err) {
      if (!isCurrent()) return
      toast.error(err instanceof Error ? err.message : "Не удалось загрузить остатки")
      setAvailability(null)
      setSourceWallets([])
    }
  }, [tokenId, tokenRequests, setField])

  useEffect(() => {
    void loadLists()
    return () => listRequests.invalidate()
  }, [loadLists, listRequests])

  useEffect(() => {
    void loadTokenState()
    return () => tokenRequests.invalidate()
  }, [loadTokenState, tokenRequests])

  useEffect(() => {
    setField("addressId", (current) => filteredAddresses.some((address) => address.id === current) ? current : filteredAddresses[0]?.id || "")
  }, [filteredAddresses, setField])

  const handleCreateAddress = async () => {
    if (!selectedToken || !newAddress.trim()) {
      toast.error("Укажите адрес назначения")
      return
    }
    setSaving(true)
    try {
      await cryptoApi.treasuryWithdrawals.addresses.create({
        networkId: selectedToken.networkId,
        address: newAddress.trim(),
        label: newAddressLabel.trim(),
      })
      setField("newAddress", "")
      setField("newAddressLabel", "")
      toast.success("Адрес добавлен")
      await loadLists()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Не удалось добавить адрес")
    } finally {
      setSaving(false)
    }
  }

  const handleCreateWithdrawal = async () => {
    if (!selectedToken || !sourceWalletId || !selectedAddress) {
      toast.error("Заполните токен, кошелёк и адрес")
      return
    }
    if (amountMinor === null || amountMinor === "0") {
      toast.error("Сумма должна быть больше нуля")
      return
    }
    if (amountExceedsWallet) {
      toast.error("Сумма больше доступного остатка hot wallet")
      return
    }
    if (amountExceedsRevenue) {
      toast.error("Сумма больше доступной platform revenue")
      return
    }
    const savedRevision = formRevision.current
    setSaving(true)
    try {
      await cryptoApi.treasuryWithdrawals.create({
        tokenId,
        sourceWalletId,
        addressId,
        destAddress: selectedAddress.address,
        amount: amountMinor,
        description: description.trim(),
      })
      if (formRevision.current === savedRevision) {
        setField("amount", "")
        setField("description", "")
        markClean()
      }
      toast.success("Казначейский вывод создан")
      await Promise.all([loadLists(), loadTokenState()])
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Не удалось создать казначейский вывод")
    } finally {
      setSaving(false)
    }
  }

  const handleApprove = async (id: string) => {
    setActionID(id)
    try {
      await cryptoApi.treasuryWithdrawals.approve(id)
      toast.success("Казначейский вывод одобрен")
      await Promise.all([loadLists(), loadTokenState()])
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Не удалось одобрить")
    } finally {
      setActionID(null)
    }
  }

  const handleLoadSigningJob = async (id: string) => {
    setSigningJobLoadingID(id)
    try {
      const job = await cryptoApi.treasuryWithdrawals.signingJob(id)
      setSigningJobs((current) => ({ ...current, [id]: job }))
      if (job.status !== "needs_review") toast.info("Задание подписи: " + job.status)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Не удалось проверить задание подписи")
    } finally {
      setSigningJobLoadingID(null)
    }
  }

  const handleRetrySigning = async (id: string) => {
    if (signingJobs[id]?.status !== "needs_review") return
    setActionID(id)
    try {
      await cryptoApi.treasuryWithdrawals.retrySigning(id)
      toast.success("Задание подписи снова в очереди")
      await loadLists()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Не удалось повторить подписание")
      await handleLoadSigningJob(id)
    } finally {
      setActionID(null)
    }
  }

  const handleReject = async (id: string) => {
    setActionID(id)
    try {
      await cryptoApi.treasuryWithdrawals.reject(id)
      toast.success("Казначейский вывод отклонён")
      await Promise.all([loadLists(), loadTokenState()])
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Не удалось отклонить")
    } finally {
      setActionID(null)
    }
  }

  const copyAddress = (address: string) => {
    copyToClipboard(address)
    toast.success("Адрес скопирован")
  }
  const { changeIncident, closeOutflow, selectedOutflowID, outflowDetail, outflowDetailLoading, incidentAction, classification, incidentReason, matchedBasisType, matchedBasisId, evidence, openOutflow, handleClassifyOutflow, handleAdjustmentDecision, handleResolveOutflow } = useTreasuryReconciliation(loadLists)

  return { changeIncident, closeOutflow, changeField, router, compact, title, canCreateTreasuryWithdrawal, approveActionLabel, rejectActionLabel, activeTab, setActiveTab, tokens, sourceWallets, withdrawals, outflows, availability, statusFilter, setStatusFilter, tokenId, sourceWalletId, addressId, amount, description, newAddress, newAddressLabel, loading, saving, actionID, signingJobs, signingJobLoadingID, selectedOutflowID, outflowDetail, outflowDetailLoading, incidentAction, classification, incidentReason, matchedBasisType, matchedBasisId, evidence, filteredAddresses, selectedWallet, normalizedAmount, amountExceedsWallet, amountExceedsRevenue, canCreateWithdrawal, canCreateAddress, formatTokenAmount, loadLists, handleCreateAddress, handleCreateWithdrawal, handleApprove, handleLoadSigningJob, handleRetrySigning, handleReject, copyAddress, openOutflow, handleClassifyOutflow, handleAdjustmentDecision, handleResolveOutflow }
}
