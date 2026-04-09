"use client"

import { AlertCircle, CheckCircle2 } from "lucide-react"
import { cn } from "@/lib/utils"
import type { WidgetRenderProps } from "@/types/dashboard"

const TASKS = [
    { label: "Ввести начальные остатки", done: false },
    { label: "Загрузить остатки из Excel", done: false },
    { label: "Настроить печатные формы", done: true },
    { label: "Добавить сотрудников", done: false },
]

export default function TasksRenderer(_props: WidgetRenderProps<"tasks">) {
    return (
        <div className="flex h-full flex-col">
            <div className="border-b px-4 py-3">
                <h3 className="text-sm font-semibold text-foreground">Текущие дела</h3>
            </div>
            <div className="flex flex-col gap-0.5 p-2">
                {TASKS.map((task, i) => (
                    <div
                        key={i}
                        className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-muted/50"
                    >
                        {task.done ? (
                            <CheckCircle2 className="h-4 w-4 shrink-0 text-success" />
                        ) : (
                            <AlertCircle className={cn("h-4 w-4 shrink-0 text-destructive")} />
                        )}
                        <span className={task.done ? "text-muted-foreground line-through" : "text-foreground"}>
                            {task.label}
                        </span>
                    </div>
                ))}
            </div>
        </div>
    )
}
