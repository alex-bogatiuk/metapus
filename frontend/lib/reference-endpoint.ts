import { useMetadataStore } from "@/stores/useMetadataStore"

/**
 * Resolves a referenceType (an entity key) to the API path of its list, from
 * the metadata store; null when the entity is not known to this user.
 */
export function resolveRefEndpoint(referenceType: string): string | null {
    const entity = useMetadataStore.getState().byKey[referenceType]
    if (!entity?.routePrefix) return null
    const prefix = entity.type === "document" ? "/document" : "/catalog"
    return `${prefix}/${entity.routePrefix}`
}
