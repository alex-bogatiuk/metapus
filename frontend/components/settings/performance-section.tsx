"use client"

import { useCallback } from "react"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { Slider } from "@/components/ui/slider"
import { useSettingsStore } from "@/stores/useSettingsStore"
import { useTabDirty } from "@/hooks/useTabDirty"
import { Badge } from "@/components/ui/badge"

export function PerformanceSection() {
  const { settings, updatePerformance, saveSection, isSaving } =
    useSettingsStore()
  const { markDirty } = useTabDirty()
  const perf = settings.performance

  const handleConcurrencyChange = useCallback(
    (value: number[]) => {
      updatePerformance({ batchConcurrency: value[0] })
      markDirty()
    },
    [updatePerformance, markDirty]
  )

  const handleSave = useCallback(() => {
    saveSection("performance")
  }, [saveSection])

  const getSpeedLabel = (value: number): string => {
    if (value <= 2) return "Низкая нагрузка"
    if (value <= 4) return "Сбалансированный"
    return "Максимальная скорость"
  }

  return (
    <div className="space-y-6">
      {/* Batch Processing */}
      <div className="space-y-4">
        <h3 className="text-sm font-semibold text-foreground">
          Пакетные операции
        </h3>

        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-foreground">
                Параллелизм пакетных операций
              </p>
              <p className="text-xs text-muted-foreground">
                Количество документов, обрабатываемых одновременно при массовом
                проведении, отмене проведения и пометке на удаление
              </p>
            </div>
            <Badge variant="secondary" className="ml-4 shrink-0 tabular-nums">
              {perf.batchConcurrency}
            </Badge>
          </div>

          <div className="grid grid-cols-[1fr_280px] items-center gap-4">
            <div className="flex items-center gap-3 text-xs text-muted-foreground">
              <span>1</span>
              <Slider
                value={[perf.batchConcurrency]}
                onValueChange={handleConcurrencyChange}
                min={1}
                max={5}
                step={1}
                className="flex-1"
              />
              <span>5</span>
            </div>
            <div className="text-right">
              <span className="text-xs text-muted-foreground">
                {getSpeedLabel(perf.batchConcurrency)}
              </span>
            </div>
          </div>

          <div className="rounded-lg border bg-muted/30 p-3">
            <p className="text-xs text-muted-foreground leading-relaxed">
              <strong className="text-foreground">Рекомендация:</strong> значение{" "}
              <strong>5</strong> обеспечивает оптимальное соотношение скорости и
              стабильности. Уменьшите значение, если при массовых операциях
              наблюдаются ошибки подключения к базе данных.
            </p>
          </div>
        </div>
      </div>

      <Separator />

      {/* Future: more performance settings can be added here */}
      <div className="space-y-4">
        <h3 className="text-sm font-semibold text-foreground">Информация</h3>
        <div className="grid grid-cols-[1fr_280px] items-start gap-4">
          <div>
            <p className="text-sm font-medium text-foreground">
              Пул подключений к базе
            </p>
            <p className="text-xs text-muted-foreground">
              Максимальное количество одновременных подключений на тенант
            </p>
          </div>
          <div className="text-right">
            <Badge variant="outline" className="tabular-nums">
              10
            </Badge>
          </div>
        </div>
      </div>

      {/* Spacer so content doesn't hide behind sticky footer */}
      <div className="h-16" />

      {/* Sticky Save footer */}
      <div className="sticky bottom-0 -mx-6 border-t bg-background px-6 py-3 flex items-center gap-3">
        <Button onClick={handleSave} disabled={isSaving}>
          {isSaving ? "Сохранение..." : "Сохранить"}
        </Button>
        <p className="text-xs text-muted-foreground">
          Изменения применятся к следующим пакетным операциям
        </p>
      </div>
    </div>
  )
}
