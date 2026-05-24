# Metapus Portal — Конкурентный UX-анализ: Stripe · Checkout.com · Square

> Цель: заимствовать лучшие интерфейсные решения из индустрии традиционных платежей и адаптировать их для криптопроцессинга.

---

## Текущее состояние портала

| Раздел | Страниц | Что есть |
|--------|---------|----------|
| Дашборд | 1 | 6 карточек: balance, currencies, funnel, chart, recent invoices |
| Инвойсы | 1 | Таблица с фильтрами, сортировкой, expandable rows, CSV, фиат-эквивалент |
| Платёжные ссылки | 1 | CRUD + copy URL |
| API-ключи | 1 | CRUD с permissions, one-time reveal |
| Настройки | 1 | Webhook URL + TTL |
| **Итого** | **5** | |

### Выявлено 25 UX-пробелов

Критичных: 5 · Важных: 7 · Улучшений: 13

---

## Матрица заимствований

### 🏆 Stripe — «Calm Technology» (эталон UX)

| Паттерн Stripe | Что берём | Приоритет | Адаптация для крипто |
|----------------|-----------|-----------|---------------------|
| **Payment Detail Page** — timeline + metadata + logs + related objects | Страница `/portal/invoices/:id` с полным жизненным циклом | 🔴 | Timeline: Created → Received → Confirmed → Swept. Blockchain explorer links вместо ARN |
| **Skeleton Loading** — layout-matching серые блоки вместо спиннеров | Заменить `<Loader2>` на skeleton cards на дашборде и таблицах | 🟡 | Прямое применение |
| **Workbench (`~`)** — developer side-panel на любой странице | Developer Drawer — отображает API request/response для текущего объекта | 🟢 | Показать raw JSON инвойса, webhook payload, blockchain tx data |
| **Copy → ✓ animation** — clipboard icon морфится в checkmark | Уже частично есть (`CopyButton`), но не на всех ID | 🟢 | Прямое применение — на txHash, addresses, payment link URLs |
| **Filter chips** — active filters как dismissible pills над таблицей | Добавить chips для активных фильтров инвойсов | 🟡 | Прямое применение |
| **Keyboard shortcuts** — `?` для справки, `/` для поиска | Добавить Command Palette (`⌘K`) + shortcuts overlay | 🟡 | У нас уже есть shortcut dialog — расширить для портала |
| **Fee transparency** — каждый платёж показывает fee + net | ✅ **Уже реализовано** в expandable rows | — | — |
| **Three-bucket balance** — Available / Pending / Reserved | Переработать Balance Card: разделить на 3 bucket'а | 🟡 | Available (confirmed), Pending (unconfirmed), Reserved (sweep in progress) |
| **Inline object links** — все ID кликабельные | Customer email → ссылка, invoice ID → detail page | 🟡 | Прямое применение |
| **Test/Live mode toggle** — визуальный banner | Testnet/Mainnet toggle с amber banner для testnet | 🔴 | Критично — Shasta testnet vs mainnet разделение |
| **Pinned shortcuts** — Recent + Favorites в sidebar | Добавить «Недавние» секцию в sidebar | 🟢 | Прямое применение |
| **Monospace for IDs** — `SF Mono` для технических данных | ✅ **Уже реализовано** (`font-mono` на номерах инвойсов) | — | — |
| **Empty states with illustration + CTA** — не просто «Нет данных» | Дизайн empty states с иллюстрациями | 🟢 | «Нет инвойсов — создайте платёжную ссылку» |
| **Payout schedule + "Pay out now"** — управление выплатами | Страница Withdrawals — расписание + ручной вывод | 🔴 | Sweep scheduling, manual withdrawal request |

---

### 🏢 Checkout.com — Enterprise-Grade Features

