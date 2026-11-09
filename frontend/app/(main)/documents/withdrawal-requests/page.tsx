"use client"

import AutoList from "@/components/shared/auto-list"
import { WithdrawalRequestSelectionPanel } from "@/components/documents/withdrawal-request-selection-panel"

export default function WithdrawalRequestsPage() {
  return (
    <AutoList
      entityName="WithdrawalRequest"
      entityType="document"
      routePrefix="withdrawal-request"
      renderSelectionPanel={(item, refresh) => (
        <WithdrawalRequestSelectionPanel item={item} refresh={refresh} />
      )}
    />
  )
}
