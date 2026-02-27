"use client"

import { useState } from "react"
import {
  Plus,
  MoreHorizontal,
  Shield,
  UserPlus,
  Search,
  Mail,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { cn } from "@/lib/utils"
import type { UserRecord, UserStatus, RoleRecord } from "@/types/settings"

// ── Demo data ───────────────────────────────────────────────────────────

const DEMO_USERS: UserRecord[] = [
  {
    id: "1",
    fullName: "Администратор",
    email: "admin@metapus.io",
    role: "Администратор",
    status: "active",
    lastLogin: "2026-02-25T14:00:00Z",
    createdAt: "2025-01-01T00:00:00Z",
  },
  {
    id: "2",
    fullName: "Иванова Мария",
    email: "m.ivanova@company.ru",
    role: "Бухгалтер",
    status: "active",
    lastLogin: "2026-02-24T09:30:00Z",
    createdAt: "2025-06-15T00:00:00Z",
  },
  {
    id: "3",
    fullName: "Петров Алексей",
    email: "a.petrov@company.ru",
    role: "Менеджер",
    status: "invited",
    lastLogin: null,
    createdAt: "2026-02-20T00:00:00Z",
  },
]

const DEMO_ROLES: RoleRecord[] = [
  {
    id: "1",
    name: "Администратор",
    description: "Полный доступ ко всем разделам системы",
    permissions: ["*"],
    usersCount: 1,
    isSystem: true,
  },
  {
    id: "2",
    name: "Бухгалтер",
    description: "Доступ к финансам, документам и отчётам",
    permissions: ["finance.*", "documents.*", "reports.*", "catalogs.read"],
    usersCount: 1,
    isSystem: false,
  },
  {
    id: "3",
    name: "Менеджер",
    description: "Доступ к продажам, CRM и складу",
    permissions: ["sales.*", "crm.*", "warehouse.read", "catalogs.read"],
    usersCount: 1,
    isSystem: false,
  },
  {
    id: "4",
    name: "Только чтение",
    description: "Просмотр данных без возможности редактирования",
    permissions: ["*.read"],
    usersCount: 0,
    isSystem: true,
  },
]

// ── Status helpers ──────────────────────────────────────────────────────

const STATUS_MAP: Record<UserStatus, { label: string; className: string }> = {
  active: {
    label: "Активен",
    className: "bg-emerald-500/15 text-emerald-600 border-emerald-500/20",
  },
  blocked: {
    label: "Заблокирован",
    className: "bg-destructive/15 text-destructive border-destructive/20",
  },
  invited: {
    label: "Приглашён",
    className: "bg-blue-500/15 text-blue-600 border-blue-500/20",
  },
}

function formatDate(iso: string | null): string {
  if (!iso) return "—"
  return new Date(iso).toLocaleDateString("ru-RU", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  })
}

// ── Sub-tabs ────────────────────────────────────────────────────────────

type SubTab = "users" | "roles"

