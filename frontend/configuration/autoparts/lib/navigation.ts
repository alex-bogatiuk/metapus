/**
 * The auto parts configuration's navigation: its catalogs and documents in
 * the sidebar and the command palette, and the route icons, labels and
 * entity keys the tabs need before metadata loads.
 */
import { PackageCheck, PackageOpen, Plus, Warehouse, Wrench } from "lucide-react"
import { registerIcons } from "@/lib/entity-icon"
import { registerRouteEntities } from "@/lib/entity-url"
import { registerNavigation } from "@/lib/navigation"
import { registerRouteLabels } from "@/lib/tab-utils"

export function registerAutopartsNavigation(): void {
  registerIcons({ entities: { item: Wrench, warehouse: Warehouse, goods_receipt: PackageOpen, goods_issue: PackageCheck } })

  registerRouteEntities({ items: "item", warehouses: "warehouse", "goods-receipts": "goods_receipt", "goods-issues": "goods_issue" })

  registerRouteLabels({
    entities: {
      items: "Номенклатура",
      item: "Номенклатура",
      warehouses: "Склады",
      warehouse: "Склады",
      "goods-receipts": "Приходные накладные",
      "goods-receipt": "Приходные накладные",
      "goods-issues": "Расходные накладные",
      "goods-issue": "Расходные накладные",
    },
  })

  registerNavigation({
    sections: [
      {
        section: "Документы",
        groups: [
          {
            label: "Склад и закупки",
            items: [{ entityKey: "goods_receipt", fallback: "Приходные накладные" }],
          },
          {
            label: "Продажи",
            items: [{ entityKey: "goods_issue", fallback: "Расходные накладные" }],
          },
        ],
      },
      {
        section: "Справочники",
        groups: [
          {
            label: "Товары и склады",
            prepend: true,
            items: [
              { entityKey: "item", fallback: "Номенклатура" },
              { entityKey: "warehouse", fallback: "Склады" },
            ],
          },
        ],
      },
    ],
    commands: [
      { id: "nav:items", label: "Номенклатура", url: "/catalogs/items", keywords: "номенклатура запчасти артикул товары items", section: "navigate" },
      { id: "nav:warehouses", label: "Склады", url: "/catalogs/warehouses", keywords: "склады склад warehouses", section: "navigate" },
      { id: "nav:goods-receipts", label: "Приходные накладные", url: "/documents/goods-receipts", keywords: "приходная накладная поступление поставщик goods receipt", section: "navigate" },
      { id: "nav:goods-issues", label: "Расходные накладные", url: "/documents/goods-issues", keywords: "расходная накладная продажа отгрузка покупатель goods issue", section: "navigate" },
      { id: "create:goods-issue", label: "Создать расходную накладную", url: "/documents/goods-issues/new", icon: Plus, keywords: "создать расходная накладная продажа отгрузка", section: "create" },
      { id: "create:goods-receipt", label: "Создать приходную накладную", url: "/documents/goods-receipts/new", icon: Plus, keywords: "создать приходная накладная поступление", section: "create" },
    ],
  })
}
