/**
 * The crypto-processing configuration's administration sections.
 */
import { Store } from "lucide-react"
import { MerchantsSection } from "@/configuration/crypto/components/settings/merchants-section"
import { registerAdminSection } from "@/lib/admin-registry"

export function registerCryptoAdmin(): void {
    registerAdminSection({
        id: "merchants",
        title: "Мерчанты",
        description: "Пользователи и доступ к мерчантам",
        icon: Store,
        component: MerchantsSection,
    })
}
