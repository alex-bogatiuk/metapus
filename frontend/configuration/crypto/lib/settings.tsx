/**
 * The crypto-processing configuration's pages in the platform settings.
 */
import { Coins, WalletCards } from "lucide-react"
import { FeeScheduleTable } from "@/configuration/crypto/components/catalogs/fee-schedule-table"
import { WalletPoolSettings } from "@/configuration/crypto/components/settings/wallet-pool-settings"
import { registerSettingsPage } from "@/lib/settings-registry"

function GlobalFeeSchedule() {
    return <FeeScheduleTable merchantId={null} />
}

export function registerCryptoSettings(): void {
    registerSettingsPage({
        id: "__fee_schedule__",
        group: "Криптопроцессинг",
        title: "Тарифы комиссий",
        summary: "Глобальные ставки по умолчанию",
        description: "Глобальные ставки по умолчанию. Применяются ко всем мерчантам, если у мерчанта нет индивидуальных настроек.",
        icon: Coins,
        component: GlobalFeeSchedule,
    })
    registerSettingsPage({
        id: "__wallet_pool__",
        group: "Криптопроцессинг",
        title: "Пул кошельков",
        summary: "Готовые адреса для инвойсов",
        description: "Система заранее создаёт адреса для инвойсов и поддерживает их нужное количество по каждой сети.",
        icon: WalletCards,
        component: WalletPoolSettings,
    })
}
