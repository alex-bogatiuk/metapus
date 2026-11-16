import { SCOPED_STORAGE_PREFIX, registerScopedStore } from "@/lib/auth-scope"
import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { PortalMerchantItem } from "@/configuration/crypto/types/portal-api"

interface PortalState {
  activeMerchantId: string | null
  merchants: PortalMerchantItem[]
}

interface PortalActions {
  setActiveMerchant: (id: string) => void
  setMerchants: (items: PortalMerchantItem[]) => void
  reset: () => void
}

// Before the scoped prefix the store persisted under this key; drop what is left.
try { if (typeof localStorage !== "undefined") localStorage.removeItem("metapus-portal") } catch { /* storage disabled */ }

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
      name: `${SCOPED_STORAGE_PREFIX}portal`,
      partialize: (state) => ({
        activeMerchantId: state.activeMerchantId,
      }),
    }
  )
)

registerScopedStore(usePortalStore)
