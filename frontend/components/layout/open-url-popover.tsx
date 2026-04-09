"use client"

import * as React from "react"
import { Plus } from "lucide-react"
import { useRouter } from "next/navigation"
import { toast } from "sonner"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { useTabsStore } from "@/stores/useTabsStore"
import { resolveTitleFromUrl } from "@/lib/tab-utils"

interface OpenUrlPopoverProps {
  /** Ref exposed so keyboard shortcut (Ctrl+L) can open the popover */
  triggerRef?: React.RefObject<HTMLButtonElement | null>
}

export function OpenUrlPopover({ triggerRef }: OpenUrlPopoverProps) {
  const router = useRouter()
  const openTab = useTabsStore((s) => s.openTab)

  const [open, setOpen] = React.useState(false)
  const [urlInput, setUrlInput] = React.useState("")
  const inputRef = React.useRef<HTMLInputElement>(null)

  // Auto-focus input when popover opens
  React.useEffect(() => {
    if (open) {
      setTimeout(() => inputRef.current?.focus(), 50)
    }
  }, [open])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const raw = urlInput.trim()
    if (!raw) return

    let pathname: string
    try {
      const url = new URL(raw, window.location.origin)
      if (url.origin !== window.location.origin) {
        toast.error("Можно открывать только страницы Metapus")
        return
      }
      pathname = url.pathname
    } catch {
      // Treat as relative path
      pathname = raw.startsWith("/") ? raw : `/${raw}`
    }

    const result = openTab({
      id: pathname,
      title: resolveTitleFromUrl(pathname),
      url: pathname,
    })
    if (result.warning) toast.warning(result.warning)

    router.push(pathname)
    setUrlInput("")
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>
            <button
              ref={triggerRef}
              className="flex h-[36px] w-8 shrink-0 items-center justify-center rounded-t-md text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors"
              aria-label="Открыть страницу по ссылке"
            >
              <Plus className="h-3.5 w-3.5" />
            </button>
          </PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent side="bottom">
          <p>Открыть страницу по ссылке (Ctrl+L)</p>
        </TooltipContent>
      </Tooltip>
      <PopoverContent className="w-80 p-3" align="end">
        <div className="space-y-2">
          <p className="text-xs font-medium text-muted-foreground">
            Вставьте ссылку на страницу
          </p>
          <form onSubmit={handleSubmit} className="flex gap-2">
            <Input
              ref={inputRef}
              placeholder="Например /documents/goods-receipts/..."
              className="h-8 text-xs"
              value={urlInput}
              onChange={(e) => setUrlInput(e.target.value)}
            />
            <Button type="submit" size="sm" className="h-8 shrink-0">
              Открыть
            </Button>
          </form>
        </div>
      </PopoverContent>
    </Popover>
  )
}
