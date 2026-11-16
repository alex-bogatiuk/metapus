"use client"

import { useLatestRequest } from "@/hooks/useLatestRequest"
import { UUID_PATH_PARAM_RE } from "@/lib/api"
import { jsonObjectSchema, type JsonObject } from "@/lib/json-value"
import type {
  ExternalOutflow,
  ExternalOutflowClassification,
  ExternalOutflowDetail
} from "@/configuration/crypto/types/treasury"
import { useSearchParams } from "next/navigation"
import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"

import { cryptoApi } from "@/configuration/crypto/lib/api"
interface IncidentDraft {
  classification: ExternalOutflowClassification
  incidentReason: string
  matchedBasisType: string
  matchedBasisId: string
  evidence: string
}
const EMPTY_INCIDENT: IncidentDraft = { classification: "unauthorized_loss", incidentReason: "", matchedBasisType: "", matchedBasisId: "", evidence: "" }

export function useTreasuryReconciliation(loadLists: () => Promise<void>) {
  const requests = useLatestRequest()
  const searchParams = useSearchParams()
  const outflowId = searchParams.get("outflow")
  const [draft, setDraft] = useState(EMPTY_INCIDENT)
  const { classification, incidentReason, matchedBasisType, matchedBasisId, evidence } = draft
  const changeIncident = (patch: Partial<IncidentDraft>) => setDraft(previous => ({ ...previous, ...patch }))
  const [selectedOutflowID, setSelectedOutflowID] = useState<string | null>(null)
  const [outflowDetail, setOutflowDetail] = useState<ExternalOutflowDetail | null>(null)
  const [outflowDetailLoading, setOutflowDetailLoading] = useState(false)
  const [incidentAction, setIncidentAction] = useState(false)
  const openOutflow = useCallback(async (outflow: Pick<ExternalOutflow, "id" | "classification">) => {
    const isCurrent = requests.begin()
    setSelectedOutflowID(outflow.id)
    setOutflowDetail(null)
    setOutflowDetailLoading(true)
    setIncidentAction(false)
    setDraft({ ...EMPTY_INCIDENT, classification: outflow.classification || "unauthorized_loss" })
    try {
      const detail = await cryptoApi.treasuryWithdrawals.reconciliation.getOutflow(outflow.id)
      if (isCurrent()) setOutflowDetail(detail)
    } catch (err) {
      if (!isCurrent()) return
      toast.error(err instanceof Error ? err.message : "Не удалось загрузить инцидент")
      setSelectedOutflowID(null)
    } finally {
      if (isCurrent()) setOutflowDetailLoading(false)
    }
  }, [requests])

  const refreshOutflowDetail = async () => {
    if (!selectedOutflowID) return
    const isCurrent = requests.capture()
    const detail = await cryptoApi.treasuryWithdrawals.reconciliation.getOutflow(selectedOutflowID)
    if (isCurrent()) setOutflowDetail(detail)
  }

  const handleClassifyOutflow = async () => {
    if (!selectedOutflowID || !incidentReason.trim()) {
      toast.error("Укажите обязательную причину классификации")
      return
    }
    if (classification === "matched_basis" && (!matchedBasisType.trim() || !matchedBasisId.trim())) {
      toast.error("Укажите тип и ID неизменяемого основания")
      return
    }
    let parsedEvidence: JsonObject | undefined
    if (evidence.trim()) {
      try {
        parsedEvidence = jsonObjectSchema.parse(JSON.parse(evidence))
      } catch {
        toast.error("Evidence должен быть корректным JSON-объектом")
        return
      }
    }
    const isCurrent = requests.capture()
    setIncidentAction(true)
    try {
      const detail = await cryptoApi.treasuryWithdrawals.reconciliation.classifyOutflow(selectedOutflowID, {
        classification,
        matchedBasisType: matchedBasisType.trim() || undefined,
        matchedBasisId: matchedBasisId.trim() || undefined,
        reason: incidentReason.trim(),
        evidence: parsedEvidence,
      })
      if (!isCurrent()) return
      setOutflowDetail(detail)
      changeIncident({ incidentReason: "" })
      toast.success("Внешнее списание классифицировано")
      void loadLists()
    } catch (err) {
      if (isCurrent()) toast.error(err instanceof Error ? err.message : "Не удалось классифицировать списание")
    } finally {
      if (isCurrent()) setIncidentAction(false)
    }
  }

  const handleAdjustmentDecision = async (decision: "approve" | "reject") => {
    const adjustmentID = outflowDetail?.adjustment?.id
    if (!adjustmentID || !incidentReason.trim()) {
      toast.error("Укажите обязательную причину решения")
      return
    }
    const isCurrent = requests.capture()
    setIncidentAction(true)
    try {
      if (decision === "approve") {
        await cryptoApi.treasuryWithdrawals.reconciliation.approveCustodyAdjustment(adjustmentID, incidentReason.trim())
      } else {
        await cryptoApi.treasuryWithdrawals.reconciliation.rejectCustodyAdjustment(adjustmentID, incidentReason.trim())
      }
      if (!isCurrent()) return
      await refreshOutflowDetail()
      if (!isCurrent()) return
      changeIncident({ incidentReason: "" })
      toast.success(decision === "approve" ? "Custody adjustment проведён" : "Custody adjustment отклонён")
      void loadLists()
    } catch (err) {
      if (isCurrent()) toast.error(err instanceof Error ? err.message : "Не удалось сохранить решение")
    } finally {
      if (isCurrent()) setIncidentAction(false)
    }
  }

  const handleResolveOutflow = async () => {
    if (!selectedOutflowID || !incidentReason.trim()) {
      toast.error("Укажите обязательную причину resolution")
      return
    }
    const isCurrent = requests.capture()
    setIncidentAction(true)
    try {
      const detail = await cryptoApi.treasuryWithdrawals.reconciliation.resolveOutflow(selectedOutflowID, incidentReason.trim())
      if (!isCurrent()) return
      setOutflowDetail(detail)
      changeIncident({ incidentReason: "" })
      toast.success("Инцидент закрыт, принадлежащие ему holds сняты")
      void loadLists()
    } catch (err) {
      if (isCurrent()) toast.error(err instanceof Error ? err.message : "Не удалось закрыть инцидент")
    } finally {
      if (isCurrent()) setIncidentAction(false)
    }
  }
  useEffect(() => {
    if (outflowId && UUID_PATH_PARAM_RE.test(outflowId)) void openOutflow({ id: outflowId })
    return () => requests.invalidate()
  }, [outflowId, openOutflow, requests])

  const closeOutflow = () => {
    requests.invalidate()
    setSelectedOutflowID(null)
    setOutflowDetail(null)
    setOutflowDetailLoading(false)
    setIncidentAction(false)
  }

  return { changeIncident, closeOutflow, selectedOutflowID, outflowDetail, outflowDetailLoading, incidentAction, classification, incidentReason, matchedBasisType, matchedBasisId, evidence, openOutflow, handleClassifyOutflow, handleAdjustmentDecision, handleResolveOutflow }
}
