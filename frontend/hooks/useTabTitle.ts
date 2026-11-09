import { useEffect } from "react"
import { usePathname } from "next/navigation"
import { useTabsStore } from "@/stores/useTabsStore"

/**
 * Updates the tab title for the current page.
 * Call with the entity name after data is loaded.
 *
 * @param entityName — display name of the entity (e.g. "Tether USD")
 * @param metadataType — human-readable type label (e.g. "Токен")
 * @param mode — keep creation visible until the form navigates to a saved entity
 * @returns The resolved title, also usable in the form toolbar.
 */
export function useTabTitle(entityName: string | undefined, metadataType: string, mode: "edit" | "create" | "copy" = "edit") {
    const pathname = usePathname()
    const updateTabTitle = useTabsStore((s) => s.updateTabTitle)
    const currentTitle = useTabsStore((s) => s.tabs.find((tab) => tab.id === pathname)?.title)
    const prefix = mode === "copy" ? "Создание копии" : mode === "create" ? "Создание" : undefined
    const title = prefix
        ? `${prefix}${entityName ? `: ${entityName}` : ""} (${metadataType})`
        : entityName ? `${entityName} (${metadataType})` : undefined

    useEffect(() => {
        if (!title || currentTitle === undefined || currentTitle === title) return
        updateTabTitle(pathname, title)
    }, [title, currentTitle, pathname, updateTabTitle])

    return title
}
