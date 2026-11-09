"use client"

import { useCallback } from "react"
import { useCompactMode } from "@/hooks/useCompactMode"
import { usePathname } from "next/navigation"
import Link from "next/link"
import { ArrowLeft, MoreHorizontal, HelpCircle, Link2 } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { FavoriteButton } from "@/components/shared/favorite-button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

export interface FormToolbarMenuItem {
  label: string
  onClick?: () => void
  destructive?: boolean
  disabled?: boolean
}

interface FormToolbarProps {
  title: string
  onHelp?: () => void
  status?: {
    label: string
    variant?: "default" | "secondary" | "outline" | "destructive" | "success"
  }
  primaryAction?: {
    label: string
    variant?: "default" | "success"
    onClick?: () => void
    disabled?: boolean
  }
  secondaryActions?: {
    label: string
    onClick?: () => void
    hideOnDesktop?: boolean
    disabled?: boolean
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
  /** If provided, renders a ⭐ favorite toggle button. */
  favoriteTarget?: {
    entityType: string
    entityId: string
    title: string
    url: string
  }
  children?: React.ReactNode
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
  onHelp,
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
  favoriteTarget,
  children,
}: FormToolbarProps) {
  const resolvedBackHref = backHref
    ? backTargetId
      ? `${backHref}?around=${encodeURIComponent(backTargetId)}`
      : backHref
    : undefined

  const compact = useCompactMode()

  return (
    <div data-material="workspace" className={cn("border-b", sticky && "sticky top-0 z-20")}>
      <div className={cn("flex flex-wrap items-center gap-2 px-4", compact ? "py-1" : "py-2")}>
        <div className="flex items-center gap-1">
          <Button type="button" variant="ghost" size="icon" className="h-7 w-7" aria-label="К списку" onClick={onClose} asChild={!!resolvedBackHref && !onClose}>
            {resolvedBackHref && !onClose ? (
              <Link href={resolvedBackHref} aria-label="К списку">
                <ArrowLeft className="h-3.5 w-3.5" />
              </Link>
            ) : (
              <ArrowLeft className="h-3.5 w-3.5" />
            )}
          </Button>
        </div>

        <div className="mr-4 flex min-w-0 flex-wrap items-center gap-2">
          <div className="min-w-0">
            <h1 className={cn("break-words font-semibold text-foreground", compact ? "text-xs" : "text-sm")}>{title}</h1>
          </div>
          {status && (
            <Badge
              variant="secondary"
              className={cn(
                "rounded-full px-2.5 text-[11px] font-bold uppercase tracking-wider animate-badge-in",
                compact ? "h-5" : "h-6",
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

        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
          {primaryAction && (
            <Button
              type="button"
              size="sm"
              variant={primaryAction.variant === "success" ? "success" : "default"}
              onClick={primaryAction.onClick}
              disabled={primaryAction.disabled}
            >
              {primaryAction.label}
            </Button>
          )}

          {children}

          {secondaryActions
            .filter((a) => !a.hideOnDesktop)
            .map((action, i) => (
              <Button
                type="button"
                key={i}
                variant="outline"
                size="sm"
                onClick={action.onClick}
                disabled={action.disabled}
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

          {(secondaryActions.length > 0 || !!extraMenuItems?.length) && <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button type="button" variant="outline" size="sm" className={cn("ml-auto", !extraMenuItems?.length && "md:hidden")}>
                Ещё
                <MoreHorizontal className="ml-1.5 h-3.5 w-3.5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuGroup>
              {secondaryActions
                .map((action, i) => (
                  <DropdownMenuItem
                    key={`hidden-${i}`}
                    onClick={action.onClick}
                    disabled={action.disabled}
                    className="md:hidden"
                  >
                    {action.label}
                  </DropdownMenuItem>
                ))}
              {(extraMenuItems ?? []).map((item, i) => (
                <DropdownMenuItem
                  key={`extra-${i}`}
                  onClick={item.onClick}
                  disabled={item.disabled}
                  className={item.destructive ? "text-destructive focus:text-destructive" : undefined}
                >
                  {item.label}
                </DropdownMenuItem>
              ))}
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>}

        </div>

        <div className="ml-auto flex shrink-0 items-center gap-1">
          {onHelp && <Button type="button" variant="ghost" size="icon" className="h-7 w-7" aria-label="Справка по настройке аккаунта" onClick={onHelp}>
            <HelpCircle className="h-3.5 w-3.5" />
          </Button>}
          <CopyLinkButton />
          {favoriteTarget && favoriteTarget.entityId && (
            <FavoriteButton
              entityType={favoriteTarget.entityType}
              entityId={favoriteTarget.entityId}
              title={favoriteTarget.title}
              url={favoriteTarget.url}
            />
          )}
        </div>
      </div>
    </div>
  )
}
