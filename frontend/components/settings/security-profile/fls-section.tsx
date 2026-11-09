"use client"

import { FlsFieldMatrix } from "@/components/settings/fls-field-picker"
import { FormState } from "@/components/settings/security-profile/model"
import { Button } from "@/components/ui/button"
import { useMetadataStore } from "@/stores/useMetadataStore"
import type {
  FieldPolicyItem
} from "@/types/security"
import {
  Plus,
  Trash2
} from "lucide-react"
import { useMemo } from "react"

export function FlsSection({
  form,
  setForm,
}: {
  form: FormState
  setForm: React.Dispatch<React.SetStateAction<FormState>>
}) {
  const metaEntities = useMetadataStore((s) => s.entities)

  // Build grouped entity options using e.key (snake_case) — matches DB format
  // Sorted alphabetically within each group
  const { catalogs, documents } = useMemo(() => {
    const cats: { value: string; label: string }[] = []
    const docs: { value: string; label: string }[] = []
    for (const e of metaEntities) {
      const item = { value: e.key, label: e.presentation.singular ?? e.name }
      if (e.type === "catalog") cats.push(item)
      else docs.push(item)
    }
    const byLabel = (a: { label: string }, b: { label: string }) => a.label.localeCompare(b.label, "ru")
    cats.sort(byLabel)
    docs.sort(byLabel)
    return { catalogs: cats, documents: docs }
  }, [metaEntities])

  const allEntities = useMemo(() => [...catalogs, ...documents], [catalogs, documents])

  // Group policies by entity
  const entityNames = [...new Set(form.fieldPolicies.map((p) => p.entityName))]

  const getPolicy = (entity: string, action: "read" | "write"): FieldPolicyItem | undefined =>
    form.fieldPolicies.find((p) => p.entityName === entity && p.action === action)

  const handleEntityChange = (
    entity: string,
    readPolicy: FieldPolicyItem | undefined,
    writePolicy: FieldPolicyItem | undefined,
  ) => {
    setForm((f) => {
      const rest = f.fieldPolicies.filter((p) => p.entityName !== entity)
      const next = [...rest]
      if (readPolicy) next.push(readPolicy)
      if (writePolicy) next.push(writePolicy)
      return { ...f, fieldPolicies: next }
    })
  }

  const addEntity = (entityValue: string) => {
    setForm((f) => ({
      ...f,
      fieldPolicies: [
        ...f.fieldPolicies,
        { entityName: entityValue, action: "read" as const, allowedFields: ["*"], tableParts: {} },
      ],
    }))
  }

  const removeEntity = (entity: string) => {
    setForm((f) => ({
      ...f,
      fieldPolicies: f.fieldPolicies.filter((p) => p.entityName !== entity),
    }))
  }

  const availableCatalogs = catalogs.filter((e) => !entityNames.includes(e.value))
  const availableDocuments = documents.filter((e) => !entityNames.includes(e.value))

  return (
    <div className="max-w-3xl space-y-4">
      <p className="text-xs text-muted-foreground mb-3">
        Скройте чувствительные поля (цены, суммы) или запретите их редактирование.
        Снимите галочку с поля, которое нужно скрыть. Без ограничений — все поля видимы.
      </p>

      {entityNames.map((entity) => {
        const label = allEntities.find((e) => e.value === entity)?.label ?? entity
        return (
          <div key={entity} className="rounded-md border p-4 space-y-3">
            <div className="flex items-center justify-between">
              <h4 className="text-sm font-medium">{label}</h4>
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 shrink-0"
                onClick={() => removeEntity(entity)}
              >
                <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
              </Button>
            </div>
            <FlsFieldMatrix
              entityName={entity}
              readPolicy={getPolicy(entity, "read")}
              writePolicy={getPolicy(entity, "write")}
              onChange={(r, w) => handleEntityChange(entity, r, w)}
            />
          </div>
        )
      })}

      {/* Grouped "Add entity" buttons */}
      {(availableDocuments.length > 0 || availableCatalogs.length > 0) && (
        <div className="space-y-3">
          {availableDocuments.length > 0 && (
            <div>
              <p className="text-[11px] font-medium text-muted-foreground mb-1.5">Документы</p>
              <div className="flex flex-wrap gap-2">
                {availableDocuments.map((e) => (
                  <Button
                    key={e.value}
                    variant="outline"
                    size="sm"
                    className="h-8 text-xs"
                    onClick={() => addEntity(e.value)}
                  >
                    <Plus className="mr-1.5 h-3 w-3" />
                    {e.label}
                  </Button>
                ))}
              </div>
            </div>
          )}
          {availableCatalogs.length > 0 && (
            <div>
              <p className="text-[11px] font-medium text-muted-foreground mb-1.5">Справочники</p>
              <div className="flex flex-wrap gap-2">
                {availableCatalogs.map((e) => (
                  <Button
                    key={e.value}
                    variant="outline"
                    size="sm"
                    className="h-8 text-xs"
                    onClick={() => addEntity(e.value)}
                  >
                    <Plus className="mr-1.5 h-3 w-3" />
                    {e.label}
                  </Button>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {entityNames.length === 0 && (
        <div className="py-8 text-center text-xs text-muted-foreground">
          Все поля доступны. Добавьте сущность, чтобы настроить скрытие полей.
        </div>
      )}
    </div>
  )
}
