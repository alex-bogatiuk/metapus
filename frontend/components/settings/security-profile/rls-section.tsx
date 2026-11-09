"use client"

import { RlsDimensionPicker } from "@/components/settings/rls-dimension-picker"
import { FormState, RLS_DIMENSION_KEYS } from "@/components/settings/security-profile/model"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { Label } from "@/components/ui/label"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Separator } from "@/components/ui/separator"
import { useMetadataStore } from "@/stores/useMetadataStore"
import {
  Plus,
  Trash2,
  X
} from "lucide-react"
import { useMemo, useState } from "react"

export function RlsSection({
  form,
  setForm,
}: {
  form: FormState
  setForm: React.Dispatch<React.SetStateAction<FormState>>
}) {
  const getLabel = useMetadataStore((s) => s.getLabel)
  const metaEntities = useMetadataStore((s) => s.entities)

  const KNOWN_DIMENSIONS = useMemo(
    () => RLS_DIMENSION_KEYS.map((key) => ({ key, label: getLabel(key, "plural") })),
    [getLabel]
  )

  // Build entity options for per-entity overrides
  const { entityCatalogs, entityDocuments, allEntityMap } = useMemo(() => {
    const cats: { value: string; label: string }[] = []
    const docs: { value: string; label: string }[] = []
    const labelMap: Record<string, string> = {}
    for (const e of metaEntities) {
      const label = e.presentation.singular ?? e.name
      const item = { value: e.key, label }
      labelMap[e.key] = label
      if (e.type === "catalog") cats.push(item)
      else docs.push(item)
    }
    const byLabel = (a: { label: string }, b: { label: string }) => a.label.localeCompare(b.label, "ru")
    cats.sort(byLabel)
    docs.sort(byLabel)
    return { entityCatalogs: cats, entityDocuments: docs, allEntityMap: labelMap }
  }, [metaEntities])

  // ── Global dimensions ─────────────────────────────────────────────

  const addGlobalDimension = (key: string) => {
    setForm((f) => ({
      ...f,
      dimensions: { ...f.dimensions, [key]: [] },
    }))
  }

  const removeGlobalDimension = (key: string) => {
    setForm((f) => {
      const dims = { ...f.dimensions }
      delete dims[key]
      return { ...f, dimensions: dims }
    })
  }

  const updateGlobalDimensionIds = (key: string, ids: string[]) => {
    setForm((f) => ({
      ...f,
      dimensions: { ...f.dimensions, [key]: ids },
    }))
  }

  const availableGlobalDimensions = KNOWN_DIMENSIONS.filter(
    (d) => !(d.key in form.dimensions)
  )

  // ── Per-entity dimensions ─────────────────────────────────────────

  const entityDimKeys = Object.keys(form.entityDimensions)

  const addEntityOverride = (entityKey: string) => {
    setForm((f) => ({
      ...f,
      entityDimensions: { ...f.entityDimensions, [entityKey]: {} },
    }))
  }

  const removeEntityOverride = (entityKey: string) => {
    setForm((f) => {
      const ed = { ...f.entityDimensions }
      delete ed[entityKey]
      return { ...f, entityDimensions: ed }
    })
  }

  const addEntityDimension = (entityKey: string, dimKey: string) => {
    setForm((f) => ({
      ...f,
      entityDimensions: {
        ...f.entityDimensions,
        [entityKey]: { ...f.entityDimensions[entityKey], [dimKey]: [] },
      },
    }))
  }

  const removeEntityDimension = (entityKey: string, dimKey: string) => {
    setForm((f) => {
      const dims = { ...f.entityDimensions[entityKey] }
      delete dims[dimKey]
      const ed = { ...f.entityDimensions }
      if (Object.keys(dims).length === 0) {
        delete ed[entityKey]
      } else {
        ed[entityKey] = dims
      }
      return { ...f, entityDimensions: ed }
    })
  }

  const updateEntityDimensionIds = (entityKey: string, dimKey: string, ids: string[]) => {
    setForm((f) => ({
      ...f,
      entityDimensions: {
        ...f.entityDimensions,
        [entityKey]: { ...f.entityDimensions[entityKey], [dimKey]: ids },
      },
    }))
  }

  const availableEntitiesForOverride = [...entityDocuments, ...entityCatalogs].filter(
    (e) => !entityDimKeys.includes(e.value)
  )

  const hasAny = Object.keys(form.dimensions).length > 0 || entityDimKeys.length > 0

  return (
    <div className="max-w-3xl space-y-6">
      <p className="text-xs text-muted-foreground">
        Ограничьте доступ по организациям, мерчантам или контрагентам.
        Пользователь видит только документы, относящиеся к выбранным значениям.
        Если ограничения не заданы — доступны все записи.
      </p>

      {/* ── Section 1: Global dimensions ── */}
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-medium">Общие ограничения</h3>
          <span className="text-[11px] text-muted-foreground">— применяются ко всем сущностям</span>
        </div>

        {Object.entries(form.dimensions).map(([key, ids]) => {
          const meta = KNOWN_DIMENSIONS.find((d) => d.key === key)
          return (
            <div key={key} className="rounded-md border p-4 space-y-2">
              <div className="flex items-center justify-between">
                <Label className="text-sm font-medium">{meta?.label ?? key}</Label>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-6 w-6"
                  onClick={() => removeGlobalDimension(key)}
                >
                  <X className="h-3 w-3" />
                </Button>
              </div>
              <RlsDimensionPicker
                dimensionKey={key}
                selectedIds={ids}
                onChange={(newIds) => updateGlobalDimensionIds(key, newIds)}
              />
            </div>
          )
        })}

        {availableGlobalDimensions.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {availableGlobalDimensions.map((d) => (
              <Button
                key={d.key}
                variant="outline"
                size="sm"
                className="h-8 text-xs"
                onClick={() => addGlobalDimension(d.key)}
              >
                <Plus className="mr-1 h-3 w-3" />
                {d.label}
              </Button>
            ))}
          </div>
        )}

        {Object.keys(form.dimensions).length === 0 && (
          <div className="rounded-md border border-dashed p-4 text-center text-xs text-muted-foreground">
            Без общих ограничений. Добавьте измерение, чтобы ограничить доступ для всех сущностей.
          </div>
        )}
      </div>

      <Separator />

      {/* ── Section 2: Per-entity overrides ── */}
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-medium">Ограничения по сущностям</h3>
          <span className="text-[11px] text-muted-foreground">— переопределяют общие для конкретной сущности</span>
        </div>

        {entityDimKeys.map((entityKey) => {
          const entityLabel = allEntityMap[entityKey] ?? entityKey
          const entityDims = form.entityDimensions[entityKey] ?? {}
          const availableDimsForEntity = KNOWN_DIMENSIONS.filter(
            (d) => !(d.key in entityDims)
          )

          return (
            <div key={entityKey} className="rounded-md border">
              <div className="flex items-center justify-between px-4 py-2.5 bg-muted/30">
                <div className="flex items-center gap-2">
                  <h4 className="text-sm font-medium">{entityLabel}</h4>
                  <Badge variant="outline" className="text-[10px] h-4 font-mono">
                    {entityKey}
                  </Badge>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-6 w-6"
                  onClick={() => removeEntityOverride(entityKey)}
                >
                  <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
                </Button>
              </div>
              <div className="px-4 pb-4 pt-3 space-y-3">
                {Object.entries(entityDims).map(([dimKey, ids]) => {
                  const dimMeta = KNOWN_DIMENSIONS.find((d) => d.key === dimKey)
                  return (
                    <div key={dimKey} className="space-y-2">
                      <div className="flex items-center justify-between">
                        <Label className="text-xs font-medium text-muted-foreground">{dimMeta?.label ?? dimKey}</Label>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-5 w-5"
                          onClick={() => removeEntityDimension(entityKey, dimKey)}
                        >
                          <X className="h-2.5 w-2.5" />
                        </Button>
                      </div>
                      <RlsDimensionPicker
                        dimensionKey={dimKey}
                        selectedIds={ids}
                        onChange={(newIds) => updateEntityDimensionIds(entityKey, dimKey, newIds)}
                      />
                    </div>
                  )
                })}

                {availableDimsForEntity.length > 0 && (
                  <div className="flex flex-wrap gap-1.5">
                    {availableDimsForEntity.map((d) => (
                      <Button
                        key={d.key}
                        variant="outline"
                        size="sm"
                        className="h-7 text-[11px]"
                        onClick={() => addEntityDimension(entityKey, d.key)}
                      >
                        <Plus className="mr-1 h-2.5 w-2.5" />
                        {d.label}
                      </Button>
                    ))}
                  </div>
                )}

                {Object.keys(entityDims).length === 0 && (
                  <p className="text-[11px] text-muted-foreground">
                    Добавьте измерение для этой сущности.
                  </p>
                )}
              </div>
            </div>
          )
        })}

        {/* Add entity override */}
        {availableEntitiesForOverride.length > 0 && (
          <EntityOverrideAdder
            documents={entityDocuments.filter((e) => !entityDimKeys.includes(e.value))}
            catalogs={entityCatalogs.filter((e) => !entityDimKeys.includes(e.value))}
            onAdd={addEntityOverride}
          />
        )}

        {entityDimKeys.length === 0 && (
          <div className="rounded-md border border-dashed p-4 text-center text-xs text-muted-foreground">
            Нет индивидуальных ограничений. Добавьте сущность, чтобы переопределить общие правила для неё.
          </div>
        )}
      </div>

      {!hasAny && (
        <div className="rounded-md border border-blue-200 bg-blue-50/50 px-3 py-2.5">
          <p className="text-[11px] text-blue-700">
            <strong>Fail-open:</strong> без ограничений пользователь имеет полный доступ ко всем данным.
            Добавьте измерение, чтобы ограничить видимость.
          </p>
        </div>
      )}
    </div>
  )
}

