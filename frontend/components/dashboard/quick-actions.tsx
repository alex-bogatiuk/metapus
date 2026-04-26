import Link from "next/link"
import { cn } from "@/lib/utils"
import { Plus, FileText, Package, Users, ArrowDownToLine, ArrowUpFromLine } from "lucide-react"
import { Button } from "@/components/ui/button"

const actions = [
  {
    label: "Создать поступление",
    href: "/documents/goods-receipts/new",
    icon: ArrowDownToLine,
  },
  {
    label: "Создать расходную накладную",
    href: "/documents/goods-issues/new",
    icon: ArrowUpFromLine,
  },
  {
    label: "Создать контрагента",
    href: "/catalogs/counterparties/new",
    icon: Users,
  },
  {
    label: "Создать номенклатуру",
    href: "/catalogs/nomenclatures/new",
    icon: Package,
  },
]

export function QuickActions() {
  return (
    <div className="rounded-lg border bg-card shadow-sm">
      <div className="border-b px-4 py-3">
        <h3 className="text-sm font-semibold text-foreground">Быстрые действия</h3>
      </div>
      <div className="flex flex-col gap-1 p-2">
        {actions.map((action) => {
          const Icon = action.icon
          return (
            <Button
              key={action.href}
              variant="ghost"
              size="sm"
              className="w-full justify-start gap-2 text-sm"
              asChild
            >
              <Link href={action.href}>
                <Icon className={cn("h-4 w-4 text-foreground")} />
                {action.label}
              </Link>
            </Button>
          )
        })}
      </div>
    </div>
  )
}
