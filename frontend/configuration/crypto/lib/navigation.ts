/**
 * The crypto-processing configuration's navigation: sidebar groups, system
 * links, command palette entries, and the route icons, labels and entity keys
 * the tabs need before metadata loads.
 */
import {
  ArrowDownToLine,
  ArrowLeftRight,
  ArrowUpFromLine,
  ArrowUpRight,
  Blocks,
  Coins,
  CreditCard,
  Cpu,
  FileQuestion,
  Fuel,
  HandCoins,
  KeyRound,
  Landmark,
  LayoutDashboard,
  Link2,
  Merge,
  Network,
  PiggyBank,
  Plus,
  QrCode,
  Receipt,
  Scale,
  Settings,
  Store,
  Vault,
  Wallet,
  Webhook,
  Zap,
} from "lucide-react"
import { registerIcons } from "@/lib/entity-icon"
import { registerRouteEntities } from "@/lib/entity-url"
import { registerNavigation } from "@/lib/navigation"
import { registerRouteLabels } from "@/lib/tab-utils"

export function registerCryptoNavigation(): void {
  registerIcons({
    entities: {
      merchant: Store,
      token: Coins,
      blockchain_network: Blocks,
      wallet: Wallet,
      crypto_provider_account: Landmark,
      crypto_invoice: Receipt,
      crypto_payment: CreditCard,
      crypto_custody_receipt: ArrowDownToLine,
      unidentified_receipts: FileQuestion,
      crypto_merchant_credit: HandCoins,
      crypto_platform_funding: PiggyBank,
      wallet_fundings: Fuel,
      withdrawal_request: ArrowUpFromLine,
      crypto_withdrawal: ArrowUpRight,
      treasury_withdrawal: Vault,
      crypto_sweep: Merge,
      crypto_resource_rental: Zap,
      crypto_network_operation: Cpu,
      crypto_provider_balance_operation: ArrowLeftRight,
      crypto_adjustment: Scale,
    },
    pages: {
      "/crypto/unidentified-receipts": FileQuestion,
      "/crypto/wallet-fundings": Fuel,
      "/documents/crypto-adjustments": Scale,
      "/documents/treasury-withdrawals": Vault,
      "/documents/withdrawal-requests": ArrowUpFromLine,
      "/admin/crypto-networks": Network,
      "/admin/merchants": Store,
      "/portal/invoices": Receipt,
      "/portal/deposit-addresses": QrCode,
      "/portal/withdrawals": ArrowUpFromLine,
      "/portal/payment-links": Link2,
      "/portal/developers/keys": KeyRound,
      "/portal/developers/webhooks": Webhook,
      "/portal/settings": Settings,
      "/portal": LayoutDashboard,
    },
  })

  registerRouteEntities({
    "blockchain-networks": "blockchain_network",
    tokens: "token",
    merchants: "merchant",
    wallets: "wallet",
    "crypto-invoices": "crypto_invoice",
    "crypto-payments": "crypto_payment",
    "crypto-withdrawals": "crypto_withdrawal",
    "withdrawal-requests": "withdrawal_request",
    "treasury-withdrawals": "treasury_withdrawal",
    "crypto-sweeps": "crypto_sweep",
    "crypto-resource-rentals": "crypto_resource_rental",
    "crypto-network-operations": "crypto_network_operation",
    "crypto-provider-accounts": "crypto_provider_account",
    "crypto-provider-balance-operations": "crypto_provider_balance_operation",
  })

  registerRouteLabels({
    sections: { crypto: "Крипто" },
    entities: {
      "crypto-sweeps": "Крипто-свипы",
      "crypto-sweep": "Крипто-свипы",
      "crypto-invoices": "Крипто-инвойсы",
      "crypto-invoice": "Крипто-инвойсы",
      "crypto-payments": "Крипто-платежи",
      "crypto-payment": "Крипто-платежи",
      "crypto-custody-receipt": "Поступления на кошельки",
      "crypto-merchant-credit": "Ручные зачисления мерчантам",
      "crypto-platform-funding": "Пополнения платформы",
      "wallet-fundings": "Пополнения кошельков",
      "crypto-withdrawals": "Крипто-выводы",
      "crypto-withdrawal": "Крипто-выводы",
      "withdrawal-requests": "Заявки на вывод",
      "withdrawal-request": "Заявки на вывод",
      "treasury-withdrawals": "Казначейские выводы",
      "treasury-withdrawal": "Казначейские выводы",
      "crypto-resource-rentals": "Аренды TRON-ресурсов",
      "crypto-resource-rental": "Аренды TRON-ресурсов",
      "crypto-network-operations": "Сетевые криптооперации",
      "crypto-network-operation": "Сетевые криптооперации",
      "crypto-provider-balance-operations": "Баланс провайдеров",
      "crypto-provider-balance-operation": "Баланс провайдеров",
      "crypto-provider-accounts": "Аккаунты провайдеров",
      "crypto-provider-account": "Аккаунты провайдеров",
      merchants: "Мерчанты",
      merchant: "Мерчанты",
      wallets: "Кошельки",
      wallet: "Кошельки",
      tokens: "Токены",
      token: "Токены",
      "blockchain-networks": "Блокчейн-сети",
      "blockchain-network": "Блокчейн-сети",
    },
  })

  registerNavigation({
    sections: [
      {
        section: "Документы",
        groups: [
          {
            label: "Приём платежей",
            items: [
              { entityKey: "crypto_invoice", fallback: "Крипто-инвойсы" },
              { entityKey: "crypto_payment", fallback: "Крипто-платежи" },
            ],
          },
          {
            label: "Поступления на кошельки",
            items: [
              { entityKey: "crypto_custody_receipt", fallback: "Поступления на кошельки" },
              { entityKey: "unidentified_receipts", fallback: "Невыясненные поступления", url: "/crypto/unidentified-receipts", permission: "crypto_receipt:read" },
              { entityKey: "crypto_merchant_credit", fallback: "Ручные зачисления мерчантам" },
              { entityKey: "crypto_platform_funding", fallback: "Пополнения платформы" },
              { entityKey: "wallet_fundings", fallback: "Пополнения кошельков", url: "/crypto/wallet-fundings", permission: "crypto_receipt:read" },
            ],
          },
          {
            label: "Выплаты и казна",
            items: [
              { entityKey: "withdrawal_request", fallback: "Заявки на вывод" },
              { entityKey: "crypto_withdrawal", fallback: "Крипто-выводы" },
              { entityKey: "treasury_withdrawal", fallback: "Казначейские выводы" },
              { entityKey: "crypto_sweep", fallback: "Крипто-свипы" },
            ],
          },
          {
            label: "Технические операции",
            items: [
              { entityKey: "crypto_resource_rental", fallback: "Аренды TRON-ресурсов" },
              { entityKey: "crypto_network_operation", fallback: "Сетевые криптооперации" },
              { entityKey: "crypto_provider_balance_operation", fallback: "Баланс провайдеров" },
            ],
          },
          {
            label: "Финансовый контроль",
            items: [
              { entityKey: "crypto_adjustment", fallback: "Криптокорректировки", url: "/documents/crypto-adjustments", permission: "crypto_reconciliation:read" },
            ],
          },
        ],
      },
      {
        section: "Справочники",
        groups: [
          { label: "Бизнес и контрагенты", prepend: true, items: [{ entityKey: "merchant", fallback: "Мерчанты" }] },
          {
            label: "Блокчейн и сеть",
            after: "Бизнес и контрагенты",
            items: [
              { entityKey: "blockchain_network", fallback: "Блокчейн-сети" },
              { entityKey: "token", fallback: "Токены" },
              { entityKey: "wallet", fallback: "Кошельки" },
              { entityKey: "crypto_provider_account", fallback: "Крипто-провайдеры" },
            ],
          },
          { label: "Интеграции", items: [{ entityKey: "crypto_rpc_endpoint", fallback: "Подключения RPC" }] },
        ],
      },
    ],
    systemLinks: [
      { title: "Криптосети", url: "/admin/crypto-networks", icon: Network, before: "/help", permission: "crypto_network:read" },
    ],
    commands: [
      // Navigate
      { id: "nav:crypto-invoices", label: "Крипто-инвойсы", url: "/documents/crypto-invoice", keywords: "крипто инвойс invoice платёж payment", section: "navigate" },
      { id: "nav:crypto-payments", label: "Крипто-платежи", url: "/documents/crypto-payment", keywords: "крипто платёж payment blockchain", section: "navigate" },
      { id: "nav:unidentified-receipts", label: "Невыясненные поступления", url: "/crypto/unidentified-receipts", keywords: "крипто невыясненные поступления зачисление мерчанту dust", section: "navigate", permission: "crypto_receipt:read" },
      { id: "nav:custody-receipts", label: "Поступления на кошельки", url: "/documents/crypto-custody-receipt", keywords: "крипто поступления receipt custody кошелёк", section: "navigate" },
      { id: "nav:merchant-credits", label: "Ручные зачисления мерчантам", url: "/documents/crypto-merchant-credit", keywords: "крипто ручное зачисление мерчанту credit", section: "navigate" },
      { id: "nav:platform-fundings", label: "Пополнения платформы", url: "/documents/crypto-platform-funding", keywords: "крипто пополнение платформы funding невыясненные", section: "navigate" },
      { id: "nav:wallet-fundings", label: "Пополнения кошельков", url: "/crypto/wallet-fundings", keywords: "крипто пополнение кошелька funding hot fee", section: "navigate", permission: "crypto_receipt:read" },
      { id: "nav:crypto-withdrawals", label: "Крипто-выводы", url: "/documents/crypto-withdrawal", keywords: "крипто вывод withdrawal", section: "navigate" },
      { id: "nav:withdrawal-requests", label: "Заявки на вывод", url: "/documents/withdrawal-requests", keywords: "крипто заявки вывод withdrawal requests approve одобрение", section: "navigate" },
      { id: "nav:treasury-withdrawals", label: "Казначейские выводы", url: "/documents/treasury-withdrawals", keywords: "крипто казначейские выводы treasury withdrawal reconciliation", section: "navigate" },
      { id: "nav:crypto-sweeps", label: "Крипто-свипы", url: "/documents/crypto-sweep", keywords: "крипто свип sweep консолидация", section: "navigate" },
      { id: "nav:crypto-resource-rentals", label: "Аренды TRON-ресурсов", url: "/documents/crypto-resource-rental", keywords: "аренды tron ресурсов energy bandwidth", section: "navigate" },
      { id: "nav:crypto-network-operations", label: "Сетевые криптооперации", url: "/documents/crypto-network-operation", keywords: "сетевые криптооперации blockchain транзакции", section: "navigate" },
      { id: "nav:crypto-provider-balance-operations", label: "Баланс провайдеров", url: "/documents/crypto-provider-balance-operation", keywords: "баланс провайдеров провайдеры корректировка", section: "navigate" },
      { id: "nav:blockchain-networks", label: "Блокчейн-сети", url: "/catalogs/blockchain-networks", keywords: "блокчейн сети blockchain network tron ethereum", section: "navigate" },
      { id: "nav:tokens", label: "Токены", url: "/catalogs/tokens", keywords: "токены token usdt usdc crypto", section: "navigate" },
      { id: "nav:merchants", label: "Мерчанты", url: "/catalogs/merchants", keywords: "мерчанты merchant магазин", section: "navigate" },
      { id: "nav:wallets", label: "Кошельки", url: "/catalogs/wallets", keywords: "кошельки wallet адрес address", section: "navigate" },
      { id: "nav:crypto-providers", label: "Крипто-провайдеры", url: "/catalogs/crypto-provider-accounts", keywords: "крипто провайдеры аккаунты provider accounts itrx", section: "navigate" },

      // Create (quick actions)
      { id: "create:crypto-invoice", label: "Создать крипто-инвойс", url: "/documents/crypto-invoice/new", icon: Plus, keywords: "создать новый крипто инвойс invoice", section: "create" },
      { id: "create:crypto-withdrawal", label: "Создать крипто-вывод", url: "/documents/crypto-withdrawal/new", icon: Plus, keywords: "создать новый крипто вывод withdrawal", section: "create" },
      { id: "create:merchant", label: "Создать мерчанта", url: "/catalogs/merchants/new", icon: Plus, keywords: "создать новый мерчант merchant", section: "create" },
      { id: "create:wallet", label: "Создать кошелек", url: "/catalogs/wallets/new", icon: Plus, keywords: "создать новый кошелек wallet pool", section: "create" },

      // System
      { id: "sys:crypto-networks", label: "Криптосети", url: "/admin/crypto-networks", keywords: "криптовалютные крипто сети настройки runtime rpc signer itrx circuits policy", section: "system", permission: "crypto_network:read" },
    ],
  })
}
