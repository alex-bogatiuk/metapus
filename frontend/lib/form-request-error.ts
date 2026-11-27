/**
 * What the user reads when the form's request fails its contract before it
 * is sent: the first field at fault by its label, "Заполните поле «Поставщик»"
 * for an empty one, so a draft filled from another document says what it
 * still needs.
 */

import { z } from "zod"
import type { JsonObject, JsonValue } from "@/lib/json-value"
import type { EntityMetadata } from "@/lib/schemas/metadata"

const blank = (value: JsonValue | undefined) => value === undefined || value === null || value === ""

export function formRequestErrorMessage(meta: EntityMetadata, draft: JsonObject, err: Error): string | null {
    if (!(err instanceof z.ZodError)) return null
    const issue = err.issues[0]
    if (!issue) return null
    const [head, line, column] = issue.path
    if (typeof head !== "string") return null
    const part = meta.tableParts?.find((p) => p.name === head)
    if (part && typeof line === "number" && typeof column === "string") {
        const label = part.columns.find((c) => c.name === column)?.label ?? column
        const rows = draft[head]
        const row = Array.isArray(rows) ? rows[line] : undefined
        const value = row && typeof row === "object" && !Array.isArray(row) ? row[column] : undefined
        return blank(value) ? `Строка ${line + 1}: заполните «${label}»` : `Строка ${line + 1}: «${label}» заполнено неверно`
    }
    const label = meta.fields.find((f) => f.name === head)?.label ?? head
    return blank(draft[head]) ? `Заполните поле «${label}»` : `Поле «${label}» заполнено неверно`
}
