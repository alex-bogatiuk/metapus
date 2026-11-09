"use client"

import { useRouter } from "next/navigation"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { CounterpartyForm } from "@/components/catalogs/counterparty-form"
import { useCatalogForm } from "@/hooks/useCatalogForm"
import { api } from "@/lib/api"
import { createFormDraftKey } from "@/stores/useFormDraftStore"
import { INITIAL_COUNTERPARTY_STATE, validateCounterparty, mapCounterpartyFromResponse, mapCounterpartyToUpdate } from "@/lib/counterparty-form"
import { Loader2 } from "lucide-react"
import { useParams } from "next/navigation"

const formKey = createFormDraftKey<typeof INITIAL_COUNTERPARTY_STATE>("frontend/app/(main)/catalogs/counterparties/[id]/page.tsx")

export default function EditCounterpartyPage() {
  const router = useRouter()
  const params = useParams<{ id: string }>()
  const { f, update, handleChange, handleSave, saving, error, loading, deletionMark, entityLabel } = useCatalogForm({
    draftKey: formKey,
    entityName: "Контрагент",
    entityKey: "counterparty",
    initialState: INITIAL_COUNTERPARTY_STATE,
    api: {
      get: api.counterparties.get,
      update: api.counterparties.update,
    },
    listPath: "/catalogs/counterparties",
    validate: validateCounterparty,
    titleField: s => s.name || undefined,
    mapFromResponse: mapCounterpartyFromResponse,
    mapToUpdate: mapCounterpartyToUpdate,
    getVersion: value => value.version,
    getDeletionMark: value => value.deletionMark,
  })

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center text-muted-foreground">
        <Loader2 className="mr-2 h-5 w-5 animate-spin" />Загрузка…
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col">
      <FormToolbar
        title={`${entityLabel}: ${f.name || ""}`}
        status={
          deletionMark
            ? { label: "Помечен на удаление", variant: "destructive" as const }
            : undefined
        }
        primaryAction={{
          label: saving ? "Сохранение…" : "Записать и закрыть",
          variant: "default",
          onClick: () => handleSave(true),
        }}
        secondaryActions={[
          { label: "Записать", onClick: () => handleSave(false) },
        ]}
        backHref="/catalogs/counterparties"
        backTargetId={params.id}
        onClose={() => router.push("/catalogs/counterparties")}
        favoriteTarget={{
          entityType: "counterparty",
          entityId: params.id,
          title: f.name || "",
          url: `/catalogs/counterparties/${params.id}`,
        }}
      />

      {error && (
        <div className="bg-destructive/10 border-b border-destructive/20 px-4 py-2 text-sm text-destructive">{error}</div>
      )}

      <CounterpartyForm value={f} onChange={patch => { update(patch); handleChange() }} />
    </div>
  )
}