| Паттерн Checkout.com | Что берём | Приоритет | Адаптация для крипто |
|----------------------|-----------|-----------|---------------------|
| **Side-panel detail** — клик по строке открывает Sheet, не full page | Dual-mode: Sheet для quick view, full page для deep dive | 🟡 | Sheet с основными данными, «Подробнее» → full page |
| **Filter Builder** — composable filter rows, не просто dropdowns | Переход на metadata-driven фильтры (уже есть `FilterSidebar` в ERP) | 🟢 | Расширить существующий паттерн из ERP |
| **Data Explorer** — custom charts, build-your-own dashboards | «Аналитика» раздел с настраиваемыми виджетами | 🟢 | Объём по дням/токенам/статусам, heatmap активности |
| **Sharable Boards** — команда видит одни KPI | Сохраняемые конфигурации дашборда | 🟢 | Per-merchant saved dashboards |
| **API Logs (30 days)** — request/response inspection | `Developers > Логи` — все API-запросы мерчанта | 🟡 | Включая webhook delivery attempts |
| **Webhook retry + delivery log** — статус каждой доставки | **Webhook Delivery Log** с retry | 🔴 | Timeline: sent → 500 → retry #1 → 200 OK |
| **Risk scoring 0-100** — визуальный индикатор риска | On-chain risk score на каждый инвойс | 🟢 | Mixer detection, sanctioned addresses, freshness score |
| **Batch processing via CSV** — bulk operations | Массовый экспорт/импорт для reconciliation | 🟡 | CSV upload для массовой выверки |
| **Shadow testing** — тест правил на live данных без эффекта | A/B тестирование fee schedules | 🟢 | Симуляция новых комиссий на исторических данных |
| **USDC Settlement tracking** — stablecoin settlement | ✅ **Наш core** — показать settlement flow визуально | 🟡 | Timeline: Invoice → Payment → Sweep → Merchant Balance |
| **User Activity audit log** — кто что сделал в дашборде | Audit log: created key, changed webhook, viewed sensitive data | 🟡 | Compliance requirement для crypto |

---

### 🟦 Square — Merchant-Friendly Patterns

| Паттерн Square | Что берём | Приоритет | Адаптация |
|----------------|-----------|-----------|-----------|
| **Quick actions grid** — «Создать инвойс», «Отправить ссылку» на главной | Action buttons на дашборде | 🟡 | «Создать платёжную ссылку», «Экспорт отчёта» |
| **Visual money flow** — анимированная диаграмма движения средств | Flow diagram: Customer → Invoice → Payment → Sweep → Balance | 🟢 | Sankey-like визуализация для onboarding |
| **Mobile-first reports** — адаптивные отчёты | Mobile-responsive таблицы и карточки | 🟡 | Портал должен работать на телефоне |
| **Notification center** — bell icon с badge counter | In-app notifications: failed payments, webhook errors, key expiration | 🟡 | Toast + bell icon + notification drawer |
| **Onboarding checklist** — прогресс-бар настройки аккаунта | «Начало работы» checklist для новых мерчантов | 🟢 | 1. API key ✓ 2. Webhook ✗ 3. First invoice ✗ |

---

## Приоритизированный план улучшений

### 🔴 Волна 1: Критические пробелы (1-2 недели)

#### 1.1 Invoice Detail Page `/portal/invoices/:id`
> Stripe: Payment Detail. Checkout.com: Side panel + full page.

```
┌─────────────────────────────────────────┐
│ 🟢 Подтверждён    #CI-0084    📋 Copy  │
│ 99.00 USDT (≈ $98.91)                  │
│ [Возврат]                               │
├─────────────────────────────────────────┤
│ TIMELINE                                │
│ ● 10:41 Инвойс создан                  │
│ ● 10:43 Получен платёж 99 USDT         │
│   └─ tx: abc123...def  🔗 TronScan     │
│   └─ from: TAddr...xyz  📋             │
│ ● 10:44 Подтверждён (12 confirmations)  │
│ ● 10:45 Sweep → merchant balance        │
├─────────────────────────────────────────┤
│ ФИНАНСЫ                                 │
│ Сумма:        99.00 USDT               │
│ Комиссия:      1.50 USDT (1.5%)        │
│ К зачислению: 97.50 USDT               │
├─────────────────────────────────────────┤
│ METADATA                                │
│ externalId: order-789                   │
│ customerEmail: user@example.com         │
├─────────────────────────────────────────┤
│ WEBHOOKS                                │
│ ✅ 10:41 invoice.created → 200 (42ms)  │
│ ✅ 10:44 invoice.paid    → 200 (38ms)  │
│ ❌ 10:44 invoice.confirmed → 500       │
│    └─ [Retry] Retry #1 → 200 (51ms)    │
└─────────────────────────────────────────┘
```

