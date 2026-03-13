/**
 * Human-readable audit log formatting for security profiles.
 *
 * Translates raw audit change diffs (field keys, old/new values)
 * into business-friendly Russian event descriptions.
 */

// ── Field key → Russian label mapping ───────────────────────────────────

const FIELD_LABELS: Record<string, string> = {
  code: "Код",
  name: "Название",
  description: "Описание",
  dimensions: "Доступ к данным",
  fieldPolicies: "Скрытие полей",
  action: "Действие",
  userId: "Пользователь",
  enabled: "Активность",
  expression: "CEL-выражение",
  effect: "Эффект",
  priority: "Приоритет",
  entityName: "Сущность",
  actions: "Действия",
  ruleName: "Название правила",
}

// ── Action → human-readable description ─────────────────────────────────

const ACTION_LABELS: Record<string, string> = {
  create: "Создание профиля",
  update: "Изменение профиля",
  delete: "Удаление профиля",
  assign_user: "Назначен пользователь",
  remove_user: "Снят пользователь",
  add_rule: "Добавлено условие",
  delete_rule: "Удалено условие",
  update_rule: "Изменено условие",
}

// ── Dimension key → Russian label ───────────────────────────────────────

const DIM_LABELS: Record<string, string> = {
  organization: "организации",
  warehouse: "склады",
  counterparty: "контрагенты",
}

// ── Entity name → Russian label ─────────────────────────────────────────

const ENTITY_LABELS: Record<string, string> = {
  goods_receipt: "Поступление товаров",
  goods_issue: "Расход товаров",
  GoodsReceipt: "Поступление товаров",
  GoodsIssue: "Расход товаров",
  Nomenclature: "Номенклатура",
  Counterparty: "Контрагенты",
  Warehouse: "Склады",
  Organization: "Организации",
  "*": "Все сущности",
}

export function getActionLabel(action: string): string {
  return ACTION_LABELS[action] ?? action
}

export function getFieldLabel(key: string): string {
  return FIELD_LABELS[key] ?? key
}

// ── Event-based formatting ──────────────────────────────────────────────

export interface AuditEventLine {
  /** Human-readable event sentence */
  text: string
  /** Visual style hint */
  variant: "neutral" | "added" | "removed" | "changed"
}

/**
 * Build human-readable event descriptions for an audit entry.
 * Returns narrative phrases like «Добавлено условие "Запрет проведения"»
 * instead of raw field diffs.
 */
export function formatAuditChanges(
  action: string,
  changes?: Record<string, unknown>
): AuditEventLine[] {
  if (!changes || Object.keys(changes).length === 0) return []

  // ── Special action handlers ───────────────────────────────────────────

  if (action === "assign_user") {
    const email = changes["userEmail"] ?? changes["userId"] ?? ""
    return [{ text: `Назначен пользователь «${email}»`, variant: "added" }]
  }

  if (action === "remove_user") {
    const email = changes["userEmail"] ?? changes["userId"] ?? ""
    return [{ text: `Снят пользователь «${email}»`, variant: "removed" }]
  }

  if (action === "add_rule") {
    const name = changes["ruleName"] ?? changes["name"] ?? "—"
    const entity = ENTITY_LABELS[String(changes["entityName"] ?? "")] ?? changes["entityName"] ?? ""
    const effect = changes["effect"] === "deny" ? "Запретить" : "Разрешить"
    return [{ text: `Добавлено условие «${name}» (${effect}) для ${entity}`, variant: "added" }]
  }

  if (action === "delete_rule") {
    const name = changes["ruleName"] ?? changes["name"] ?? "—"
    return [{ text: `Удалено условие «${name}»`, variant: "removed" }]
  }

  if (action === "update_rule") {
    const name = changes["ruleName"] ?? changes["name"] ?? "—"
    const lines: AuditEventLine[] = [{ text: `Изменено условие «${name}»`, variant: "changed" }]
    appendFieldDiffs(changes, lines, ["ruleName", "name", "action"])
    return lines
  }

  if (action === "create") {
    const name = changes["name"] ?? ""
    return [{ text: `Создан профиль «${name}»`, variant: "added" }]
  }

  if (action === "delete") {
    const name = changes["name"] ?? ""
    return [{ text: `Удалён профиль «${name}»`, variant: "removed" }]
  }

  // ── Generic update: produce per-field event lines ─────────────────────

  const lines: AuditEventLine[] = []
  appendFieldDiffs(changes, lines, ["action"])
  return lines
}

/** Append human-readable lines for each changed field in a diff object */
function appendFieldDiffs(
  changes: Record<string, unknown>,
  lines: AuditEventLine[],
  skipKeys: string[],
) {
  for (const [key, val] of Object.entries(changes)) {
    if (skipKeys.includes(key)) continue

    const label = getFieldLabel(key)

    // Diff format: { old: ..., new: ... }
    if (val && typeof val === "object" && ("old" in val || "new" in val)) {
      const diff = val as { old?: unknown; new?: unknown }
      const oldStr = formatValue(key, diff.old)
      const newStr = formatValue(key, diff.new)

      if (oldStr === "—" && newStr !== "—") {
        lines.push({ text: `${label}: установлено «${newStr}»`, variant: "added" })
      } else if (oldStr !== "—" && newStr === "—") {
        lines.push({ text: `${label}: убрано (было «${oldStr}»)`, variant: "removed" })
      } else {
        lines.push({ text: `${label}: «${oldStr}» → «${newStr}»`, variant: "changed" })
      }
      continue
    }

    // Plain value
    lines.push({ text: `${label}: ${formatValue(key, val)}`, variant: "neutral" })
  }
}

/** Format a single value into a short human-readable string */
function formatValue(key: string, value: unknown): string {
  if (value === null || value === undefined) return "—"

  // Boolean-like
  if (key === "enabled") return value ? "Включено" : "Отключено"
  if (key === "effect") return value === "deny" ? "Запретить" : "Разрешить"

  // Complex JSON objects
  if (typeof value === "string" && key === "dimensions") {
    return formatDimensionsSummary(value)
  }
  if (typeof value === "string" && key === "fieldPolicies") {
    return formatPoliciesSummary(value)
  }

  // Entity name
  if (key === "entityName" && typeof value === "string") {
    return ENTITY_LABELS[value] ?? value
  }

  return String(value)
}

function formatDimensionsSummary(jsonStr: string): string {
  try {
    const parsed = JSON.parse(jsonStr) as Record<string, string[]>
    const parts = Object.entries(parsed).map(([k, v]) => {
      const label = DIM_LABELS[k] ?? k
      const count = Array.isArray(v) ? v.length : 0
      return `${label}: ${count}`
    })
    return parts.length > 0 ? parts.join(", ") : "без ограничений"
  } catch {
    return jsonStr.length > 60 ? jsonStr.slice(0, 57) + "…" : jsonStr
  }
}

function formatPoliciesSummary(jsonStr: string): string {
  try {
    const parsed = JSON.parse(jsonStr)
    if (Array.isArray(parsed)) {
      if (parsed.length === 0) return "без ограничений"
      return `${parsed.length} ${pluralPolicies(parsed.length)}`
    }
    return jsonStr.length > 60 ? jsonStr.slice(0, 57) + "…" : jsonStr
  } catch {
    return jsonStr.length > 60 ? jsonStr.slice(0, 57) + "…" : jsonStr
  }
}

function pluralPolicies(n: number): string {
  if (n === 1) return "ограничение"
  if (n >= 2 && n <= 4) return "ограничения"
  return "ограничений"
}
