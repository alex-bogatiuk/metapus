"use client"

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { PreferencesContent } from "@/components/settings/preferences-content"

interface UserPreferencesDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function UserPreferencesDialog({ open, onOpenChange }: UserPreferencesDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl px-0 pb-0">
        <DialogHeader className="px-6">
          <DialogTitle>Настройки интерфейса</DialogTitle>
          <DialogDescription>
            Ваши персональные настройки оформления и форматов отображения.
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className="h-[60vh] max-h-[600px] px-6 pb-6">
          <PreferencesContent />
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