**Backend**: новый endpoint `GET /portal/v1/invoices/:id` с JOIN на payments + webhooks
**Frontend**: новая страница + reuse `ExpandedDetails` → полный detail view

---

#### 1.2 Webhook Delivery Log
> Stripe: per-endpoint delivery attempts + retry. Checkout.com: event log + signing.

- Модель: `sys_webhook_deliveries` (invoice_id, url, status_code, response_time_ms, attempt, created_at)
- UI: Timeline в Invoice Detail + отдельная страница `Developers > Webhooks`
- Actions: «Отправить тест», «Повторить доставку»

---

#### 1.3 Testnet/Mainnet Toggle
> Stripe: Test/Live mode с amber banner.

- Toggle в header портала (рядом с merchant switcher)
- Testnet: amber banner `⚠️ ТЕСТОВАЯ СЕТЬ — данные не настоящие`
- Фильтрация данных по `network.isTestnet` флагу
- Раздельные API-ключи (test_ / live_ prefix)

---

#### 1.4 Withdrawals/Payouts Page `/portal/withdrawals`
> Stripe: Three-bucket balance + payout schedule.

```
┌──────────────────────────────────────────┐
│  БАЛАНС МЕРЧАНТА                         │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ │
│  │ Available │ │ Pending  │ │ Reserved │ │
│  │ $4,200    │ │ $340     │ │ $0       │ │
│  │ ✅ Ready  │ │ ⏳ 2 tx  │ │ 🔒 —     │ │
│  └──────────┘ └──────────┘ └──────────┘ │
│                                          │
│  [💸 Вывести средства]                   │
│                                          │
│  ИСТОРИЯ ВЫВОДОВ                         │
│  2026-05-19  500 USDT  ✅ Completed      │
│  2026-05-15  1200 USDT ✅ Completed      │
└──────────────────────────────────────────┘
```

---

### 🟡 Волна 2: Важные улучшения (2-3 недели)

| # | Фича | Источник | Описание |
|---|------|---------|----------|
| 2.1 | **Skeleton loading** | Stripe | Заменить спиннеры на skeleton cards на дашборде |
| 2.2 | **Invoice Side Panel** (Sheet) | Checkout.com | Quick view при клике на строку, «Подробнее» → full page |
| 2.3 | **Three-bucket balance** | Stripe | Переработать BalanceCard: Available / Pending / Reserved |
| 2.4 | **Filter chips** | Stripe | Dismissible pills для активных фильтров над таблицей |
| 2.5 | **Quick actions на дашборде** | Square | Кнопки «Создать ссылку», «Экспорт», «API docs» |
| 2.6 | **Notification center** | Square | Bell icon + drawer с alerts (failed webhooks, expired invoices) |
| 2.7 | **API Logs page** | Checkout.com | `Developers > Логи` — 30-day request/response history |
| 2.8 | **Mobile responsive tables** | Square | Card-view fallback для мобильных устройств |
| 2.9 | **Fee schedule visibility** | Stripe | Мерчант видит свои текущие комиссии в Settings |
| 2.10 | **Create Invoice from UI** | Square | Форма ручного создания инвойса (не только через API) |

---

### 🟢 Волна 3: Конкурентные преимущества (3-4 недели)

