import type { MetadataCodeAssignment } from "@/types/generated/http-contracts"
import { useMetadataStore } from "@/stores/useMetadataStore"

/**
 * What an empty code field of a catalog says, from the code rule the
 * server publishes in the entity metadata (entity.Definition.Code).
 * A code the user must enter shows no placeholder: its field is required.
 */
export function codePlaceholder(code: MetadataCodeAssignment | undefined): string | undefined {
  switch (code?.source) {
    case "series":
      return "Присвоится при записи"
    case "derived":
      return "Заполнится из реквизитов"
    default:
      return undefined
  }
}

/** The code rule of a catalog by its metadata key, e.g. "organization". */
export function useCatalogCode(entityKey: string): MetadataCodeAssignment | undefined {
  return useMetadataStore((s) => s.byKey[entityKey]?.code)
}
