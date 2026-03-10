"use client"

import Link from "next/link"
import { ArrowLeft, ArrowRight, MoreHorizontal, HelpCircle, X } from "lucide-react"
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
  backHref?: string
  backTargetId?: string
  onClose?: () => void
  sticky?: boolean
}

import { cn } from "@/lib/utils"

export function FormToolbar({
  title,
  status,
  primaryAction,
  secondaryActions = [],
  extraMenuItems,
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
                status.variant === "success" && "bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-300",
                status.variant === "destructive" && "bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-300",
                (status.variant === "outline" || !status.variant) && "bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300",
                status.variant === "default" && "bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300",
                status.variant === "secondary" && "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300"
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

        {onClose && (
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            onClick={onClose}
          >
            <X className="h-3.5 w-3.5" />
          </Button>
        )}
      </div>
    </div>
  )
}
