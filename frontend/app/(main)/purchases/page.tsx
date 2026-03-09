import Link from "next/link"


const sections = [
  {
    title: "Закупки",
    color: "text-success",
    items: [
      { label: "Поставщики", href: "/catalogs/counterparties" },
      { label: "Заказы поставщикам", href: "/purchases/orders" },
      { label: "Счета на оплату (полученные)", href: "/purchases/invoices" },
      { label: "Приходные накладные", href: "/purchases/goods-receipts" },
      { label: "Возвраты поставщикам", href: "/purchases/returns" },
      { label: "Дополнительные расходы", href: "/purchases/expenses" },
    ],
  },
  {
    title: "Товары и услуги",
    color: "text-success",
    items: [
      { label: "Номенклатура", href: "/catalogs/nomenclature" },
    ],
  },
  {
    title: "Аналитика",
    color: "text-success",
    items: [{ label: "Отчеты", href: "/reports" }],
  },
  {
    title: "Сервис",
    color: "text-success",
    items: [
      { label: "Состояния заказов поставщикам", href: "#" },
      { label: "Распознавание документов", href: "#" },
      { label: "Дополнительные обработки", href: "#" },
    ],
  },
  {
    title: "Расчеты с поставщиками",
    color: "text-success",
    items: [
      { label: "Сверки взаиморасчетов", href: "#" },
    ],
  },
]

export default function PurchasesPage() {
  return (
    <div className="flex h-full flex-col">
      <div className="flex-1 overflow-auto p-6">
        <div className="grid grid-cols-1 gap-x-12 gap-y-8 md:grid-cols-2 lg:grid-cols-3">
          {sections.map((section) => (
            <div key={section.title}>
              <h2 className={`mb-3 text-sm font-semibold ${section.color}`}>
                {section.title}
              </h2>
              <div className="flex flex-col gap-1">
                {section.items.map((item) => (
                  <Link
                    key={item.label}
                    href={item.href}
                    className="rounded-md px-2 py-1.5 text-sm text-foreground transition-colors hover:bg-muted"
                  >
                    {item.label}
                  </Link>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