export function EntityOverrideAdder({
  documents,
  catalogs,
  onAdd,
}: {
  documents: { value: string; label: string }[]
  catalogs: { value: string; label: string }[]
  onAdd: (entityKey: string) => void
}) {
  const [open, setOpen] = useState(false)

  if (documents.length === 0 && catalogs.length === 0) return null

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm" className="h-8 text-xs">
          <Plus className="mr-1.5 h-3 w-3" />
          Добавить сущность
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[280px] p-0" align="start">
        <Command>
          <CommandInput placeholder="Поиск…" className="h-8 text-xs" />
          <CommandList>
            <CommandEmpty className="py-4 text-center text-xs text-muted-foreground">Не найдено</CommandEmpty>
            {documents.length > 0 && (
              <CommandGroup heading="Документы">
                {documents.map((e) => (
                  <CommandItem
                    key={e.value}
                    value={e.label}
                    onSelect={() => { onAdd(e.value); setOpen(false) }}
                    className="text-xs"
                  >
                    {e.label}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
            {catalogs.length > 0 && (
              <CommandGroup heading="Справочники">
                {catalogs.map((e) => (
                  <CommandItem
                    key={e.value}
                    value={e.label}
                    onSelect={() => { onAdd(e.value); setOpen(false) }}
                    className="text-xs"
                  >
                    {e.label}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
