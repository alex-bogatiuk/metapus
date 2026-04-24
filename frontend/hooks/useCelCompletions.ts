/**
 * useCelCompletions — registers Monaco Editor autocomplete for CEL condition editor.
 *
 * Fetches entity metadata (fields + tableParts) from `/meta/:entityName` when
 * the selected target entities change, then registers a CompletionItemProvider
 * that suggests:
 *   - `doc.*`          — all fields from the entity schema
 *   - `humanAmounts.*` — MinorUnits fields converted to float64 (money type)
 *   - `currency.*`     — decimalPlaces, symbol, name
 *   - `action`         — string variable
 *   - `entityType`     — string variable
 *
 * Pattern: metadata-driven (Pattern #5). No hardcoded field lists.
 * Reusable across /new and /[id] automation rule pages.
 */

import { useEffect, useRef, useCallback } from "react"
import type { editor, languages, IDisposable } from "monaco-editor"
import { api } from "@/lib/api"

interface EntityField {
  name: string
  label?: string
  type: string
}

interface EntitySchema {
  name: string
  type: string
  fields: EntityField[]
  tableParts?: {
    name: string
    label?: string
    columns: EntityField[]
  }[]
}

/** CEL built-in variables that are always available. */
const BUILTIN_VARIABLES: languages.CompletionItem[] = [
  {
    label: "doc",
    kind: 5, // Variable
    detail: "Документ — payload события",
    insertText: "doc",
    documentation: "Объект документа. Используйте doc.field для доступа к полям.",
    range: undefined as unknown as languages.CompletionItem["range"],
  },
  {
    label: "action",
    kind: 5,
    detail: "string — Действие (posted, unposted, created, ...)",
    insertText: "action",
    documentation: 'Действие, вызвавшее событие. Например: "posted", "created", "updated".',
    range: undefined as unknown as languages.CompletionItem["range"],
  },
  {
    label: "entityType",
    kind: 5,
    detail: "string — Тип сущности (goods_receipt, goods_issue, ...)",
    insertText: "entityType",
    documentation: "Ключ сущности, например: goods_receipt.",
    range: undefined as unknown as languages.CompletionItem["range"],
  },
  {
    label: "humanAmounts",
    kind: 5,
    detail: "Суммы в человекочитаемом формате",
    insertText: "humanAmounts",
    documentation: "Все MinorUnits поля документа, пересчитанные в float64 с учётом decimalPlaces валюты.",
    range: undefined as unknown as languages.CompletionItem["range"],
  },
  {
    label: "currency",
    kind: 5,
    detail: "Валюта документа",
    insertText: "currency",
    documentation: "Метаданные валюты: decimalPlaces, symbol, name.",
    range: undefined as unknown as languages.CompletionItem["range"],
  },
]

/** Currency sub-fields (always the same structure). */
const CURRENCY_FIELDS: EntityField[] = [
  { name: "decimalPlaces", label: "Количество знаков", type: "integer" },
  { name: "symbol", label: "Символ валюты (₽, $)", type: "string" },
  { name: "name", label: "Название валюты", type: "string" },
]

/** Maps metadata field type → readable CEL type hint. */
function celTypeHint(fieldType: string): string {
  switch (fieldType) {
    case "money": return "int64 (MinorUnits)"
    case "number": case "decimal": return "float64"
    case "integer": return "int"
    case "boolean": return "bool"
    case "date": case "datetime": return "string (ISO 8601)"
    case "reference": return "string (UUID)"
    default: return "string"
  }
}

/**
 * Hook to register Monaco CEL autocomplete based on entity metadata.
 *
 * @param targetEntities - Array of selected entity keys (e.g. ["goods_receipt"])
 * @param editorLanguage - Monaco language id to register completions for
 */
