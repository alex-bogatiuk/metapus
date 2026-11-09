"use client"

/**
 * MerchantUsersTab — компактная таблица управления доступом пользователей к мерчанту.
 *
 * UX-паттерны:
 * - SAP Fiori SmartTable: compact rows h-10, видно 10+ записей без скролла
 * - 1С inline edit: роль меняется Select-ом прямо в строке, без диалога
 * - Guard Rail: блокировка удаления последнего Владельца
 * - Inline Add Row: [+ Добавить] вставляет пустую строку, Tab-навигация
 *
 * Роли (расширяемо — добавить в MERCHANT_ROLES, таблица перестраивается автоматически):
 * - Owner   (1): полный доступ + управление ключами и пользователями
 * - Manager (2): просмотр документов и операций
 * - Viewer  (3): только чтение
 */

import { useState, useEffect, useCallback, useRef } from "react"
import { api } from "@/lib/api"
import type { MerchantUserItem } from "@/types/merchant-api"
import { MerchantRole, MERCHANT_ROLES } from "@/types/merchant-api"
import { Button } from "@/components/ui/button"
import { UserPicker } from "@/components/shared/user-picker"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
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
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import {
  Users,
  Plus,
  Trash2,
  RefreshCw,
  Check,
  X,
  Crown,
  FileText,
  Eye,
  Loader2,
} from "lucide-react"
import { toast } from "sonner"
import { format } from "date-fns"
import { ru } from "date-fns/locale"
import { cn } from "@/lib/utils"

// ─── Types ───────────────────────────────────────────────────────────────────

interface MerchantUsersTabProps {
  merchantId: string
  /** true when the parent entity hasn't been saved yet */
  isNew?: boolean
}

/** Pending inline-add row state */
interface AddingRow {
  userId: string
  userDisplay: string
  role: MerchantRole
}

// ─── Role metadata ────────────────────────────────────────────────────────────

const _ROLE_ICON: Record<MerchantRole, React.ReactNode> = {
  [MerchantRole.Owner]:   <Crown   className="h-3 w-3 text-amber-500"          />,
  [MerchantRole.Manager]: <FileText className="h-3 w-3 text-blue-500"          />,
  [MerchantRole.Viewer]:  <Eye     className="h-3 w-3 text-muted-foreground"   />,
}

const _ROLE_CLASS: Record<MerchantRole, string> = {
  [MerchantRole.Owner]:   "text-amber-600 dark:text-amber-400",
  [MerchantRole.Manager]: "text-blue-600  dark:text-blue-400",
  [MerchantRole.Viewer]:  "text-muted-foreground",
}

// ─── Main component ────────────────────────────────────────────────────────

