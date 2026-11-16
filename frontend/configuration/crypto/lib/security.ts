/**
 * The crypto-processing configuration's security profile parts: the merchant
 * RLS dimension, the merchant-operator preset and the related role hints.
 */
import { Store } from "lucide-react"
import { cryptoApi } from "@/configuration/crypto/lib/api"
import { registerAffinityRule, registerProfilePreset, registerSecurityDimension } from "@/lib/security-registry"

export function registerCryptoSecurity(): void {
  registerSecurityDimension({
    key: "merchant",
    search: async (search) => {
      const res = await cryptoApi.merchants.list({ search: search || undefined, limit: 20 })
      return (res.items ?? []).map((m) => ({ id: m.id, name: m.name, code: m.code }))
    },
  })

  registerProfilePreset({
    code: "viewer",
    fieldPolicies: [
      { entityName: "CryptoInvoice", action: "read", allowedFields: ["*", "-expected_amount", "-received_amount"] },
      { entityName: "CryptoPayment", action: "read", allowedFields: ["*", "-amount", "-fee_amount"] },
    ],
  })
  registerProfilePreset({
    code: "merchant_operator",
    name: "Оператор мерчанта",
    description: "Полный доступ к операциям, ограниченные мерчанты",
    icon: Store,
    dimensions: { merchant: [] },
    fieldPolicies: [],
    after: "manager_limited",
  })

  registerAffinityRule({
    roleHint: "merchant_operator",
    label: "Оператор мерчанта",
    match: (f) => Object.keys(f.dimensions).includes("merchant") && f.fieldPolicies.length === 0,
  })
  registerAffinityRule({
    roleHint: "compliance",
    label: "Комплаенс",
    match: (f) =>
      Object.keys(f.dimensions).includes("merchant") &&
      f.fieldPolicies.some((p) => p.allowedFields.some((a) => a.startsWith("-"))),
  })
}