export function useCelCompletions(
  targetEntities: string[],
  editorLanguage = "go",
) {
  const schemaRef = useRef<EntitySchema | null>(null)
  const disposablesRef = useRef<IDisposable[]>([])

  // Fetch entity schema when selection changes
  const fetchSchema = useCallback(async () => {
    // For now, we use the first entity. Multi-entity merge could be added later.
    const entityKey = targetEntities.length === 1 ? targetEntities[0] : null
    if (!entityKey) {
      schemaRef.current = null
      return
    }
    try {
      const schema = await api.meta.getEntity(entityKey) as unknown as EntitySchema
      schemaRef.current = schema
    } catch {
      schemaRef.current = null
    }
  }, [targetEntities])

  useEffect(() => {
    fetchSchema()
  }, [fetchSchema])

  /** Called with onMount from Monaco Editor — registers completion provider. */
  const handleEditorMount = useCallback(
    (_editor: editor.IStandaloneCodeEditor, monaco: typeof import("monaco-editor")) => {
      // Clean up previous disposables
      disposablesRef.current.forEach(d => d.dispose())
      disposablesRef.current = []

      const provider = monaco.languages.registerCompletionItemProvider(editorLanguage, {
        triggerCharacters: ["."],
        provideCompletionItems: (model, position) => {
          const word = model.getWordUntilPosition(position)
          const range = {
            startLineNumber: position.lineNumber,
            endLineNumber: position.lineNumber,
            startColumn: word.startColumn,
            endColumn: word.endColumn,
          }

          // Get text before cursor to detect context (e.g. "doc.")
          const lineContent = model.getLineContent(position.lineNumber)
          const textBefore = lineContent.substring(0, position.column - 1)

          // === doc.* completions ===
          if (textBefore.endsWith("doc.")) {
            const schema = schemaRef.current
            if (!schema) return { suggestions: [] }

            const suggestions: languages.CompletionItem[] = schema.fields.map(f => ({
              label: f.name,
              kind: monaco.languages.CompletionItemKind.Field,
              detail: `${celTypeHint(f.type)}${f.label ? ` — ${f.label}` : ""}`,
              insertText: f.name,
              documentation: f.label || f.name,
              range,
            }))

            // Add table part names (e.g. "lines")
            if (schema.tableParts) {
              for (const tp of schema.tableParts) {
                suggestions.push({
                  label: tp.name,
                  kind: monaco.languages.CompletionItemKind.Property,
                  detail: `[]object — ${tp.label || tp.name}`,
                  insertText: tp.name,
                  documentation: tp.label || `Табличная часть: ${tp.name}`,
                  range,
                })
              }
            }

            return { suggestions }
          }

          // === humanAmounts.* completions ===
          if (textBefore.endsWith("humanAmounts.")) {
            const schema = schemaRef.current
            if (!schema) return { suggestions: [] }

            // humanAmounts contains only money-type fields
            const moneyFields = schema.fields.filter(f => f.type === "money")
            const suggestions: languages.CompletionItem[] = moneyFields.map(f => ({
              label: f.name,
              kind: monaco.languages.CompletionItemKind.Field,
              detail: `float64 (human) — ${f.label || f.name}`,
              insertText: f.name,
              documentation: `Сумма в формате float64, конвертированная из MinorUnits с учётом валюты. Исп. для числовых сравнений.`,
              range,
            }))

            return { suggestions }
          }

          // === currency.* completions ===
          if (textBefore.endsWith("currency.")) {
            const suggestions: languages.CompletionItem[] = CURRENCY_FIELDS.map(f => ({
              label: f.name,
              kind: monaco.languages.CompletionItemKind.Field,
              detail: `${celTypeHint(f.type)} — ${f.label}`,
              insertText: f.name,
              documentation: f.label || f.name,
              range,
            }))
            return { suggestions }
          }

          // === Top-level variable completions ===
          // Only show if no dot context (user is typing a variable name)
          if (!textBefore.includes(".") || textBefore.endsWith(" ") || textBefore.endsWith("(") || textBefore.endsWith("&") || textBefore.endsWith("|")) {
            const suggestions = BUILTIN_VARIABLES.map(v => ({
              ...v,
              range,
            }))
            return { suggestions }
          }

          return { suggestions: [] }
        },
      })

      disposablesRef.current.push(provider)
    },
    [editorLanguage],
  )

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      disposablesRef.current.forEach(d => d.dispose())
      disposablesRef.current = []
    }
  }, [])

  return { handleEditorMount }
}
