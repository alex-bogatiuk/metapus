"use client"

import Link from "next/link"
import { ArrowDownToLine, ArrowUpFromLine, Users, Package } from "lucide-react"
import { Button } from "@/components/ui/button"
import type { WidgetRenderProps } from "@/types/dashboard"

const DEFAULT_ACTIONS = [
    { label: "Создать поступление", href: "/documents/goods-receipts/new", icon: "ArrowDownToLine" },
    { label: "Создать расходную накладную", href: "/documents/goods-issues/new", icon: "ArrowUpFromLine" },
    { label: "Создать контрагента", href: "/catalogs/counterparties/new", icon: "Users" },
    { label: "Создать номенклатуру", href: "/catalogs/nomenclatures/new", icon: "Package" },
]

const ICON_MAP: Record<string, React.ElementType> = {
    ArrowDownToLine,
    ArrowUpFromLine,
    Users,
    Package,
}

export default function QuickActionsRenderer({ config }: WidgetRenderProps<"quick-actions">) {
    const actions = config.actions && config.actions.length > 0 ? config.actions : DEFAULT_ACTIONS

    return (
        <div className="flex h-full flex-col">
            <div className="border-b px-4 py-3">
                <h3 className="text-sm font-semibold text-foreground">Быстрые действия</h3>
            </div>
            <div className="flex flex-col gap-1 p-2">
                {actions.map((action) => {
                    const Icon = ICON_MAP[action.icon]
                    return (
                        <Button
                            key={action.href}
                            variant="ghost"
                            size="sm"
                            className="w-full justify-start gap-2 text-sm"
                            asChild
                        >
                            <Link href={action.href}>
                                {Icon && <Icon className="h-4 w-4 text-foreground" />}
                                {action.label}
                            </Link>
                        </Button>
                    )
                })}
            </div>
        </div>
    )
}
