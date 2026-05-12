import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { PortalMerchantItem } from "@/types/portal-api"

interface PortalState {
  activeMerchantId: string | null
  merchants: PortalMerchantItem[]
}

interface PortalActions {
  setActiveMerchant: (id: string) => void
  setMerchants: (items: PortalMerchantItem[]) => void
  reset: () => void
}

export const usePortalStore = create<PortalState & PortalActions>()(
  persist(
    (set) => ({
      activeMerchantId: null,
      merchants: [],

      setActiveMerchant: (id) =>
        set({ activeMerchantId: id }),

      setMerchants: (items) =>
        set((state) => ({
          merchants: items,
          // If no active merchant is set, default to the first one
          activeMerchantId: state.activeMerchantId
            && items.some((m) => m.id === state.activeMerchantId)
            ? state.activeMerchantId
            : items[0]?.id ?? null,
        })),

      reset: () =>
        set({ activeMerchantId: null, merchants: [] }),
    }),
    {
      name: "metapus-portal",
      partialize: (state) => ({
        activeMerchantId: state.activeMerchantId,
      }),
    }
  )
)
