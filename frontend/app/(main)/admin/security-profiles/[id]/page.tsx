"use client"

import { AuditSection } from "@/components/settings/security-profile/audit-section"
import { FlsSection } from "@/components/settings/security-profile/fls-section"
import { RlsSection } from "@/components/settings/security-profile/rls-section"
import { RoleAffinityHint } from "@/components/settings/security-profile/role-affinity-hint"
import { RulesSection } from "@/components/settings/security-profile/rules-section"
import { UsersSection } from "@/components/settings/security-profile/users-section"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { useSecurityProfile } from "@/hooks/useSecurityProfile"
import {
  ArrowLeft,
  Loader2,
  Save
} from "lucide-react"

export default function SecurityProfilePage() {
  const { router, profileId, isNew, profile, form, setForm, rules, setRules, loading, saving, activeTab, setActiveTab, handleSave } = useSecurityProfile()

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="shrink-0 border-b bg-workspace-surface px-6 py-3">
        <div className="flex items-center justify-between max-w-5xl mx-auto">
          <div className="flex items-center gap-3">
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8"
              onClick={() => router.push("/settings")}
            >
              <ArrowLeft className="h-4 w-4" />
            </Button>
            <div>
              <div className="flex items-center gap-2">
                <h1 className="text-base font-semibold">
                  {isNew ? "Новый профиль безопасности" : profile?.name}
                </h1>
                {profile?.isSystem && (
                  <Badge variant="secondary" className="text-[10px]">Системный</Badge>
                )}
              </div>
              {!isNew && (
                <p className="text-xs text-muted-foreground font-mono">{profile?.code}</p>
              )}
            </div>
          </div>
          <Button size="sm" onClick={handleSave} disabled={saving}>
            {saving ? (
              <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
            ) : (
              <Save className="mr-1.5 h-3.5 w-3.5" />
            )}
            Сохранить
          </Button>
        </div>
      </div>

      {/* Content */}
      <ScrollArea className="flex-1">
        <div className="max-w-5xl mx-auto px-6 py-6">
          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList className="w-fit mb-6">
              <TabsTrigger value="general">Основное</TabsTrigger>
              <TabsTrigger value="rls">
                Доступ к данным (RLS)
                {(Object.keys(form.dimensions).length + Object.keys(form.entityDimensions).length) > 0 && (
                  <span className="ml-1.5 inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-foreground/10 px-1 text-[10px] font-medium text-foreground">
                    {Object.keys(form.dimensions).length + Object.keys(form.entityDimensions).length}
                  </span>
                )}
              </TabsTrigger>
              <TabsTrigger value="fls">
                Доступность полей (FLS)
                {form.fieldPolicies.length > 0 && (
                  <span className="ml-1.5 inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-foreground/10 px-1 text-[10px] font-medium text-foreground">
                    {form.fieldPolicies.length}
                  </span>
                )}
              </TabsTrigger>
              <TabsTrigger value="rules">
                Бизнес правила (CEL)
                {rules.filter((r) => r.enabled).length > 0 && (
                  <span className="ml-1.5 inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-foreground/10 px-1 text-[10px] font-medium text-foreground">
                    {rules.filter((r) => r.enabled).length}
                  </span>
                )}
              </TabsTrigger>
              {!isNew && (
                <TabsTrigger value="users">
                  Пользователи
                  {(profile?.userCount ?? 0) > 0 && (
                    <span className="ml-1.5 inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-foreground/10 px-1 text-[10px] font-medium text-foreground">
                      {profile!.userCount}
                    </span>
                  )}
                </TabsTrigger>
              )}
              {!isNew && <TabsTrigger value="audit">История</TabsTrigger>}
            </TabsList>

            <TabsContent value="general" className="mt-0">
              <div className="max-w-xl space-y-4">
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <Label className="text-xs text-muted-foreground">Код</Label>
                    <Input
                      value={form.code}
                      onChange={(e) => setForm((f) => ({ ...f, code: e.target.value }))}
                      placeholder="manager_limited"
                      className="mt-1 h-9 text-sm font-mono"
                      disabled={profile?.isSystem}
                    />
                  </div>
                  <div>
                    <Label className="text-xs text-muted-foreground">Название</Label>
                    <Input
                      value={form.name}
                      onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                      placeholder="Менеджер (ограниченный)"
                      className="mt-1 h-9 text-sm"
                    />
                  </div>
                </div>
                <div>
                  <Label className="text-xs text-muted-foreground">Описание</Label>
                  <Textarea
                    value={form.description}
                    onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
                    placeholder="Описание профиля безопасности..."
                    className="text-sm resize-none"
                    rows={3}
                  />
                </div>
                <RoleAffinityHint form={form} />
              </div>
            </TabsContent>

            <TabsContent value="rls" className="mt-0">
              <RlsSection form={form} setForm={setForm} />
            </TabsContent>

            <TabsContent value="fls" className="mt-0">
              <FlsSection form={form} setForm={setForm} />
            </TabsContent>

            <TabsContent value="rules" className="mt-0">
              <RulesSection
                profileId={isNew ? undefined : profileId}
                rules={rules}
                setRules={setRules}
              />
            </TabsContent>

            {!isNew && (
              <TabsContent value="users" className="mt-0">
                <UsersSection profileId={profileId} />
              </TabsContent>
            )}

            {!isNew && (
              <TabsContent value="audit" className="mt-0">
                <AuditSection profileId={profileId} />
              </TabsContent>
            )}
          </Tabs>
        </div>
      </ScrollArea>
    </div>
  )
}
