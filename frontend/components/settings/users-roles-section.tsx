"use client"

import { useCallback, useEffect, useState } from "react"
import { useTabState } from "@/hooks/useTabState"
import {
  Shield,
  ShieldCheck,
  Search,
  Loader2,
  Plus,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { cn } from "@/lib/utils"
import { api, ApiError } from "@/lib/api"
import { toast } from "sonner"
import type {
  UserResponse,
  RoleResponse,
  PermissionResponse,
} from "@/types/security"
import { RoleEditorSheet } from "@/components/settings/role-editor-sheet"
import { UserSettingsSheet } from "@/components/settings/user-settings-sheet"

// ── Sub-tabs ────────────────────────────────────────────────────────────

type SubTab = "users" | "roles"

export function UsersRolesSection() {
  const [activeTab, setActiveTab] = useTabState<SubTab>("activeTab", "users")
  const [search, setSearch] = useState("")
  const [createOpen, setCreateOpen] = useState(false)
  const [reloadKey, setReloadKey] = useState(0)
  const [createRole, setCreateRole] = useState(false)

  return (
    <div className="space-y-4">
      {/* Toolbar */}
      <div className="flex items-center gap-2">
        <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as SubTab)}>
          <TabsList>
            <TabsTrigger value="users">Пользователи</TabsTrigger>
            <TabsTrigger value="roles">Роли</TabsTrigger>
          </TabsList>
        </Tabs>

        <div className="relative ml-auto flex items-center gap-2">
          <div className="relative">
            <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Поиск..."
              className="h-8 w-56 pl-8 text-xs"
            />
          </div>
          {activeTab === "users" ? (
            <Button size="sm" className="h-8 text-xs" onClick={() => setCreateOpen(true)}>
              <Plus className="mr-1.5 h-3.5 w-3.5" />
              Создать пользователя
            </Button>
          ) : (
            <Button size="sm" className="h-8 text-xs" onClick={() => setCreateRole(true)}>
              <Plus className="mr-1.5 h-3.5 w-3.5" />
              Создать роль
            </Button>
          )}
        </div>
      </div>

      <Separator />

      {activeTab === "users" ? (
        <UsersTable search={search} reloadKey={reloadKey} />
      ) : (
        <RolesGrid
          search={search}
          createRole={createRole}
          onCreateConsumed={() => setCreateRole(false)}
        />
      )}

      <CreateUserDialog
        open={createOpen}
        onClose={(saved: boolean) => {
          setCreateOpen(false)
          if (saved) setReloadKey((k) => k + 1)
        }}
      />
    </div>
  )
}

// ── Users Table ─────────────────────────────────────────────────────────

function UsersTable({ search, reloadKey }: { search: string; reloadKey: number }) {
  const [users, setUsers] = useState<UserResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [selectedUser, setSelectedUser] = useState<UserResponse | null>(null)

  const loadUsers = useCallback(async () => {
    try {
      setLoading(true)
      const res = await api.users.list()
      setUsers(res.items ?? [])
    } catch (e) {
      toast.error("Не удалось загрузить пользователей")
      console.error(e)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadUsers() }, [loadUsers, reloadKey])

  const filtered = users.filter(
    (u) =>
      u.fullName.toLowerCase().includes(search.toLowerCase()) ||
      u.email.toLowerCase().includes(search.toLowerCase())
  )

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12 text-sm text-muted-foreground">
        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
        Загрузка...
      </div>
    )
  }

  return (
    <>
      <div className="rounded-md border">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b bg-muted/40">
              <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground">
                Пользователь
              </th>
              <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground">
                Роли
              </th>
              <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground">
                Профиль безопасности
              </th>
              <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground">
                Статус
              </th>
              <th className="w-10 px-3 py-2" />
            </tr>
          </thead>
          <tbody>
            {filtered.map((user) => (
              <tr
                key={user.id}
                className="border-b last:border-b-0 hover:bg-muted/20 transition-colors cursor-pointer"
                onClick={() => setSelectedUser(user)}
              >
                <td className="px-3 py-2.5">
                  <div>
                    <p className="font-medium text-foreground">
                      {user.fullName || user.email}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {user.email}
                    </p>
                  </div>
                </td>
                <td className="px-3 py-2.5">
                  <div className="flex flex-wrap gap-1">
                    {user.roles && user.roles.length > 0 ? (
                      user.roles.map((r) => (
                        <Badge
                          key={r.id}
                          variant="outline"
                          className="text-[10px]"
                        >
                          {r.name}
                        </Badge>
                      ))
                    ) : (
                      <span className="text-xs text-muted-foreground">—</span>
                    )}
                  </div>
                </td>
                <td className="px-3 py-2.5">
                  {user.securityProfile ? (
                    <Badge variant="outline" className="text-[10px]">
                      <ShieldCheck className="mr-1 h-3 w-3" />
                      {user.securityProfile.name}
                    </Badge>
                  ) : (
                    <span className="text-xs text-muted-foreground">—</span>
                  )}
                </td>
                <td className="px-3 py-2.5">
                  <Badge
                    variant="outline"
                    className={cn(
                      "text-[10px] font-bold uppercase tracking-wider",
                      user.isActive
                        ? "bg-success/15 text-success border-success/20"
                        : "bg-destructive/15 text-destructive border-destructive/20"
                    )}
                  >
                    {user.isActive ? "Активен" : "Неактивен"}
                  </Badge>
                  {user.isAdmin && (
                    <Badge variant="secondary" className="ml-1 text-[10px]">
                      Admin
                    </Badge>
                  )}
                </td>
                <td className="px-3 py-2.5" />
              </tr>
            ))}
            {filtered.length === 0 && (
              <tr>
                <td
                  colSpan={5}
                  className="px-3 py-8 text-center text-sm text-muted-foreground"
                >
                  Пользователи не найдены
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* User Settings Sheet */}
      <UserSettingsSheet
        user={selectedUser}
        onClose={(saved) => {
          setSelectedUser(null)
          if (saved) loadUsers()
        }}
      />

    </>
  )
}

