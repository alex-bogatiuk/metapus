import { useEffect } from "react"
import { usePathname } from "next/navigation"
import { useTabsStore } from "@/stores/useTabsStore"

/**
 * Updates the tab title for the current page.
 * Call with the entity name after data is loaded.
 *
 * @param entityName — display name of the entity (e.g. "Бумага офисная А4")
 * @param metadataType — human-readable type label (e.g. "Номенклатура")
 */
export function useTabTitle(entityName: string | undefined, metadataType: string) {
    const pathname = usePathname()
    const updateTabTitle = useTabsStore((s) => s.updateTabTitle)

    useEffect(() => {
        if (!entityName) return
        const title = `${entityName} (${metadataType})`
        updateTabTitle(pathname, title)
    }, [entityName, metadataType, pathname, updateTabTitle])
}
