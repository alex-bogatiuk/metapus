"use client"

import { useRouter } from "next/navigation"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { CounterpartyForm } from "@/components/catalogs/counterparty-form"
import { useCatalogForm } from "@/hooks/useCatalogForm"
import { api } from "@/lib/api"
import { createFormDraftKey } from "@/stores/useFormDraftStore"
import { INITIAL_COUNTERPARTY_STATE, validateCounterparty, mapCounterpartyToCreate } from "@/lib/counterparty-form"

const formKey = createFormDraftKey<typeof INITIAL_COUNTERPARTY_STATE>("frontend/app/(main)/catalogs/counterparties/new/page.tsx")

export default function NewCounterpartyPage() {
  const router = useRouter()
  const { f, update, handleChange, handleSave, saving, error, entityLabel } = useCatalogForm({
    draftKey: formKey,
    entityName: "Контрагент",
    entityKey: "counterparty",
    initialState: INITIAL_COUNTERPARTY_STATE,
    api: { create: api.counterparties.create },
    listPath: "/catalogs/counterparties",
    validate: validateCounterparty,
    titleField: s => s.name || undefined,
    mapToCreate: mapCounterpartyToCreate,
  })

  return (
    <div className="flex h-full flex-col">
      <FormToolbar
        title={`${entityLabel} (создание)`}
        primaryAction={{
          label: saving ? "Сохранение…" : "Записать и закрыть",
          variant: "default",
          onClick: () => handleSave(true),
        }}
        secondaryActions={[
          { label: "Записать", onClick: () => handleSave(false) },
        ]}
        backHref="/catalogs/counterparties"
        onClose={() => router.push("/catalogs/counterparties")}
      />

      {error && (
        <div className="bg-destructive/10 border-b border-destructive/20 px-4 py-2 text-sm text-destructive">{error}</div>
      )}

      <CounterpartyForm value={f} onChange={patch => { update(patch); handleChange() }} />
    </div>
  )
}
