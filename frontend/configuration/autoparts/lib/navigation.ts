/**
 * The auto parts configuration's navigation: its catalogs and documents in
 * the sidebar and the command palette, and the route icons, labels and
 * entity keys the tabs need before metadata loads.
 */
import { PackageOpen, Plus, Warehouse, Wrench } from "lucide-react"
import { registerIcons } from "@/lib/entity-icon"
import { registerRouteEntities } from "@/lib/entity-url"
import { registerNavigation } from "@/lib/navigation"
import { registerRouteLabels } from "@/lib/tab-utils"

export function registerAutopartsNavigation(): void {
  registerIcons({ entities: { item: Wrench, warehouse: Warehouse, goods_receipt: PackageOpen } })

  registerRouteEntities({ items: "item", warehouses: "warehouse", "goods-receipts": "goods_receipt" })

  registerRouteLabels({
    entities: {
      items: "Номенклатура",
      item: "Номенклатура",
      warehouses: "Склады",
      warehouse: "Склады",
      "goods-receipts": "Приходные накладные",
      "goods-receipt": "Приходные накладные",
    },
  })

  registerNavigation({
    sections: [
      {
        section: "Документы",
        groups: [{ label: "Склад и закупки", items: [{ entityKey: "goods_receipt", fallback: "Приходные накладные" }] }],
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
      { id: "create:goods-receipt", label: "Создать приходную накладную", url: "/documents/goods-receipts/new", icon: Plus, keywords: "создать приходная накладная поступление", section: "create" },
    ],
  })
}