// ── Roles Grid ──────────────────────────────────────────────────────────

function RolesGrid({ search, createRole, onCreateConsumed }: { search: string; createRole: boolean; onCreateConsumed: () => void }) {
  const [roles, setRoles] = useState<RoleResponse[]>([])
  const [allPermissions, setAllPermissions] = useState<PermissionResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [selectedRole, setSelectedRole] = useState<RoleResponse | "new" | null>(null)
  const [selectedRolePerms, setSelectedRolePerms] = useState<Set<string>>(new Set())

  const loadRoles = useCallback(async () => {
    try {
      setLoading(true)
      const [rolesRes, permsRes] = await Promise.all([
        api.roles.list(),
        api.permissions.list(),
      ])
      setRoles(rolesRes.items ?? [])
      setAllPermissions(permsRes.items ?? [])
    } catch (e) {
      toast.error("Не удалось загрузить роли")
      console.error(e)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadRoles() }, [loadRoles])

  // Open create sheet when parent signals
  useEffect(() => {
    if (createRole) {
      setSelectedRolePerms(new Set())
      setSelectedRole("new")
      onCreateConsumed()
    }
  }, [createRole, onCreateConsumed])

  const handleSelectRole = useCallback(async (role: RoleResponse) => {
    try {
      const detailed = await api.roles.get(role.id)
      const res = await api.roles.getPermissions(role.id)
      setSelectedRolePerms(new Set((res.items ?? []).map((p) => p.code)))
      setSelectedRole(detailed)
    } catch {
      setSelectedRolePerms(new Set())
      setSelectedRole(role)
    }
  }, [])

  const filtered = roles.filter(
    (r) =>
      r.name.toLowerCase().includes(search.toLowerCase()) ||
      (r.description ?? "").toLowerCase().includes(search.toLowerCase())
  )

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12 text-sm text-muted-foreground">
        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
        Загрузка...
      </div>
    )
  }

  return (
    <>
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
      {filtered.map((role) => (
        <div
          key={role.id}
          className="group relative rounded-lg border bg-card p-4 transition-colors hover:border-primary/30 cursor-pointer"
          onClick={() => handleSelectRole(role)}
        >
          <div className="flex items-start justify-between">
            <div className="flex items-center gap-2">
              <Shield
                className={cn(
                  "h-4 w-4",
                  role.isSystem ? "text-primary" : "text-muted-foreground"
                )}
              />
              <h4 className="text-sm font-medium text-foreground">
                {role.name}
              </h4>
              <span className="text-xs text-muted-foreground font-mono">
                {role.code}
              </span>
              {role.isSystem && (
                <Badge variant="secondary" className="text-[10px]">
                  Системная
                </Badge>
              )}
            </div>
          </div>

          {role.description && (
            <p className="mt-1.5 text-xs text-muted-foreground">
              {role.description}
            </p>
          )}
        </div>
      ))}
      {filtered.length === 0 && (
        <div className="col-span-2 py-8 text-center text-sm text-muted-foreground">
          Роли не найдены
        </div>
      )}
    </div>
    <RoleEditorSheet
      role={selectedRole}
      permissions={allPermissions}
      rolePerms={selectedRolePerms}
      onClose={(saved) => {
        setSelectedRole(null)
        if (saved) loadRoles()
      }}
    />
    </>
  )
}

// ── Create User Dialog ───────────────────────────────────────────────

function CreateUserDialog({
  open,
  onClose,
}: {
  open: boolean
  onClose: (saved: boolean) => void
}) {
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [firstName, setFirstName] = useState("")
  const [lastName, setLastName] = useState("")
  const [saving, setSaving] = useState(false)

  const handleSave = async () => {
    if (!email || !password) {
      toast.error("Email и пароль обязательны")
      return
    }
    setSaving(true)
    try {
      await api.users.create({ email, password, firstName: firstName || undefined, lastName: lastName || undefined })
      toast.success("Пользователь создан")
      setEmail("")
      setPassword("")
      setFirstName("")
      setLastName("")
      onClose(true)
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "Ошибка создания пользователя")
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose(false)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Новый пользователь</DialogTitle>
          <DialogDescription>Создайте учётную запись для нового пользователя</DialogDescription>
        </DialogHeader>
        <div className="space-y-3 py-2">
          <div>
            <Label className="text-xs text-muted-foreground">Email *</Label>
            <Input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="user@example.com"
              className="h-9 text-sm"
            />
          </div>
          <div>
            <Label className="text-xs text-muted-foreground">Пароль *</Label>
            <Input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Минимум 8 символов"
              className="h-9 text-sm"
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label className="text-xs text-muted-foreground">Имя</Label>
              <Input
                value={firstName}
                onChange={(e) => setFirstName(e.target.value)}
                className="h-9 text-sm"
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Фамилия</Label>
              <Input
                value={lastName}
                onChange={(e) => setLastName(e.target.value)}
                className="h-9 text-sm"
              />
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => onClose(false)}>
            Отмена
          </Button>
          <Button size="sm" onClick={handleSave} disabled={saving}>
            {saving && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
            Создать
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
