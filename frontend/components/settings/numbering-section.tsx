"use client"

import { useCallback } from "react"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { useSettingsStore } from "@/stores/useSettingsStore"
import { useTabDirty } from "@/hooks/useTabDirty"

export function NumberingSection() {
  const { settings, updateNumbering, saveSection, isSaving } = useSettingsStore()
  const { markDirty } = useTabDirty()
  const num = settings.numbering

  const update = useCallback(
    (field: string, value: string | boolean) => {
      updateNumbering({ [field]: value })
      markDirty()
    },
    [updateNumbering, markDirty]
  )

  return (
    <div className="space-y-6">
      {/* Auto-numbering */}
      <div className="space-y-4">
        <div className="grid grid-cols-[1fr_280px] items-center gap-4">
          <div>
            <p className="text-sm font-medium text-foreground">Автонумерация</p>
            <p className="text-xs text-muted-foreground">
              Автоматически присваивать номера новым документам
            </p>
          </div>
          <div className="flex justify-end">
            <Switch
              checked={num.autoNumbering}
              onCheckedChange={(v) => update("autoNumbering", v)}
            />
          </div>
        </div>

        {num.autoNumbering && (
          <div className="grid grid-cols-[1fr_280px] items-start gap-4">
            <div>
              <p className="text-sm font-medium text-foreground">Префикс номера</p>
              <p className="text-xs text-muted-foreground">
                Добавляется перед порядковым номером (например, &laquo;ПТ-&raquo;)
              </p>
            </div>
            <Input
              value={num.numberPrefix}
              onChange={(e) => update("numberPrefix", e.target.value)}
              placeholder="ПТ-"
              className="h-9 text-sm"
            />
          </div>
        )}
      </div>

      {/* Spacer so content doesn't hide behind sticky footer */}
      <div className="h-16" />

      {/* Sticky Save footer */}
      <div className="sticky bottom-0 -mx-6 border-t bg-background px-6 py-3 flex items-center gap-3">
        <Button onClick={() => saveSection("numbering")} disabled={isSaving}>
          {isSaving ? "Сохранение..." : "Сохранить"}
        </Button>
        <p className="text-xs text-muted-foreground">
          Изменения применятся к новым документам
        </p>
      </div>
    </div>
  )
}
