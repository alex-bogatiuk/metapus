"use client";
import useSWR from "swr";
import { api } from "@/lib/api";
import { scopeKey } from "@/lib/auth-scope";
import { useAuthStore } from "@/stores/useAuthStore";
import { usePortalStore } from "@/configuration/crypto/stores/usePortalStore";

export function usePortalInvoiceDetail(invoiceId: string) {
  const scope = useAuthStore((state) => state.cacheScope);
  const merchantId = usePortalStore((state) => state.activeMerchantId);
  const merchants = usePortalStore((state) => state.merchants);
  const enabled =
    !!scope &&
    !!merchantId &&
    merchants.some((merchant) => merchant.id === merchantId);
  const key = enabled
    ? ([
        scopeKey(scope),
        "portal-invoice-detail",
        merchantId!,
        invoiceId,
      ] as const)
    : null;
  const result = useSWR(
    key,
    ([, , selectedMerchant, selectedInvoice]) =>
      api.portal.invoiceDetail(selectedInvoice, selectedMerchant),
    {
      revalidateOnMount: true,
      revalidateIfStale: false,
      revalidateOnFocus: false,
      revalidateOnReconnect: false,
      refreshInterval: 0,
      shouldRetryOnError: false,
      keepPreviousData: false,
    },
  );
  return {
    ...result,
    loading: !enabled || result.isLoading,
    contextKey: JSON.stringify(key),
    refresh: () => result.mutate().catch(() => undefined),
  };
}