export function MerchantUsersTab({ merchantId, isNew }: MerchantUsersTabProps) {
  const [users, setUsers]               = useState<MerchantUserItem[]>([])
  const [loading, setLoading]           = useState(false)
  const [addingRow, setAddingRow]       = useState<AddingRow | null>(null)
  const [savingAdd, setSavingAdd]       = useState(false)
  const [removeTarget, setRemoveTarget] = useState<MerchantUserItem | null>(null)
  /** userId → true while its role change is in flight */
  const [roleSaving, setRoleSaving]     = useState<Record<string, boolean>>({})

  const loadUsers = useCallback(async () => {
    if (!merchantId || isNew) return
    setLoading(true)
    try {
      const items = await api.merchantUsers.list(merchantId)
      setUsers(items)
    } catch {
      toast.error("Не удалось загрузить пользователей")
    } finally {
      setLoading(false)
    }
  }, [merchantId, isNew])

  useEffect(() => { loadUsers() }, [loadUsers])

  // ── Inline role change (optimistic) ──────────────────────────────────────
  const handleRoleChange = async (user: MerchantUserItem, newRole: MerchantRole) => {
    if (newRole === user.role) return
    const prev = users
    // Optimistic update
    setUsers((u) => u.map((x) => x.userId === user.userId ? { ...x, role: newRole } : x))
    setRoleSaving((s) => ({ ...s, [user.userId]: true }))
    try {
      await api.merchantUsers.updateRole(merchantId, user.userId, { role: newRole })
      toast.success("Роль обновлена")
    } catch {
      setUsers(prev) // rollback
      toast.error("Не удалось изменить роль")
    } finally {
      setRoleSaving((s) => { const n = { ...s }; delete n[user.userId]; return n })
    }
  }

  // ── Guard Rail: last Owner check ─────────────────────────────────────────
  const isLastOwner = (user: MerchantUserItem): boolean =>
    user.role === MerchantRole.Owner &&
    users.filter((u) => u.role === MerchantRole.Owner).length === 1

  const handleRemoveClick = (user: MerchantUserItem) => {
    if (isLastOwner(user)) {
      toast.error("Невозможно удалить единственного Владельца. Назначьте другого Владельца перед удалением.")
      return
    }
    setRemoveTarget(user)
  }

  // ── Guard Rail: owner role downgrade ─────────────────────────────────────
  const handleRoleChangeGuarded = (user: MerchantUserItem, newRole: MerchantRole) => {
    if (isLastOwner(user) && newRole !== MerchantRole.Owner) {
      toast.error("Невозможно понизить роль единственного Владельца. Назначьте другого Владельца.")
      return
    }
    handleRoleChange(user, newRole)
  }

  // ── Confirm remove ────────────────────────────────────────────────────────
  const handleRemoveConfirm = async () => {
    if (!removeTarget) return
    try {
      await api.merchantUsers.remove(merchantId, removeTarget.userId)
      toast.success("Доступ пользователя отозван")
      setRemoveTarget(null)
      await loadUsers()
    } catch {
      toast.error("Не удалось отозвать доступ")
    }
  }

  // ── Inline add row ────────────────────────────────────────────────────────
  const startAddRow = () => {
    if (addingRow) return
    setAddingRow({ userId: "", userDisplay: "", role: MerchantRole.Viewer })
  }

  const cancelAdd = () => setAddingRow(null)

  const commitAdd = async () => {
    if (!addingRow?.userId) {
      toast.error("Выберите пользователя")
      return
    }
    setSavingAdd(true)
    try {
      await api.merchantUsers.add(merchantId, { userId: addingRow.userId, role: addingRow.role })
      toast.success("Доступ предоставлен")
      setAddingRow(null)
      await loadUsers()
    } catch {
      toast.error("Не удалось добавить пользователя")
    } finally {
      setSavingAdd(false)
    }
  }

  // ── isNew guard ───────────────────────────────────────────────────────────
  if (isNew) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-center text-muted-foreground gap-3">
        <Users className="h-8 w-8 opacity-40" />
        <p className="text-sm">Сохраните мерчанта, чтобы управлять пользователями</p>
      </div>
    )
  }

  const ownerCount = users.filter((u) => u.role === MerchantRole.Owner).length

  return (
    <TooltipProvider delayDuration={300}>
      <div className="space-y-3">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm font-medium">Пользователи мерчанта</p>
            <p className="text-xs text-muted-foreground mt-0.5">
              Управляйте доступом и ролями · {users.length} пользователь(ей)
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="icon" onClick={loadUsers} disabled={loading} title="Обновить">
              <RefreshCw className={cn("h-4 w-4", loading && "animate-spin")} />
            </Button>
            <Button size="sm" onClick={startAddRow} disabled={!!addingRow}>
              <Plus className="mr-1.5 h-3.5 w-3.5" />
              Добавить
            </Button>
          </div>
        </div>

        {/* Compact table */}
        {users.length === 0 && !addingRow ? (
          <div className="flex flex-col items-center justify-center py-10 text-center text-muted-foreground gap-3 border rounded-lg">
            <Users className="h-7 w-7 opacity-40" />
            <p className="text-sm">Нет привязанных пользователей</p>
            <Button size="sm" variant="outline" onClick={startAddRow}>
              <Plus className="mr-1.5 h-3.5 w-3.5" />
              Добавить первого
            </Button>
          </div>
        ) : (
          <div className="border rounded-lg overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="h-9 text-xs py-0 pl-4">Пользователь</TableHead>
                  <TableHead className="h-9 text-xs py-0 w-[200px]">Роль</TableHead>
                  <TableHead className="h-9 text-xs py-0 w-[110px]">Добавлен</TableHead>
                  <TableHead className="h-9 py-0 w-[52px]" />
                </TableRow>
              </TableHeader>

              <TableBody>
                {/* Existing users */}
                {users.map((user) => (
                  <UserRow
                    key={user.userId}
                    user={user}
                    isRoleSaving={!!roleSaving[user.userId]}
                    isLastOwner={ownerCount === 1 && user.role === MerchantRole.Owner}
                    onRoleChange={(role) => handleRoleChangeGuarded(user, role)}
                    onRemove={() => handleRemoveClick(user)}
                  />
                ))}

                {/* Inline add row */}
                {addingRow && (
                  <AddRow
                    row={addingRow}
                    saving={savingAdd}
                    onChange={setAddingRow}
                    onCommit={commitAdd}
                    onCancel={cancelAdd}
                  />
                )}
              </TableBody>
            </Table>
          </div>
        )}

        {/* Guard Rail: remove confirm */}
        <AlertDialog open={!!removeTarget} onOpenChange={() => setRemoveTarget(null)}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Отозвать доступ?</AlertDialogTitle>
              <AlertDialogDescription>
                Пользователь{" "}
                <span className="font-medium">
                  {removeTarget?.userFullName || removeTarget?.userEmail || removeTarget?.userId.slice(0, 8) + "…"}
                </span>{" "}
                потеряет доступ к этому мерчанту.
                Доступ можно восстановить повторным добавлением.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Отмена</AlertDialogCancel>
              <AlertDialogAction
                className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                onClick={handleRemoveConfirm}
              >
                <Trash2 className="mr-1.5 h-3.5 w-3.5" />
                Отозвать доступ
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </TooltipProvider>
  )
}