export function UsersRolesSection() {
  const [activeTab, setActiveTab] = useState<SubTab>("users")
  const [search, setSearch] = useState("")
  const [inviteOpen, setInviteOpen] = useState(false)

  return (
    <div className="space-y-4">
      {/* Toolbar */}
      <div className="flex items-center gap-2">
        <div className="flex rounded-md border bg-muted/40 p-0.5">
          <button
            onClick={() => setActiveTab("users")}
            className={cn(
              "rounded-sm px-3 py-1.5 text-xs font-medium transition-colors",
              activeTab === "users"
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            Пользователи
          </button>
          <button
            onClick={() => setActiveTab("roles")}
            className={cn(
              "rounded-sm px-3 py-1.5 text-xs font-medium transition-colors",
              activeTab === "roles"
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            Роли
          </button>
        </div>

        <div className="relative ml-auto">
          <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Поиск..."
            className="h-8 w-56 pl-8 text-xs"
          />
        </div>

        {activeTab === "users" ? (
          <Button size="sm" onClick={() => setInviteOpen(true)}>
            <UserPlus className="mr-1.5 h-3.5 w-3.5" />
            Пригласить
          </Button>
        ) : (
          <Button size="sm">
            <Plus className="mr-1.5 h-3.5 w-3.5" />
            Создать роль
          </Button>
        )}
      </div>

      <Separator />

      {activeTab === "users" ? (
        <UsersTable users={DEMO_USERS} search={search} />
      ) : (
        <RolesGrid roles={DEMO_ROLES} search={search} />
      )}

      {/* Invite dialog */}
      <InviteUserDialog open={inviteOpen} onOpenChange={setInviteOpen} />
    </div>
  )
}

// ── Users Table ─────────────────────────────────────────────────────────

function UsersTable({
  users,
  search,
}: {
  users: UserRecord[]
  search: string
}) {
  const filtered = users.filter(
    (u) =>
      u.fullName.toLowerCase().includes(search.toLowerCase()) ||
      u.email.toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div className="rounded-md border">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b bg-muted/40">
            <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground">
              Пользователь
            </th>
            <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground">
              Роль
            </th>
            <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground">
              Статус
            </th>
            <th className="px-3 py-2 text-left text-xs font-medium text-muted-foreground">
              Последний вход
            </th>
            <th className="w-10 px-3 py-2" />
          </tr>
        </thead>
        <tbody>
          {filtered.map((user) => {
            const statusInfo = STATUS_MAP[user.status]
            return (
              <tr
                key={user.id}
                className="border-b last:border-b-0 hover:bg-muted/20 transition-colors"
              >
                <td className="px-3 py-2.5">
                  <div>
                    <p className="font-medium text-foreground">
                      {user.fullName}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {user.email}
                    </p>
                  </div>
                </td>
                <td className="px-3 py-2.5 text-muted-foreground">
                  {user.role}
                </td>
                <td className="px-3 py-2.5">
                  <Badge
                    variant="outline"
                    className={cn(
                      "text-[10px] font-bold uppercase tracking-wider",
                      statusInfo.className
                    )}
                  >
                    {statusInfo.label}
                  </Badge>
                </td>
                <td className="px-3 py-2.5 text-xs text-muted-foreground">
                  {formatDate(user.lastLogin)}
                </td>
                <td className="px-3 py-2.5">
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button variant="ghost" size="icon" className="h-7 w-7">
                        <MoreHorizontal className="h-3.5 w-3.5" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="w-44">
                      <DropdownMenuItem>Редактировать</DropdownMenuItem>
                      <DropdownMenuItem>Сменить роль</DropdownMenuItem>
                      <DropdownMenuItem>Сбросить пароль</DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem className="text-destructive focus:text-destructive">
                        Заблокировать
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </td>
              </tr>
            )
          })}
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
  )
}

// ── Roles Grid ──────────────────────────────────────────────────────────

function RolesGrid({
  roles,
  search,
}: {
  roles: RoleRecord[]
  search: string
}) {
  const filtered = roles.filter(
    (r) =>
      r.name.toLowerCase().includes(search.toLowerCase()) ||
      r.description.toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
      {filtered.map((role) => (
        <div
          key={role.id}
          className="group relative rounded-lg border bg-card p-4 transition-colors hover:border-primary/30"
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
              {role.isSystem && (
                <Badge variant="secondary" className="text-[10px]">
                  Системная
                </Badge>
              )}
            </div>

            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity"
                >
                  <MoreHorizontal className="h-3.5 w-3.5" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-40">
                <DropdownMenuItem disabled={role.isSystem}>
                  Редактировать
                </DropdownMenuItem>
                <DropdownMenuItem>Дублировать</DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  disabled={role.isSystem}
                  className="text-destructive focus:text-destructive"
                >
                  Удалить
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>

          <p className="mt-1.5 text-xs text-muted-foreground">
            {role.description}
          </p>

          <div className="mt-3 flex items-center gap-3 text-xs text-muted-foreground">
            <span>
              {role.usersCount}{" "}
              {role.usersCount === 1 ? "пользователь" : "пользователей"}
            </span>
            <span>·</span>
            <span>
              {role.permissions.length}{" "}
              {role.permissions.length === 1 ? "разрешение" : "разрешений"}
            </span>
          </div>
        </div>
      ))}
      {filtered.length === 0 && (
        <div className="col-span-2 py-8 text-center text-sm text-muted-foreground">
          Роли не найдены
        </div>
      )}
    </div>
  )
}

// ── Invite Dialog ───────────────────────────────────────────────────────

function InviteUserDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Пригласить пользователя</DialogTitle>
          <DialogDescription>
            Отправьте приглашение по электронной почте. Пользователь получит
            ссылку для регистрации.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-2">
          <div>
            <Label className="mb-1.5 text-xs text-muted-foreground">
              Email
            </Label>
            <div className="relative">
              <Mail className="absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                type="email"
                placeholder="user@company.ru"
                className="h-9 pl-9 text-sm"
              />
            </div>
          </div>
          <div>
            <Label className="mb-1.5 text-xs text-muted-foreground">
              ФИО
            </Label>
            <Input
              placeholder="Иванов Иван Иванович"
              className="h-9 text-sm"
            />
          </div>
          <div>
            <Label className="mb-1.5 text-xs text-muted-foreground">
              Роль
            </Label>
            <Select defaultValue="3">
              <SelectTrigger className="h-9 text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {DEMO_ROLES.map((r) => (
                  <SelectItem key={r.id} value={r.id}>
                    {r.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Отмена
          </Button>
          <Button onClick={() => onOpenChange(false)}>
            <UserPlus className="mr-1.5 h-3.5 w-3.5" />
            Отправить приглашение
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
