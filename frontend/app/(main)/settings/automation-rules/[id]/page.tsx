"use client"

import { useRouter, useParams } from "next/navigation"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Button } from "@/components/ui/button"
import { Play } from "lucide-react"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { ScrollArea } from "@/components/ui/scroll-area"
import { useCatalogForm } from "@/hooks/useCatalogForm"
import { api } from "@/lib/api"
import Editor from "@monaco-editor/react"
import type { UpdateAutomationRuleRequest } from "@/types/automation"
import type { ServiceAccount } from "@/types/service-account"
import { useState, useEffect } from "react"
import { toast } from "sonner"

interface RuleFormState extends UpdateAutomationRuleRequest {
  id?: string
  [key: string]: unknown
}

const INITIAL_STATE: RuleFormState = {
  name: "",
  eventType: "",
  actionType: "webhook",
  serviceAccountId: "",
  conditionCel: "",
  actionTemplate: "",
  isActive: true,
}

export default function EditAutomationRulePage() {
  const router = useRouter()
  const params = useParams()
  const idStr = params.id as string
  const [accounts, setAccounts] = useState<ServiceAccount[]>([])

  useEffect(() => {
    api.system.serviceAccounts.list().then(setAccounts).catch(console.error)
  }, [])

  const { f, update, handleChange, handleSave, saving, error, loading, deletionMark, entityLabel } = useCatalogForm({
    entityName: "Правило автоматизации",
    entityKey: "automation_rule",
    initialState: INITIAL_STATE,
    api: api.automationRules,
    listPath: "/settings/automation-rules",
    validate: (s) => {
      if (!s.name) return "Укажите наименование"
      if (!s.eventType) return "Укажите тип события"
      if (!s.actionType) return "Укажите тип действия"
      if (!s.serviceAccountId) return "Выберите сервисный аккаунт"
      return null
    },
    mapFromResponse: (r) => ({
      id: r.id,
      name: r.name,
      eventType: r.eventType,
      actionType: r.actionType,
      serviceAccountId: r.serviceAccountId,
      conditionCel: r.conditionCel || "",
      actionTemplate: r.actionTemplate,
      isActive: r.isActive,
    }),
    mapToUpdate: (s) => ({
      name: s.name,
      eventType: s.eventType,
      actionType: s.actionType,
      serviceAccountId: s.serviceAccountId,
      conditionCel: s.conditionCel || undefined,
      actionTemplate: s.actionTemplate,
      isActive: s.isActive,
    }),
  })

  // Test Feature
  const [isTesting, setIsTesting] = useState(false)
  
  const handleTest = async () => {
     setIsTesting(true)
     try {
       // Mock payload
       const payload = {
          action: "posted",
          entityType: "document",
          doc: { id: "test-doc-123", docTotal: 1000 }
       }
       const res = await api.automationRules.test(idStr, {
           conditionCel: f.conditionCel,
           actionTemplate: f.actionTemplate,
           payload,
       })

       if (!res.conditionMatched) {
           toast.error(`Условие не выполнено. Ошибка: ${res.conditionError || 'Ложь'}`)
       } else if (res.renderError) {
           toast.error(`Ошибка генерации шаблона: ${res.renderError}`)
       } else {
           toast.success("Успешно сгенерировано!", {
             description: (
                <pre className="text-[10px] mt-2 bg-black text-white p-2 rounded-md overflow-x-auto max-h-[200px]">
                   {res.renderedPayload}
                </pre>
             ),
             duration: 10000,
           })
       }
     } catch (err) {
       toast.error("Ошибка при проверке правила")
     } finally {
       setIsTesting(false)
     }
  }

  if (loading) return null

  return (
    <div className="flex h-full flex-col">
      <FormToolbar
        title={`${entityLabel}: ${f.name || idStr}`}
        primaryAction={{
          label: saving ? "Сохранение…" : "Записать и закрыть",
          variant: "default",
          onClick: () => handleSave(true),
        }}
        secondaryActions={[
          { label: "Записать", onClick: () => handleSave(false) },
        ]}
        backHref="/settings/automation-rules"
        onClose={() => router.push("/settings/automation-rules")}
      />

      {error && (
        <div className="bg-destructive/10 border-b border-destructive/20 px-4 py-2 text-sm text-destructive">{error}</div>
      )}

      {deletionMark && (
        <div className="bg-destructive/10 border-b border-destructive/20 px-4 py-2 text-sm text-destructive">
          Объект помечен на удаление
        </div>
      )}

      <ScrollArea className="flex-1">
        <div className="p-6 max-w-5xl space-y-8">
          
          <div className="grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-2">
            <div className="md:col-span-2 flex items-center justify-between">
               <div>
                  <Label className="text-xs text-muted-foreground">Наименование *</Label>
                  <Input className="mt-1 w-full md:w-[400px]" value={f.name} onChange={(e) => { update({ name: e.target.value }); handleChange() }} />
               </div>
               <div className="flex items-center space-x-2 mr-4">
                  <Switch
                    checked={f.isActive}
                    onCheckedChange={(v) => { update({ isActive: v }); handleChange() }}
                  />
                  <Label>Активно</Label>
               </div>
            </div>

            <div>
              <Label className="text-xs text-muted-foreground">Событие *</Label>
              <Input 
                 className="mt-1" 
                 value={f.eventType} 
                 onChange={(e) => { update({ eventType: e.target.value }); handleChange() }} 
                 placeholder="document.goods_receipt.posted" 
              />
            </div>

            <div className="flex gap-4">
                <div className="flex-1">
                  <Label className="text-xs text-muted-foreground">Тип действия *</Label>
                  <Select value={f.actionType} onValueChange={(v) => { update({ actionType: v }); handleChange() }}>
                    <SelectTrigger className="mt-1"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="webhook">Webhook API</SelectItem>
                      <SelectItem value="telegram">Telegram Bot</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex-1">
                  <Label className="text-xs text-muted-foreground">Аккаунт *</Label>
                  <Select value={f.serviceAccountId} onValueChange={(v) => { update({ serviceAccountId: v }); handleChange() }}>
                    <SelectTrigger className="mt-1"><SelectValue placeholder="Выберите..." /></SelectTrigger>
                    <SelectContent>
                      {accounts.filter(a => a.accountType === f.actionType).map(acc => (
                         <SelectItem key={acc.id} value={acc.id}>{acc.name}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
            </div>
          </div>

          <div className="space-y-2">
            <Label className="text-xs text-muted-foreground font-semibold">Условие выполнения (CEL)</Label>
            <div className="border rounded-md overflow-hidden h-[150px]">
              <Editor
                defaultLanguage="go"
                value={f.conditionCel || ""}
                onChange={(v) => { update({ conditionCel: v }); handleChange() }}
                options={{ minimap: { enabled: false }, lineNumbers: 'off', scrollBeyondLastLine: false }}
              />
            </div>
            <p className="text-[10px] text-muted-foreground">Пример: <code>doc.docTotal &gt; 100000 && action == 'posted'</code></p>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label className="text-xs text-muted-foreground font-semibold">Шаблон действия (Go Text Template)</Label>
              <Button variant="secondary" size="sm" onClick={handleTest} disabled={isTesting}>
                 <Play className="w-4 h-4 mr-2" />
                 Проверить правило (Dry Run)
              </Button>
            </div>
            <div className="border rounded-md overflow-hidden h-[250px]">
              <Editor
                defaultLanguage="json"
                value={f.actionTemplate || ""}
                onChange={(v) => { update({ actionTemplate: v || "" }); handleChange() }}
                options={{ minimap: { enabled: false }, scrollBeyondLastLine: false }}
              />
            </div>
            <p className="text-[10px] text-muted-foreground">
               Доступно: <code>&#123;&#123; .doc &#125;&#125;</code>, <code>&#123;&#123; .action &#125;&#125;</code>.
               Функции: <code>&#123;&#123; .doc | json &#125;&#125;</code>, <code>&#123;&#123; .doc | jsonIndent &#125;&#125;</code>
            </p>
          </div>

        </div>
      </ScrollArea>
    </div>
  )
}
