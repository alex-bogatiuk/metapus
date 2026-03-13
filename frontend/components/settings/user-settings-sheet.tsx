"use client"

import { useCallback, useEffect, useState } from "react"
import {
  Loader2,
  LogIn,
  Save,
  Shield,
  ShieldCheck,
  UserX,
  UserCheck,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Separator } from "@/components/ui/separator"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { api, ApiError } from "@/lib/api"
import { toast } from "sonner"
import type {
  UserResponse,
  RoleResponse,
  SecurityProfileResponse,
} from "@/types/security"
import { EffectiveAccessDialog } from "@/components/settings/effective-access-dialog"
import { useAuthStore } from "@/stores/useAuthStore"
import { useRouter } from "next/navigation"

// ── Props ────────────────────────────────────────────────────────────────

interface UserSettingsSheetProps {
  user: UserResponse | null
  onClose: (saved: boolean) => void
}

// ── Component ────────────────────────────────────────────────────────────

export function UserSettingsSheet({ user, onClose }: UserSettingsSheetProps) {
  const open = !!user
  const router = useRouter()
  const currentUserId = useAuthStore((s) => s.user?.id)

  // User info state
  const [firstName, setFirstName] = useState("")
  const [lastName, setLastName] = useState("")
  const [isAdmin, setIsAdmin] = useState(false)

  // Roles state
  const [allRoles, setAllRoles] = useState<RoleResponse[]>([])
  const [selectedRoleCodes, setSelectedRoleCodes] = useState<Set<string>>(new Set())
  const [rolesLoading, setRolesLoading] = useState(false)

  // Security profile state
  const [allProfiles, setAllProfiles] = useState<SecurityProfileResponse[]>([])
  const [selectedProfileId, setSelectedProfileId] = useState("")
  const [profilesLoading, setProfilesLoading] = useState(false)

  // Block confirmation
  const [blockConfirmOpen, setBlockConfirmOpen] = useState(false)

  // Effective access
  const [effectiveUser, setEffectiveUser] = useState<UserResponse | null>(null)

  // Save state
  const [saving, setSaving] = useState(false)

  // Initialize state from user
  useEffect(() => {
    if (!user) return
    setFirstName(user.firstName ?? "")
    setLastName(user.lastName ?? "")
    setIsAdmin(user.isAdmin)
    setSelectedRoleCodes(new Set(user.roles?.map((r) => r.code) ?? []))
    setSelectedProfileId(user.securityProfile?.id ?? "")
  }, [user])

  // Load roles and profiles
  useEffect(() => {
    if (!user) return
    setRolesLoading(true)
    setProfilesLoading(true)
    api.roles.list()
      .then((res) => setAllRoles(res.items ?? []))
      .catch(() => {})
      .finally(() => setRolesLoading(false))
    api.security.profiles.list()
      .then((res) => setAllProfiles(res.items ?? []))
      .catch(() => {})
      .finally(() => setProfilesLoading(false))
  }, [user])

  const toggleRole = (code: string) => {
    setSelectedRoleCodes((prev) => {
      const next = new Set(prev)
      if (next.has(code)) next.delete(code)
      else next.add(code)
      return next
    })
  }

  const handleSave = useCallback(async () => {
    if (!user) return
    setSaving(true)
    try {
      // 1. Update user info
      await api.users.update(user.id, { firstName, lastName, isAdmin })

      // 2. Sync roles (add new, remove old)
      const currentCodes = new Set(user.roles?.map((r) => r.code) ?? [])
      for (const code of selectedRoleCodes) {
        if (!currentCodes.has(code)) {
          await api.auth.assignRole({ userId: user.id, roleCode: code })
        }
      }
      for (const code of currentCodes) {
        if (!selectedRoleCodes.has(code)) {
          await api.auth.revokeRole({ userId: user.id, roleCode: code })
        }
      }

      // 3. Sync security profile
      const currentProfileId = user.securityProfile?.id ?? ""
      if (selectedProfileId !== currentProfileId) {
        if (currentProfileId) {
          await api.security.profiles.removeUser(currentProfileId, user.id)
        }
        if (selectedProfileId) {
          await api.security.profiles.assignUser(selectedProfileId, user.id)
        }
      }

      toast.success("Настройки пользователя сохранены")
      onClose(true)
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "Ошибка сохранения")
    } finally {
      setSaving(false)
    }
  }, [user, firstName, lastName, isAdmin, selectedRoleCodes, selectedProfileId, onClose])

  const handleToggleBlock = async () => {
    if (!user) return
    try {
      await api.users.update(user.id, { isActive: !user.isActive })
      toast.success(user.isActive ? "Пользователь заблокирован" : "Пользователь разблокирован")
      setBlockConfirmOpen(false)
      onClose(true)
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "Ошибка")
    }
  }

  return (
    <>
      <Sheet open={open} onOpenChange={(o) => !o && onClose(false)}>
        <SheetContent className="w-full sm:max-w-lg p-0 flex flex-col">
          <SheetHeader className="px-6 py-4 border-b shrink-0">
            <div className="flex items-center justify-between pr-10">
              <div>
                <SheetTitle className="text-base">
                  {user?.fullName || user?.email}
                </SheetTitle>
                <p className="text-xs text-muted-foreground mt-0.5">{user?.email}</p>
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
          </SheetHeader>

          <ScrollArea className="flex-1">
            <div className="px-6 py-5 space-y-6">

              {/* ── Section: User Info ──────────────────────────────── */}
              <section className="space-y-3">
                <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  Данные пользователя
                </h3>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <Label className="text-xs text-muted-foreground mb-1">Имя</Label>
                    <Input
                      value={firstName}
                      onChange={(e) => setFirstName(e.target.value)}
                      className="h-9 text-sm"
                    />
                  </div>
                  <div>
                    <Label className="text-xs text-muted-foreground mb-1">Фамилия</Label>
                    <Input
                      value={lastName}
                      onChange={(e) => setLastName(e.target.value)}
                      className="h-9 text-sm"
                    />
                  </div>
                </div>
                <div className="flex items-center justify-between rounded-md border px-3 py-2.5">
                  <div>
                    <Label className="text-sm">Администратор</Label>
                    <p className="text-[11px] text-muted-foreground">Полный доступ ко всем настройкам</p>
                  </div>
                  <Switch checked={isAdmin} onCheckedChange={setIsAdmin} />
                </div>
              </section>

              <Separator />

              {/* ── Section: Roles ──────────────────────────────────── */}
              <section className="space-y-3">
                <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  Роли
                </h3>
                <p className="text-[11px] text-muted-foreground">
                  Роли определяют базовые права: доступ к документам, справочникам, операциям
                </p>
                {rolesLoading ? (
                  <div className="flex items-center gap-2 py-3 text-xs text-muted-foreground">
                    <Loader2 className="h-3 w-3 animate-spin" />
                    Загрузка ролей...
                  </div>
                ) : (
                  <div className="space-y-1">
                    {allRoles.map((role) => (
                      <label
                        key={role.id}
                        className="flex items-center gap-2.5 rounded-md border px-3 py-1.5 cursor-pointer hover:bg-muted/40 transition-colors"
                      >
                        <Checkbox
                          checked={selectedRoleCodes.has(role.code)}
                          onCheckedChange={() => toggleRole(role.code)}
                          className="h-4 w-4"
                        />
                        <span className="text-sm font-medium">{role.name}</span>
                        <span className="text-[10px] text-muted-foreground font-mono">{role.code}</span>
                        {role.isSystem && (
                          <Badge variant="secondary" className="text-[10px] h-4">Системная</Badge>
                        )}
                      </label>
                    ))}
                    {allRoles.length === 0 && (
                      <p className="text-xs text-muted-foreground py-2">Нет доступных ролей</p>
                    )}
                  </div>
                )}
              </section>

              <Separator />

              {/* ── Section: Security Profile ──────────────────────── */}
              <section className="space-y-3">
                <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  Профиль безопасности
                </h3>
                <p className="text-[11px] text-muted-foreground">
                  Дополнительные ограничения: доступ к данным по организациям, скрытие полей, условия
                </p>
                {profilesLoading ? (
                  <div className="flex items-center gap-2 py-3 text-xs text-muted-foreground">
                    <Loader2 className="h-3 w-3 animate-spin" />
                    Загрузка профилей...
                  </div>
                ) : (
                  <Select value={selectedProfileId} onValueChange={setSelectedProfileId}>
                    <SelectTrigger className="h-9 text-sm">
                      <SelectValue placeholder="Без профиля" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="none">Без профиля</SelectItem>
                      {allProfiles.map((p) => (
                        <SelectItem key={p.id} value={p.id}>
                          <div className="flex items-center gap-2">
                            <ShieldCheck className="h-3 w-3 text-muted-foreground" />
                            {p.name}
                            <span className="text-xs text-muted-foreground font-mono">{p.code}</span>
                          </div>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              </section>

              <Separator />

              {/* ── Section: Actions ───────────────────────────────── */}
              <section className="space-y-3">
                <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  Действия
                </h3>
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-8 text-xs"
                    onClick={() => setEffectiveUser(user)}
                  >
                    <Shield className="mr-1.5 h-3.5 w-3.5" />
                    Эффективные права
                  </Button>
                  {user && user.isActive && user.id !== currentUserId && (
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-8 text-xs"
                      onClick={async () => {
                        try {
                          const res = await api.users.impersonate(user.id)
                          useAuthStore.getState().startImpersonation(res.tokens, res.user as import("@/types/auth").AuthUserResponse)
                          onClose(false)
                          router.push("/")
                          router.refresh()
                        } catch (err) {
                          if (err instanceof ApiError) toast.error(err.message)
                          else toast.error("Не удалось войти от имени пользователя")
                        }
                      }}
                    >
                      <LogIn className="mr-1.5 h-3.5 w-3.5" />
                      Войти от имени
                    </Button>
                  )}
                  {user?.isActive ? (
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-8 text-xs text-destructive hover:text-destructive"
                      onClick={() => setBlockConfirmOpen(true)}
                    >
                      <UserX className="mr-1.5 h-3.5 w-3.5" />
                      Заблокировать
                    </Button>
                  ) : (
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-8 text-xs text-emerald-600 hover:text-emerald-600"
                      onClick={() => setBlockConfirmOpen(true)}
                    >
                      <UserCheck className="mr-1.5 h-3.5 w-3.5" />
                      Разблокировать
                    </Button>
                  )}
                </div>
              </section>

            </div>
          </ScrollArea>
        </SheetContent>
      </Sheet>

      {/* Block/Unblock confirmation */}
      <AlertDialog open={blockConfirmOpen} onOpenChange={setBlockConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {user?.isActive ? "Заблокировать пользователя?" : "Разблокировать пользователя?"}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {user?.isActive
                ? `Пользователь «${user?.fullName || user?.email}» не сможет войти в систему.`
                : `Пользователь «${user?.fullName || user?.email}» снова сможет войти в систему.`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Отмена</AlertDialogCancel>
            <AlertDialogAction
              className={user?.isActive
                ? "bg-destructive text-destructive-foreground hover:bg-destructive/90"
                : ""}
              onClick={handleToggleBlock}
            >
              {user?.isActive ? "Заблокировать" : "Разблокировать"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Effective Access Dialog */}
      <EffectiveAccessDialog
        user={effectiveUser}
        onClose={() => setEffectiveUser(null)}
      />
    </>
  )
}
