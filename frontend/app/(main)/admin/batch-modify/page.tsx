"use client"

import { useRouter } from "next/navigation"
import { ArrowLeft, Pencil, Construction } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ScrollArea } from "@/components/ui/scroll-area"

export default function BatchModifyPage() {
  const router = useRouter()

  return (
    <ScrollArea className="flex-1">
      <div className="space-y-6 p-6">
        {/* Header */}
        <div className="flex items-center gap-3">
          <Button variant="ghost" size="icon" onClick={() => router.back()}>
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">
              Групповое изменение реквизитов
            </h1>
            <p className="text-sm text-muted-foreground mt-1">
              Массовое изменение значений полей в справочниках и документах
            </p>
          </div>
        </div>

        {/* Stub content */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base flex items-center gap-2">
              <Construction className="h-5 w-5 text-amber-500" />
              В разработке
            </CardTitle>
            <CardDescription>
              Инструмент группового изменения реквизитов находится в стадии разработки
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="text-center py-12 text-muted-foreground">
              <Pencil className="h-12 w-12 mx-auto mb-4 text-muted-foreground/30" />
              <p className="text-sm font-medium">
                Скоро здесь появится возможность массово изменять реквизиты объектов
              </p>
              <p className="text-xs mt-2 max-w-md mx-auto">
                Инструмент позволит выбрать тип объекта, задать фильтр,
                указать изменяемые поля и применить изменения ко всем подходящим записям.
              </p>
            </div>
          </CardContent>
        </Card>
      </div>
    </ScrollArea>
  )
}
