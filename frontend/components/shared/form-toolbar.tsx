"use client"

import { useCallback } from "react"
import { usePathname } from "next/navigation"
import Link from "next/link"
import { ArrowLeft, MoreHorizontal, HelpCircle, Link2 } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

export interface FormToolbarMenuItem {
  label: string
  onClick?: () => void
  destructive?: boolean
}

interface FormToolbarProps {
  title: string
  status?: {
    label: string
    variant?: "default" | "secondary" | "outline" | "destructive" | "success"
  }
  primaryAction: {
    label: string
    variant?: "default" | "success"
    onClick?: () => void
  }
  secondaryActions?: {
    label: string
    onClick?: () => void
    hideOnDesktop?: boolean
  }[]
  extraMenuItems?: FormToolbarMenuItem[]
  /** Optional print menu button rendered between secondary actions and "More" (Еще) */
  printMenu?: React.ReactNode
  /** Optional icon-only buttons rendered near printMenu (for movements, related docs, etc.) */
  toolbarIcons?: {
    icon: React.ReactNode
    title: string
    onClick?: () => void
  }[]
  backHref?: string
  backTargetId?: string
  onClose?: () => void
  sticky?: boolean
}

import { cn } from "@/lib/utils"

function CopyLinkButton() {
  const pathname = usePathname()
  const handleCopy = useCallback(() => {
    const fullUrl = `${window.location.origin}${pathname}`
    navigator.clipboard.writeText(fullUrl).then(() => {
      toast.success("Ссылка скопирована в буфер обмена")
    })
  }, [pathname])

  return (
    <Button
      variant="ghost"
      size="icon"
      className="h-7 w-7"
      onClick={handleCopy}
      title="Копировать ссылку"
    >
      <Link2 className="h-3.5 w-3.5" />
    </Button>
  )
}

export function FormToolbar({
  title,
  status,
  primaryAction,
  secondaryActions = [],
  extraMenuItems,
  printMenu,
  toolbarIcons,
  backHref,
  backTargetId,
  onClose,
  sticky = true,
}: FormToolbarProps) {
  const resolvedBackHref = backHref
    ? backTargetId
      ? `${backHref}?around=${encodeURIComponent(backTargetId)}`
      : backHref
    : undefined

  return (
    <div className={cn("border-b bg-card", sticky && "sticky top-0 z-20 shadow-sm")}>
      <div className="flex items-center gap-2 px-4 py-2">
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="icon" className="h-7 w-7" asChild={!!resolvedBackHref}>
            {resolvedBackHref ? (
              <Link href={resolvedBackHref}>
                <ArrowLeft className="h-3.5 w-3.5" />
              </Link>
            ) : (
              <ArrowLeft className="h-3.5 w-3.5" />
            )}
          </Button>
        </div>

        <div className="mr-4 flex items-center gap-2">
          <h1 className="text-sm font-semibold text-foreground">{title}</h1>
          {status && (
            <Badge
              variant="secondary"
              className={cn(
                "h-6 rounded-full px-2.5 text-[11px] font-bold uppercase tracking-wider",
                status.variant === "success" && "bg-success/10 text-success",
                status.variant === "destructive" && "bg-destructive/10 text-destructive",
                (status.variant === "outline" || !status.variant) && "bg-muted text-muted-foreground",
                status.variant === "default" && "bg-primary/10 text-primary-foreground",
                status.variant === "secondary" && "bg-secondary text-secondary-foreground"
              )}
            >
              {status.label}
            </Badge>
          )}
        </div>

        <div className="flex flex-1 items-center gap-2">
          <Button
            size="sm"
            variant={primaryAction.variant === "success" ? "success" : "default"}
            onClick={primaryAction.onClick}
          >
            {primaryAction.label}
          </Button>

          {secondaryActions
            .filter((a) => !a.hideOnDesktop)
            .map((action, i) => (
              <Button
                key={i}
                variant="outline"
                size="sm"
                onClick={action.onClick}
                className="hidden md:flex"
              >
                {action.label}
              </Button>
            ))}

          {printMenu}

          {(toolbarIcons ?? []).map((ti, i) => (
            <Button
              key={i}
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              onClick={ti.onClick}
              title={ti.title}
            >
              {ti.icon}
            </Button>
          ))}

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" className="ml-auto">
                Еще
                <MoreHorizontal className="ml-1.5 h-3.5 w-3.5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              {secondaryActions
                .filter((a) => a.hideOnDesktop)
                .map((action, i) => (
                  <DropdownMenuItem key={`hidden-${i}`} onClick={action.onClick} className="md:hidden">
                    {action.label}
                  </DropdownMenuItem>
                ))}
              {(extraMenuItems ?? []).map((item, i) => (
                <DropdownMenuItem
                  key={`extra-${i}`}
                  onClick={item.onClick}
                  className={item.destructive ? "text-destructive focus:text-destructive" : undefined}
                >
                  {item.label}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>

          <Button variant="ghost" size="icon" className="h-7 w-7">
            <HelpCircle className="h-3.5 w-3.5" />
          </Button>
        </div>

        <CopyLinkButton />
      </div>
    </div>
  )
}