// ─── Existing user row ────────────────────────────────────────────────────────

interface UserRowProps {
  user: MerchantUserItem
  isRoleSaving: boolean
  isLastOwner: boolean
  onRoleChange: (role: MerchantRole) => void
  onRemove: () => void
}

function UserRow({ user, isRoleSaving, isLastOwner, onRoleChange, onRemove }: UserRowProps) {
  const displayName = user.userFullName || user.userEmail || null
  const subtitle    = user.userFullName && user.userEmail ? user.userEmail : null

  return (
    <TableRow className="h-10 group">
      {/* Identity */}
      <TableCell className="py-0 pl-4">
        <div className="flex flex-col justify-center min-w-0">
          <span className="text-sm truncate font-medium leading-tight">
            {displayName ?? <span className="font-mono text-xs text-muted-foreground">{user.userId.slice(0, 8)}…</span>}
          </span>
          {subtitle && (
            <span className="text-[11px] text-muted-foreground truncate leading-tight">{subtitle}</span>
          )}
        </div>
      </TableCell>

      {/* Inline role select */}
      <TableCell className="py-0">
        <div className="flex items-center gap-1.5">
          {isRoleSaving
            ? <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
            : _ROLE_ICON[user.role]
          }
          <Select
            value={String(user.role)}
            onValueChange={(v) => onRoleChange(Number(v) as MerchantRole)}
            disabled={isRoleSaving}
          >
            <SelectTrigger
              className={cn(
                "h-7 border-0 bg-transparent shadow-none text-xs font-medium w-auto gap-1 px-1",
                "hover:bg-muted focus:ring-0 focus:ring-offset-0",
                _ROLE_CLASS[user.role],
              )}
            >
              {/* Show only label in trigger — not the description */}
              <span>{MERCHANT_ROLES.find((r) => r.value === user.role)?.label ?? ""}</span>
            </SelectTrigger>
            <SelectContent align="start">
              {MERCHANT_ROLES.map((r) => (
                <SelectItem key={r.value} value={String(r.value)} className="text-xs">
                  <div className="flex flex-col">
                    <span className="font-medium">{r.label}</span>
                    <span className="text-[10px] text-muted-foreground">{r.description}</span>
                  </div>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </TableCell>

      {/* Date */}
      <TableCell className="py-0 text-xs text-muted-foreground">
        {fmtDate(user.createdAt)}
      </TableCell>

      {/* Remove */}
      <TableCell className="py-0 pr-2">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className={cn(
                "h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity",
                isLastOwner
                  ? "text-muted-foreground/40 cursor-not-allowed"
                  : "text-muted-foreground hover:text-destructive",
              )}
              onClick={onRemove}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="left" className="text-xs">
            {isLastOwner ? "Единственный Владелец — удаление заблокировано" : "Отозвать доступ"}
          </TooltipContent>
        </Tooltip>
      </TableCell>
    </TableRow>
  )
}

// ─── Inline add row ───────────────────────────────────────────────────────────

interface AddRowProps {
  row: AddingRow
  saving: boolean
  onChange: (row: AddingRow) => void
  onCommit: () => void
  onCancel: () => void
}

function AddRow({ row, saving, onChange, onCommit, onCancel }: AddRowProps) {
  const roleSelectRef = useRef<HTMLButtonElement>(null)

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") { onCancel(); return }
    if (e.key === "Enter")  { onCommit(); return }
  }

  return (
    <TableRow className="h-10 bg-muted/30 border-t-2 border-primary/20">
      {/* UserPicker */}
      <TableCell className="py-1 pl-4" onKeyDown={handleKeyDown}>
        <UserPicker
          value={row.userId}
          displayName={row.userDisplay}
          onChange={(id, display) => {
            onChange({ ...row, userId: id, userDisplay: display })
            // After selecting user — focus role select
            if (id) setTimeout(() => roleSelectRef.current?.click(), 50)
          }}
          placeholder="Выберите пользователя…"
          className="h-7 text-xs border-dashed"
        />
      </TableCell>

      {/* Role select */}
      <TableCell className="py-1" onKeyDown={handleKeyDown}>
        <Select
          value={String(row.role)}
          onValueChange={(v) => onChange({ ...row, role: Number(v) as MerchantRole })}
        >
          <SelectTrigger
            ref={roleSelectRef}
            className="h-7 text-xs border-dashed w-full"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent align="start">
            {MERCHANT_ROLES.map((r) => (
              <SelectItem key={r.value} value={String(r.value)} className="text-xs">
                <div className="flex flex-col">
                  <span className="font-medium">{r.label}</span>
                  <span className="text-[10px] text-muted-foreground">{r.description}</span>
                </div>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </TableCell>

      {/* Date placeholder */}
      <TableCell className="py-1 text-xs text-muted-foreground">—</TableCell>

      {/* Commit / Cancel */}
      <TableCell className="py-1 pr-2">
        <div className="flex items-center gap-0.5">
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 text-emerald-600 hover:text-emerald-700 hover:bg-emerald-50 dark:hover:bg-emerald-950/30"
            onClick={onCommit}
            disabled={saving || !row.userId}
            title="Сохранить (Enter)"
          >
            {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5" />}
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 text-muted-foreground hover:text-destructive"
            onClick={onCancel}
            disabled={saving}
            title="Отмена (Esc)"
          >
            <X className="h-3.5 w-3.5" />
          </Button>
        </div>
      </TableCell>
    </TableRow>
  )
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function fmtDate(iso: string): string {
  try { return format(new Date(iso), "dd MMM yyyy", { locale: ru }) }
  catch { return iso }
}
