import type { AdvancedFilterItem, CursorListParams } from "@/types/common"
import type { IntegrationProviderResponse } from "@/types/generated/http-contracts"

// Expand a derived purpose into a provider predicate before requesting a page.
// Filtering and counting still happen in SQL, before cursor pagination.
export function integrationListParams(params: CursorListParams | undefined, providers: readonly IntegrationProviderResponse[]): CursorListParams | undefined {
  if (!params?.filter?.length) return params
  const filter = params.filter.map((item): AdvancedFilterItem => {
    if (item.field !== "integration_kind" && item.field !== "integrationKind") return item
    const values = Array.isArray(item.value) ? item.value : [item.value]
    const matches = (provider: IntegrationProviderResponse) => provider.kinds.some(kind => values.includes(kind))
    const ids = providers.filter(provider => {
      switch (item.operator) {
        case "eq": case "in": return matches(provider)
        case "neq": case "nin": return !matches(provider)
        case "null": return !provider.kinds.length
        case "not_null": return !!provider.kinds.length
        default: throw new Error("Недопустимое условие фильтра по назначению интеграции.")
      }
    }).map(provider => provider.id)
    return { field: "provider", operator: "in", value: ids }
  })
  return { ...params, filter }
}