| # | Фича | Источник | Описание |
|---|------|---------|----------|
| 3.1 | **Command Palette (⌘K)** | Stripe | Глобальный поиск по инвойсам, ссылкам, настройкам |
| 3.2 | **Developer Drawer (~)** | Stripe Workbench | Slide-in панель с raw JSON, API logs для текущего объекта |
| 3.3 | **Data Explorer** | Checkout.com | Настраиваемые графики: объём по дням/токенам/статусам |
| 3.4 | **On-chain Risk Score** | Checkout.com Fraud | Score 0-100 на каждый платёж (mixer, sanctions, freshness) |
| 3.5 | **Empty state illustrations** | Stripe | SVG иллюстрации + CTA вместо «Нет данных» |
| 3.6 | **Onboarding checklist** | Square | Progress bar для новых мерчантов (API key → Webhook → First invoice) |
| 3.7 | **Settlement flow visualization** | Square | Анимированная Sankey-диаграмма: Payment → Sweep → Balance |
| 3.8 | **Batch CSV operations** | Checkout.com | Массовая выверка через CSV upload |

---

### 🔵 Волна 4: Долгосрочные инвестиции

| # | Фича | Источник | Описание |
|---|------|---------|----------|
| 4.1 | **Sharable KPI Boards** | Checkout.com | Per-merchant сохраняемые аналитические доски |
| 4.2 | **Audit Log** | Checkout.com | Кто создал ключ, изменил webhook, просмотрел sensitive data |
| 4.3 | **Branding/Customization** | Stripe | Лого мерчанта, цвета checkout page, custom domain |
| 4.4 | **AI Assistant** | Checkout.com | Чатбот для FAQ по API, статусам инвойсов |
| 4.5 | **Multi-currency fiat display** | Stripe | Переключение базовой валюты (USD/EUR/RUB) |

---

## Архитектурные рекомендации

### Design System

> **Stripe принцип «Calm Technology»**: минимум цвета, максимум whitespace, монохромная палитра с одним акцентом.

| Аспект | Текущее | Рекомендация |
|--------|---------|-------------|
| Цветовая палитра | Много badge-цветов | Серые тона + 1 accent (brand) + semantic (green/amber/red) |
| Typography | Дефолты shadcn | Inter для UI, JetBrains Mono для данных. 3-4 размера |
| Loading | `<Loader2>` спиннер | Skeleton screens matching layout |
| Empty states | «Нет данных» текстом | SVG illustration + CTA button |
| Spacing | Компактно | Больше whitespace — «дышащий» дизайн |

### Shared Utilities

```
// Дедуплицировать (сейчас в 3 файлах):
formatWithDecimals() → lib/format.ts
formatDate()         → lib/format.ts
getExplorerUrl()     → lib/blockchain.ts
getNetworkColor()    → lib/blockchain.ts
```

### Information Architecture (целевая)

```
Portal Sidebar (целевая структура)
├── 🏠 Дашборд          → /portal
├── 📄 Инвойсы           → /portal/invoices
├── 💸 Выводы            → /portal/withdrawals     ← NEW
├── 🔗 Платёжные ссылки  → /portal/payment-links
├── 📊 Аналитика         → /portal/analytics        ← NEW
├── ─── Разработчикам ───
│   ├── 🔑 API-ключи     → /portal/developers/keys
│   ├── 🪝 Вебхуки       → /portal/developers/webhooks ← NEW
│   └── 📋 API-логи      → /portal/developers/logs     ← NEW
└── ⚙️ Настройки         → /portal/settings
```

---

## Open Questions

> [!IMPORTANT]
> **Какие волны берём в работу?** Рекомендую начать с 1.1 (Invoice Detail Page) — это самый высокий ROI: одна страница закрывает 3 пробела (detail view + timeline + webhook log).

> [!IMPORTANT]  
> **Testnet/Mainnet разделение** — это фундаментальный вопрос. У нас сейчас в одной базе testnet и mainnet данные? Или database-per-tenant уже разделяет?

> [!NOTE]
> **Withdrawals** — есть ли бэкенд для инициирования вывода средств мерчантом? Или это пока только ручной процесс через ERP?
