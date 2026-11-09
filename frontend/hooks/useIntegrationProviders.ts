"use client"

import useSWR from "swr"
import { integrationRuntime } from "@/lib/api/integrations"
import type { IntegrationProviderResponse, IntegrationKindResponse } from "@/types/generated/http-contracts"

const noProviders: IntegrationProviderResponse[] = []
const noKinds: IntegrationKindResponse[] = []

export function useIntegrationProviders() {
  const result = useSWR("integration-provider-definitions", integrationRuntime.providers, { revalidateOnFocus: false })
  return { ...result, providers: result.data?.providers ?? noProviders, kinds: result.data?.kinds ?? noKinds }
}
