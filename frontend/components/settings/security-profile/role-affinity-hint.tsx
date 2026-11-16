"use client"

import type { FormState } from "@/components/settings/security-profile/model"
import { getAffinityRules } from "@/lib/security-registry"
import { Badge } from "@/components/ui/badge"

export function RoleAffinityHint({ form }: { form: FormState }) {
  const hasDims = Object.keys(form.dimensions).length > 0
  const hasFls = form.fieldPolicies.length > 0
  if (!hasDims && !hasFls) return null

  const matched = getAffinityRules().filter((r) => r.match(form))
  if (matched.length === 0) return null

  return (
    <div className="rounded-md border border-border bg-muted/50 px-3 py-2.5">
      <p className="text-[11px] text-foreground font-medium mb-1">Рекомендуемые роли для этого профиля</p>
      <div className="flex flex-wrap gap-1.5">
        {matched.map((m) => (
          <Badge key={m.roleHint} variant="outline" className="text-[10px] h-5 bg-background border-border text-foreground">
            {m.label}
            <span className="ml-1 text-muted-foreground font-mono">{m.roleHint}</span>
          </Badge>
        ))}
      </div>
      <p className="text-[10px] text-muted-foreground mt-1.5">
        Подсказка на основе настроенных ограничений (RLS/FLS). Назначьте роль пользователю на вкладке «Пользователи и роли».
      </p>
    </div>
  )
}
