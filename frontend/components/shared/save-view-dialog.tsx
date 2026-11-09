"use client"

/**
 * SaveViewDialog — minimal dialog for saving the current list state as a named view.
 *
 * Fields: name (required), visibility toggle (personal/shared).
 * Used inside FilterSidebar's saved views dropdown.
 */

import { useState, useCallback } from "react"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogFooter,
    DialogDescription,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import type { ViewVisibility } from "@/types/list-view"

interface SaveViewDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    onSave: (name: string, visibility: ViewVisibility) => Promise<void>
}

export function SaveViewDialog({ open, onOpenChange, onSave }: SaveViewDialogProps) {
    const [name, setName] = useState("")
    const [visibility, setVisibility] = useState<ViewVisibility>("personal")
    const [saving, setSaving] = useState(false)

    const handleSave = useCallback(async () => {
        if (!name.trim()) return
        setSaving(true)
        try {
            await onSave(name.trim(), visibility)
            setName("")
            setVisibility("personal")
            onOpenChange(false)
        } catch {
            // Error is logged by the hook — dialog stays open for retry.
        } finally {
            setSaving(false)
        }
    }, [name, visibility, onSave, onOpenChange])

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-[360px]">
                <DialogHeader>
                    <DialogTitle className="text-sm">Сохранить вид</DialogTitle>
                    <DialogDescription className="sr-only">
                        Сохранить текущие фильтры и настройки как именованный вид
                    </DialogDescription>
                </DialogHeader>

                <div className="flex flex-col gap-3 py-2">
                    <div className="flex flex-col gap-1.5">
                        <Label htmlFor="view-name" className="text-xs">
                            Наименование
                        </Label>
                        <Input
                            id="view-name"
                            className="h-8 text-sm"
                            placeholder="Не проведённые, За сегодня..."
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                            onKeyDown={(e) => {
                                if (e.key === "Enter") {
                                    e.preventDefault()
                                    handleSave()
                                }
                            }}
                            autoFocus
                        />
                    </div>

                    <div className="flex flex-col gap-1.5">
                        <Label className="text-xs">Доступность</Label>
                        <ToggleGroup
                            type="single"
                            variant="outline"
                            value={visibility}
                            onValueChange={(val) => {
                                if (val) setVisibility(val as ViewVisibility)
                            }}
                            className="w-full gap-0 -space-x-px"
                        >
                            <ToggleGroupItem
                                value="personal"
                                className="h-7 text-[11px] px-3 flex-1 rounded-none rounded-l-md data-[state=on]:z-10"
                            >
                                Личный
                            </ToggleGroupItem>
                            <ToggleGroupItem
                                value="shared"
                                className="h-7 text-[11px] px-3 flex-1 rounded-none rounded-r-md data-[state=on]:z-10"
                            >
                                Общий
                            </ToggleGroupItem>
                        </ToggleGroup>
                    </div>
                </div>

                <DialogFooter>
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={() => onOpenChange(false)}
                        disabled={saving}
                    >
                        Отмена
                    </Button>
                    <Button
                        size="sm"
                        onClick={handleSave}
                        disabled={saving || !name.trim()}
                    >
                        {saving ? "Сохранение..." : "Сохранить"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
