"use client"

import { useCallback, useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import {
  Plus,
  MoreHorizontal,
  ShieldCheck,
  Search,
  Pencil,
  Trash2,
  Copy,
  Loader2,
  Users,
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
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { cn } from "@/lib/utils"
import { api } from "@/lib/api"
import { toast } from "sonner"
import type { SecurityProfileResponse } from "@/types/security"
import { ProfilePresetPicker, type ProfilePreset } from "@/components/settings/profile-presets"
import { useProfilePresetStore } from "@/stores/useProfilePresetStore"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

export function SecurityProfilesSection() {
  const router = useRouter()
  const [profiles, setProfiles] = useState<SecurityProfileResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState("")
  const [deleteTarget, setDeleteTarget] = useState<SecurityProfileResponse | null>(null)
  const [presetOpen, setPresetOpen] = useState(false)

  const loadProfiles = useCallback(async () => {
    try {
      setLoading(true)
      const res = await api.security.profiles.list()
      setProfiles(res.items ?? [])
    } catch (e) {
      toast.error("Не удалось загрузить профили безопасности")
      console.error(e)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadProfiles() }, [loadProfiles])

  const handleCreate = () => {
    setPresetOpen(true)
  }

  const handlePresetSelect = (preset: ProfilePreset) => {
    setPresetOpen(false)
    useProfilePresetStore.getState().setPreset(preset)
    router.push("/settings/security-profiles/new")
  }

  const handlePresetSkip = () => {
    setPresetOpen(false)
    router.push("/settings/security-profiles/new")
  }

  const handleEdit = (profile: SecurityProfileResponse) => {
    router.push(`/settings/security-profiles/${profile.id}`)
  }

  const handleDuplicate = async (profile: SecurityProfileResponse) => {
    try {
      await api.security.profiles.create({
        code: profile.code + "_copy",
        name: profile.name + " (копия)",
        description: profile.description,
        dimensions: profile.dimensions,
        fieldPolicies: profile.fieldPolicies,
      })
      toast.success("Профиль скопирован")
      loadProfiles()
    } catch {
      toast.error("Не удалось скопировать профиль")
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    try {
      await api.security.profiles.delete(deleteTarget.id)
      toast.success("Профиль удалён")
      setDeleteTarget(null)
      loadProfiles()
    } catch {
      toast.error("Не удалось удалить профиль")
    }
  }

  const filtered = profiles.filter(
    (p) =>
      p.name.toLowerCase().includes(search.toLowerCase()) ||
      p.code.toLowerCase().includes(search.toLowerCase()) ||
      (p.description ?? "").toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div className="space-y-4">
      {/* Toolbar */}
      <div className="flex items-center gap-2">
        <div className="relative flex-1 max-w-xs">
          <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Поиск профилей..."
            className="h-8 pl-8 text-xs"
          />
        </div>
        <Button size="sm" onClick={handleCreate}>
          <Plus className="mr-1.5 h-3.5 w-3.5" />
          Создать профиль
        </Button>
      </div>

      <Separator />

      {/* Profile cards */}
      {loading ? (
        <div className="flex items-center justify-center py-12 text-sm text-muted-foreground">
          <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          Загрузка...
        </div>
      ) : filtered.length === 0 ? (
        <div className="py-12 text-center space-y-2">
          <ShieldCheck className="mx-auto h-8 w-8 text-muted-foreground/40" />
          <p className="text-sm text-muted-foreground">
            {search ? "Профили не найдены" : "Нет профилей безопасности"}
          </p>
          {!search && (
            <p className="text-xs text-muted-foreground/60">
              Профиль объединяет доступ к данным, скрытие полей и условия для управления правами
            </p>
          )}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3">
          {filtered.map((profile) => (
            <ProfileCard
              key={profile.id}
              profile={profile}
              onEdit={() => handleEdit(profile)}
              onDuplicate={() => handleDuplicate(profile)}
              onDelete={() => setDeleteTarget(profile)}
            />
          ))}
        </div>
      )}

      {/* Preset picker dialog */}
      <Dialog open={presetOpen} onOpenChange={(o) => !o && setPresetOpen(false)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Новый профиль безопасности</DialogTitle>
          </DialogHeader>
          <ProfilePresetPicker onSelect={handlePresetSelect} onSkip={handlePresetSkip} />
        </DialogContent>
      </Dialog>

      {/* Delete confirmation */}
      <AlertDialog open={!!deleteTarget} onOpenChange={(o) => !o && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Удалить профиль?</AlertDialogTitle>
            <AlertDialogDescription>
              Профиль «{deleteTarget?.name}» будет удалён. Все привязки пользователей к этому
              профилю будут сняты. Это действие необратимо.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Отмена</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={handleDelete}
            >
              Удалить
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

// ── Profile Card ──────────────────────────────────────────────────────────

function ProfileCard({
  profile,
  onEdit,
  onDuplicate,
  onDelete,
}: {
  profile: SecurityProfileResponse
  onEdit: () => void
  onDuplicate: () => void
  onDelete: () => void
}) {
  const dimCount = profile.dimensions ? Object.keys(profile.dimensions).length : 0
  const flsCount = profile.fieldPolicies?.length ?? 0
  const ruleCount = profile.policyRules?.length ?? 0

  return (
    <div
      className="group relative rounded-lg border bg-card p-4 transition-colors hover:border-primary/30 cursor-pointer"
      onClick={onEdit}
    >
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-2.5">
          <ShieldCheck
            className={cn(
              "h-4 w-4 shrink-0",
              profile.isSystem ? "text-primary" : "text-muted-foreground"
            )}
          />
          <div>
            <div className="flex items-center gap-2">
              <h4 className="text-sm font-medium text-foreground">{profile.name}</h4>
              <span className="text-xs text-muted-foreground font-mono">{profile.code}</span>
              {profile.isSystem && (
                <Badge variant="secondary" className="text-[10px]">Системный</Badge>
              )}
            </div>
            {profile.description && (
              <p className="mt-0.5 text-xs text-muted-foreground">{profile.description}</p>
            )}
          </div>
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity"
              onClick={(e) => e.stopPropagation()}
            >
              <MoreHorizontal className="h-3.5 w-3.5" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-44">
            <DropdownMenuItem onClick={(e) => { e.stopPropagation(); onEdit() }}>
              <Pencil className="mr-2 h-3.5 w-3.5" />
              Редактировать
            </DropdownMenuItem>
            <DropdownMenuItem onClick={(e) => { e.stopPropagation(); onDuplicate() }}>
              <Copy className="mr-2 h-3.5 w-3.5" />
              Дублировать
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              disabled={profile.isSystem}
              className="text-destructive focus:text-destructive"
              onClick={(e) => { e.stopPropagation(); onDelete() }}
            >
              <Trash2 className="mr-2 h-3.5 w-3.5" />
              Удалить
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* Stats badges */}
      <div className="mt-3 flex items-center gap-2 flex-wrap">
        <Badge variant="outline" className="text-[10px] h-5 font-normal gap-1">
          <Users className="h-3 w-3 text-muted-foreground" />
          {profile.userCount} {profile.userCount === 1 ? "пользователь" : profile.userCount >= 2 && profile.userCount <= 4 ? "пользователя" : "пользователей"}
        </Badge>
        {dimCount > 0 && (
          <Badge variant="outline" className="text-[10px] h-5 font-normal gap-1">
            <span className="font-semibold text-blue-600">Доступ</span>
            {dimCount} огр.
          </Badge>
        )}
        {flsCount > 0 && (
          <Badge variant="outline" className="text-[10px] h-5 font-normal gap-1">
            <span className="font-semibold text-violet-600">Поля</span>
            {flsCount} огр.
          </Badge>
        )}
        {ruleCount > 0 && (
          <Badge variant="outline" className="text-[10px] h-5 font-normal gap-1">
            <span className="font-semibold text-amber-600">CEL</span>
            {ruleCount} усл.
          </Badge>
        )}
        {dimCount === 0 && flsCount === 0 && ruleCount === 0 && (
          <span className="text-[11px] text-muted-foreground/60 italic">Без ограничений</span>
        )}
      </div>
    </div>
  )
}
