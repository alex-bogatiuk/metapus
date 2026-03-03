"use client"

import Link from "next/link"
import { ArrowLeft, ArrowRight, MoreHorizontal, HelpCircle, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

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
  backHref?: string
  onClose?: () => void
  sticky?: boolean
}

import { cn } from "@/lib/utils"

export function FormToolbar({
  title,
  status,
  primaryAction,
  secondaryActions = [],
  backHref,
  onClose,
  sticky = true,
}: FormToolbarProps) {
  return (
    <div className={cn("border-b bg-card", sticky && "sticky top-0 z-20 shadow-sm")}>
      <div className="flex items-center gap-2 px-4 py-2">
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="icon" className="h-7 w-7" asChild={!!backHref}>
            {backHref ? (
              <Link href={backHref}>
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
            <div
              className={cn(
                "inline-flex h-6 items-center px-2.5 rounded-full border text-[11px] font-bold uppercase tracking-wider",
                status.variant === "success" && "border-success text-success bg-transparent",
                status.variant === "destructive" && "border-destructive text-destructive bg-transparent",
                (status.variant === "outline" || !status.variant) && "border-muted-foreground text-muted-foreground bg-transparent",
                status.variant === "default" && "border-primary text-primary bg-transparent"
              )}
            >
              {status.label}
            </div>
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
              <DropdownMenuItem>Скопировать</DropdownMenuItem>
              <DropdownMenuItem>Пометить на удаление</DropdownMenuItem>
              <DropdownMenuItem>История изменений</DropdownMenuItem>
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
