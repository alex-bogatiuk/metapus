"use client"

import { CelEditor } from "@/components/settings/cel-editor"
import { EntityCombobox } from "@/components/settings/security-profile/entity-combobox"
import { RULE_ACTIONS } from "@/components/settings/security-profile/model"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { useSecurityPolicyRules, type SecurityPolicyRulesProps } from "@/hooks/useSecurityPolicyRules"
import { cn } from "@/lib/utils"
import {
  AlertTriangle,
  BookOpen,
  Check,
  ChevronRight,
  Loader2,
  Pencil,
  Plus,
  Trash2,
  X
} from "lucide-react"

export function RulesSection({
  profileId,
  rules,
  setRules,
}: SecurityPolicyRulesProps) {
  const { entityCatalogs, entityDocuments, entityLabelMap, adding, setAdding, editingRuleId, setEditingRuleId, saving, validationResult, setValidationResult, ruleFields, newRule, setNewRule, sandboxOpen, setSandboxOpen, sandboxEntity, sandboxExpr, setSandboxExpr, sandboxDoc, setSandboxDoc, sandboxAction, setSandboxAction, sandboxResult, setSandboxResult, sandboxTesting, sandboxMockLoading, sandboxFields, sandboxCheatOpen, setSandboxCheatOpen, handleSandboxEntityChange, handleEditRule, handleSaveRule, handleDeleteRule, handleToggleRule, handleTestExpression } = useSecurityPolicyRules({ profileId, rules, setRules })

  return (
    <div className="max-w-3xl space-y-4">
      <p className="text-xs text-muted-foreground mb-3">
        Программируемые условия для тонкой настройки прав доступа.
        Например: запретить редактирование проведённых документов или ограничить сумму операции.
      </p>

      {/* CEL Sandbox */}
      <div className="rounded-md border">
        <button
          className="w-full flex items-center justify-between px-4 py-2.5 text-xs font-medium hover:bg-muted/40 transition-colors"
          onClick={() => setSandboxOpen(!sandboxOpen)}
        >
          <span>🧪 Проверка выражения</span>
          <ChevronRight className={cn("h-3.5 w-3.5 text-muted-foreground transition-transform", sandboxOpen && "rotate-90")} />
        </button>
        {sandboxOpen && (
          <div className="px-4 pb-4 space-y-3 border-t pt-3">
            {/* Entity selector + auto-mock */}
            <div className="flex items-center gap-2">
              <div className="flex-1">
                <Label className="text-xs text-muted-foreground mb-1">Сущность</Label>
                <EntityCombobox
                  value={sandboxEntity}
                  onChange={handleSandboxEntityChange}
                  documents={entityDocuments}
                  catalogs={entityCatalogs}
                  placeholder="Выберите сущность…"
                  className="w-full"
                />
              </div>
              {sandboxMockLoading && (
                <div className="pt-4">
                  <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                </div>
              )}
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div>
                <Label className="text-xs text-muted-foreground mb-1">CEL-выражение</Label>
                <div className="rounded-md border overflow-hidden">
                  <CelEditor
                    value={sandboxExpr}
                    onChange={(v) => { setSandboxExpr(v); setSandboxResult(null) }}
                    fields={sandboxFields}
                    height={140}
                  />
                </div>
              </div>
              <div>
                <div className="flex items-center justify-between mb-1">
                  <Label className="text-xs text-muted-foreground">Документ (JSON)</Label>
                </div>
                <Textarea
                  value={sandboxDoc}
                  onChange={(e) => setSandboxDoc(e.target.value)}
                  className="text-xs font-mono resize-none"
                  rows={7}
                />
              </div>
            </div>
            <div className="flex items-end gap-3">
              <div className="w-40">
                <Label className="text-xs text-muted-foreground mb-1">Действие</Label>
                <Select value={sandboxAction} onValueChange={setSandboxAction}>
                  <SelectTrigger className="h-8 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="create">create</SelectItem>
                    <SelectItem value="read">read</SelectItem>
                    <SelectItem value="update">update</SelectItem>
                    <SelectItem value="delete">delete</SelectItem>
                    <SelectItem value="post">post</SelectItem>
                    <SelectItem value="unpost">unpost</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <Button
                size="sm"
                className="h-8 text-xs"
                onClick={handleTestExpression}
                disabled={sandboxTesting || !sandboxExpr.trim()}
              >
                {sandboxTesting ? <Loader2 className="mr-1 h-3 w-3 animate-spin" /> : null}
                Проверить
              </Button>
              {sandboxEntity && sandboxFields.length > 0 && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 text-xs ml-auto"
                  onClick={() => setSandboxCheatOpen(!sandboxCheatOpen)}
                >
                  <BookOpen className="mr-1 h-3 w-3" />
                  {sandboxCheatOpen ? "Скрыть поля" : "Доступные поля"}
                </Button>
              )}
            </div>

            {/* Field cheat sheet */}
            {sandboxCheatOpen && sandboxFields.length > 0 && (
              <div className="rounded-md bg-muted/40 p-3">
                <p className="text-[11px] font-medium text-muted-foreground mb-2">Поля документа (doc.*)</p>
                <div className="grid grid-cols-2 sm:grid-cols-3 gap-x-4 gap-y-0.5">
                  {sandboxFields.filter((f) => f.name !== "id" && f.name !== "version" && f.name !== "createdAt" && f.name !== "updatedAt").map((f) => (
                    <div key={f.name} className="flex items-center gap-1.5 text-[11px]">
                      <code className="font-mono text-primary">doc.{f.name}</code>
                      <span className="text-muted-foreground">({f.type})</span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {sandboxResult && (
              <div className={cn(
                "rounded-md px-3 py-2 text-xs flex items-center gap-2",
                sandboxResult.error
                  ? "bg-destructive/10 text-destructive"
                  : sandboxResult.result
                    ? "bg-success/10 text-success"
                    : "bg-warning/10 text-warning"
              )}>
                {sandboxResult.error ? (
                  <><AlertTriangle className="h-3.5 w-3.5 shrink-0" /> {sandboxResult.error}</>
                ) : sandboxResult.result ? (
                  <><Check className="h-3.5 w-3.5 shrink-0" /> Результат: true</>
                ) : (
                  <><X className="h-3.5 w-3.5 shrink-0" /> Результат: false</>
                )}
                <span className="ml-auto text-[10px] text-muted-foreground">{sandboxResult.elapsed}</span>
              </div>
            )}
          </div>
        )}
      </div>

      {!profileId && (
        <div className="rounded-md border border-amber-500/30 bg-amber-500/5 p-3 flex items-start gap-2">
          <AlertTriangle className="h-4 w-4 text-amber-600 shrink-0 mt-0.5" />
          <p className="text-xs text-amber-700">
            Сначала сохраните профиль, чтобы добавлять условия.
          </p>
        </div>
      )}

      {/* Existing rules */}
      {rules.map((rule) => (
        <div
          key={rule.id}
          className={cn(
            "rounded-md border p-4 space-y-2",
            !rule.enabled && "opacity-50"
          )}
        >
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Badge variant="outline" className="text-[10px] h-4 min-w-5 justify-center font-mono">
                {rule.priority}
              </Badge>
              <h4 className="text-sm font-medium">{rule.name}</h4>
              <Badge
                variant={rule.effect === "deny" ? "destructive" : "default"}
                className="text-[10px] h-4"
              >
                {rule.effect === "deny" ? "Запретить" : "Разрешить"}
              </Badge>
              <Badge variant="outline" className="text-[10px] h-4 font-mono">
                {entityLabelMap[rule.entityName] ?? rule.entityName}
              </Badge>
              {rule.actions.map((a) => (
                <Badge key={a} variant="secondary" className="text-[10px] h-4">
                  {RULE_ACTIONS.find((ra) => ra.value === a)?.label ?? a}
                </Badge>
              ))}
            </div>
            <div className="flex items-center gap-1">
              <Switch
                checked={rule.enabled}
                onCheckedChange={() => handleToggleRule(rule)}
                className="scale-75"
              />
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6"
                onClick={() => handleEditRule(rule)}
              >
                <Pencil className="h-3 w-3 text-muted-foreground" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6"
                onClick={() => handleDeleteRule(rule)}
              >
                <Trash2 className="h-3 w-3 text-muted-foreground" />
              </Button>
            </div>
          </div>
          <p className="text-xs font-mono text-muted-foreground bg-muted/40 rounded px-2 py-1">
            {rule.expression}
          </p>
        </div>
      ))}

      {/* Add rule form */}
      {adding ? (
        <div className="rounded-md border border-primary/30 p-4 space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label className="text-xs text-muted-foreground mb-1">Название</Label>
              <Input
                value={newRule.name}
                onChange={(e) => setNewRule((r) => ({ ...r, name: e.target.value }))}
                placeholder="Запрет проведения"
                className="h-8 text-xs"
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground mb-1">Сущность</Label>
              <EntityCombobox
                value={newRule.entityName}
                onChange={(v) => setNewRule((r) => ({ ...r, entityName: v }))}
                documents={entityDocuments}
                catalogs={entityCatalogs}
                withWildcard
                className="w-full"
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label className="text-xs text-muted-foreground">Действия</Label>
            <div className="flex flex-wrap gap-1.5">
              {RULE_ACTIONS.map((a) => {
                const selected = newRule.actions.includes(a.value)
                return (
                  <button
                    key={a.value}
                    type="button"
                    className={cn(
                      "inline-flex items-center rounded-md border px-2.5 py-1 text-[11px] font-medium transition-colors",
                      selected
                        ? "border-primary bg-primary/10 text-primary"
                        : "border-input bg-background text-muted-foreground hover:bg-muted/50"
                    )}
                    onClick={() => {
                      setNewRule((r) => ({
                        ...r,
                        actions: selected
                          ? r.actions.filter((v) => v !== a.value)
                          : [...r.actions, a.value],
                      }))
                    }}
                  >
                    {selected && <Check className="mr-1 h-3 w-3" />}
                    {a.label}
                  </button>
                )
              })}
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label className="text-xs text-muted-foreground mb-1">Эффект</Label>
              <Select
                value={newRule.effect}
                onValueChange={(v) => setNewRule((r) => ({ ...r, effect: v as "deny" | "allow" }))}
              >
                <SelectTrigger className="h-8 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="deny">Запретить операцию</SelectItem>
                  <SelectItem value="allow">Явно разрешить</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label className="text-xs text-muted-foreground mb-1">Приоритет</Label>
              <Input
                type="number"
                value={newRule.priority}
                onChange={(e) => setNewRule((r) => ({ ...r, priority: parseInt(e.target.value) || 10 }))}
                className="h-8 text-xs font-mono"
              />
            </div>
          </div>

          <div>
            <Label className="text-xs text-muted-foreground mb-1">CEL-выражение</Label>
            <div className="relative">
              <div className="rounded-md border overflow-hidden">
                <CelEditor
                  value={newRule.expression}
                  onChange={(v) => {
                    setNewRule((r) => ({ ...r, expression: v }))
                    setValidationResult(null)
                  }}
                  fields={ruleFields}
                  placeholder="doc.posted == true"
                  height={56}
                />
              </div>
              {validationResult && (
                <div className="absolute right-2 top-2 z-10">
                  {validationResult.valid ? (
                    <Check className="h-4 w-4 text-emerald-500" />
                  ) : (
                    <AlertTriangle className="h-4 w-4 text-destructive" />
                  )}
                </div>
              )}
            </div>
            {validationResult && !validationResult.valid && validationResult.error && (
              <p className="text-[11px] text-destructive mt-1">{validationResult.error}</p>
            )}
          </div>

          <div className="flex justify-end gap-2">
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-xs"
              onClick={() => {
                setAdding(false)
                setEditingRuleId(null)
                setValidationResult(null)
              }}
            >
              Отменить
            </Button>
            <Button
              size="sm"
              className="h-7 text-xs"
              onClick={handleSaveRule}
              disabled={saving}
            >
              {saving && <Loader2 className="mr-1 h-3 w-3 animate-spin" />}
              {editingRuleId ? "Сохранить" : "Добавить"}
            </Button>
          </div>
        </div>
      ) : (
        <Button
          variant="outline"
          size="sm"
          className="h-8 text-xs"
          onClick={() => setAdding(true)}
          disabled={!profileId}
        >
          <Plus className="mr-1.5 h-3 w-3" />
          Добавить условие
        </Button>
      )}

      {rules.length === 0 && !adding && (
        <div className="py-6 text-center text-xs text-muted-foreground">
          Нет дополнительных условий. Основные права определяются ролями.
        </div>
      )}
    </div>
  )
}

