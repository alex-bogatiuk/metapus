"use client"

import { useCallback, useMemo } from "react"
import { useIntegrationProviders } from "@/hooks/useIntegrationProviders"
import { integrationListParams } from "@/lib/integrations/provider-filters"
import type { AutoListColumn } from "@/lib/entity-registry"
import type { CursorListParams } from "@/types/common"
import type { FilterFieldMeta } from "@/components/shared/filter-config-dialog"
import AutoList from "@/components/shared/auto-list"
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"

interface IntegrationListProps { entityName: string }

export default function IntegrationList({ entityName }: IntegrationListProps) {
  const registry = useIntegrationProviders()
  const columns = useMemo<AutoListColumn[]>(() => [{
    key: "integrationKind", label: "Назначение", sortable: false, width: 230,
    render: item => {
      const provider = registry.providers.find(p => p.id === item.provider)
      return registry.kinds.filter(kind => provider?.kinds.includes(kind.id)).map(kind => kind.label).join(", ")
    },
  }], [registry.providers, registry.kinds])
  const filters = useMemo<FilterFieldMeta[]>(() => [{
    key: "integrationKind", label: "Назначение", fieldType: "enum",
    enumValues: registry.kinds.map(kind => ({ value: kind.id, label: kind.label })),
  }], [registry.kinds])
  const transformParams = useCallback((params?: CursorListParams) => integrationListParams(params, registry.providers), [registry.providers])
  if (registry.isLoading) return <Skeleton className="h-48" />
  if (!registry.data) return <Alert variant="destructive"><AlertTitle>Не удалось загрузить провайдеров</AlertTitle><AlertDescription><Button variant="outline" onClick={() => void registry.mutate()}>Повторить загрузку</Button></AlertDescription></Alert>
  return <AutoList entityName={entityName} entityType="catalog" routePrefix={entityName === "IntegrationAccount" ? "integration-accounts" : "integration-pools"}
    additionalColumns={columns} additionalFilterFields={filters} transformParams={transformParams} />
}
