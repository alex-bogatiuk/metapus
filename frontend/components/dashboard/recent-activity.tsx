import Link from "next/link"
import { Badge } from "@/components/ui/badge"

const activities = [
  {
    id: 1,
    type: "Поступление товаров",
    number: "НФ00-000002",
    date: "12.02.2026",
    counterparty: "ООО Поставщик",
    amount: "110,00",
    status: "posted",
  },
  {
    id: 2,
    type: "Поступление товаров",
    number: "НФ00-000001",
    date: "06.02.2026",
    counterparty: "ООО Тест",
    amount: "11,00",
    status: "posted",
  },
  {
    id: 3,
    type: "Реализация товаров",
    number: "РТ00-000001",
    date: "05.02.2026",
    counterparty: "ИП Покупатель",
    amount: "55,00",
    status: "draft",
  },
  {
    id: 4,
    type: "Поступление товаров",
    number: "НФ00-000003",
    date: "04.02.2026",
    counterparty: "ООО Склад-Сервис",
    amount: "250,00",
    status: "posted",
  },
]

export function RecentActivity() {
  return (
    <div className="rounded-lg border bg-card shadow-sm">
      <div className="flex items-center justify-between border-b px-4 py-3">
        <h3 className="text-sm font-semibold text-foreground">
          Последние документы
        </h3>
        <Link
          href="/purchases/goods-receipts"
          className="text-xs font-medium text-primary hover:underline"
        >
          Все документы
        </Link>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="px-4 py-2 text-left text-xs font-medium text-muted-foreground">
                Тип
              </th>
              <th className="px-4 py-2 text-left text-xs font-medium text-muted-foreground">
                Номер
              </th>
              <th className="px-4 py-2 text-left text-xs font-medium text-muted-foreground">
                Дата
              </th>
              <th className="px-4 py-2 text-left text-xs font-medium text-muted-foreground">
                Контрагент
              </th>
              <th className="px-4 py-2 text-right text-xs font-medium text-muted-foreground">
                Сумма
              </th>
              <th className="px-4 py-2 text-center text-xs font-medium text-muted-foreground">
                Статус
              </th>
            </tr>
          </thead>
          <tbody>
            {activities.map((a) => (
              <tr
                key={a.id}
                className="border-b last:border-0 hover:bg-muted/30 transition-colors cursor-pointer"
              >
                <td className="px-4 py-2.5 text-foreground">{a.type}</td>
                <td className="px-4 py-2.5 font-mono text-xs text-foreground">
                  {a.number}
                </td>
                <td className="px-4 py-2.5 text-muted-foreground">{a.date}</td>
                <td className="px-4 py-2.5 text-foreground">
                  {a.counterparty}
                </td>
                <td className="px-4 py-2.5 text-right font-mono text-foreground">
                  {a.amount}
                </td>
                <td className="px-4 py-2.5 text-center">
                  <Badge
                    variant={a.status === "posted" ? "success" : "secondary"}
                    className="text-[10px]"
                  >
                    {a.status === "posted" ? "Проведен" : "Черновик"}
                  </Badge>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
