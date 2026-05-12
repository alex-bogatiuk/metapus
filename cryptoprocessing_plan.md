# Merchant Portal — детальный план реализации

## Контекст и решения по Open Questions

**Q1 (Redirect после логина):** Принято B + нюанс. Определяющий признак — не отсутствие `isAdmin`, а наличие `portalRole > 0` в JWT. Логика:
```
isAdmin=true              → всегда ERP /
portalRole > 0 && !isAdmin → Portal /portal
нет ни того ни другого   → /login (нет доступа)
```
Это позволит в будущем выдать admin-пользователю доступ к мерчанту для тестирования, не ломая его ERP-сессию.

**Q2:** Переключатель активного мерчанта в sidebar портала. `usePortalStore` хранит `activeMerchantId`.

**Q3:** Оба: portal `/portal/` + новые крипто-виджеты в ERP Dashboard (агрегированные, для оператора).

---

## Архитектура: что откуда берётся

```
JWT (merchantIds + portalRole)
    ↓
MerchantPortal middleware → WithMerchantScope(ctx)
    ↓
PortalDashboardRepo → WHERE merchant_id = ANY($merchantIds)
    ↓
/portal/v1/* handlers → DTOs → apiFetch
    ↓
usePortalStore (activeMerchantId)
    ↓
/(portal)/layout.tsx + виджеты
```

---

## Фаза 1 — Backend: JWT + MerchantScope

### 1.1 [MODIFY] `internal/core/context/user.go`

Добавить два поля в `UserContext`:
```go
MerchantIDs []string // uuid strings; пусто = нет портального доступа
PortalRole  int      // 1=Owner 2=Manager 3=Viewer; 0=нет доступа
```

### 1.2 [MODIFY] `internal/domain/auth/jwt.go`

В `Claims` добавить:
```go
MerchantIDs []string `json:"mids,omitempty"`
PortalRole  int      `json:"prl,omitempty"`
```

В `GenerateAccessToken` добавить параметры `merchantIDs []string, portalRole int`.

В `ValidateToken` маппить новые поля в `UserContext`.

### 1.3 [MODIFY] `internal/domain/auth/service.go`

Добавить зависимость `merchantUserRepo merchant.MerchantUserRepository` в `Service`.

В `generateTokenPair` после загрузки roles/permissions:
```go
// Load merchant associations for portal JWT
merchantAssocs, _ := s.merchantUserRepo.ListByUser(ctx, user.ID)
merchantIDs := make([]string, 0, len(merchantAssocs))
var portalRole int
for _, a := range merchantAssocs {
    merchantIDs = append(merchantIDs, a.MerchantID.String())
    if int(a.Role) < portalRole || portalRole == 0 {
        portalRole = int(a.Role) // highest privilege wins
    }
}
// Pass to GenerateAccessToken
```

### 1.4 [NEW] `internal/core/context/merchant_scope.go`

```go
package context

import (
    "context"
    "metapus/internal/core/apperror"
    "metapus/internal/core/id"
)

// MerchantPortalRole — iota+1, zero = нет доступа.
type MerchantPortalRole int

const (
    PortalRoleOwner   MerchantPortalRole = iota + 1
    PortalRoleManager
    PortalRoleViewer
)

type MerchantScope struct {
    MerchantIDs []id.ID
    Role        MerchantPortalRole
}

type merchantScopeKey struct{}

func WithMerchantScope(ctx context.Context, scope MerchantScope) context.Context {
    return context.WithValue(ctx, merchantScopeKey{}, scope)
}

// MustGetMerchantScope — паника если нет scope. Аналог TxManager.
// Все portal-репозитории обязаны вызывать этот метод.
func MustGetMerchantScope(ctx context.Context) MerchantScope {
    v, ok := ctx.Value(merchantScopeKey{}).(MerchantScope)
    if !ok || len(v.MerchantIDs) == 0 {
        panic("merchant_scope not injected — missing MerchantPortal middleware")
    }
    return v
}

func GetMerchantScope(ctx context.Context) (MerchantScope, bool) {
    v, ok := ctx.Value(merchantScopeKey{}).(MerchantScope)
    return v, ok && len(v.MerchantIDs) > 0
}
```

---

## Фаза 2 — Backend: Middleware + Router

### 2.1 [NEW] `internal/infrastructure/http/v1/middleware/merchant_portal.go`

```go
// MerchantPortal извлекает merchantIds из JWT UserContext
// и инжектирует MerchantScope в context. Аналог TenantDB.
func MerchantPortal() gin.HandlerFunc {
    return func(c *gin.Context) {
        user := appctx.GetUser(c.Request.Context())
        if user == nil || len(user.MerchantIDs) == 0 {
            _ = c.Error(apperror.NewForbidden("portal access requires merchant association"))
            c.Abort()
            return
        }
        ids := make([]id.ID, 0, len(user.MerchantIDs))
        for _, s := range user.MerchantIDs {
            parsed, err := id.Parse(s)
            if err != nil { continue }
            ids = append(ids, parsed)
        }
        scope := appctx.MerchantScope{
            MerchantIDs: ids,
            Role:        appctx.MerchantPortalRole(user.PortalRole),
        }
        ctx := appctx.WithMerchantScope(c.Request.Context(), scope)
        c.Request = c.Request.WithContext(ctx)
        c.Next()
    }
}

// RequirePortalRole проверяет минимальный уровень доступа.
func RequirePortalRole(min appctx.MerchantPortalRole) gin.HandlerFunc {
    return func(c *gin.Context) {
        scope, ok := appctx.GetMerchantScope(c.Request.Context())
        if !ok || scope.Role > min { // Role: 1=Owner < 2=Manager < 3=Viewer
            _ = c.Error(apperror.NewForbidden("insufficient portal role"))
            c.Abort()
            return
        }
        c.Next()
    }
}
```

### 2.2 [NEW] `registerMerchantPortalRoutes()` в `router.go`

```
Middleware chain: TenantDB → Auth → MerchantPortal → endpoint

GET /portal/v1/merchants             — список доступных мерчантов (из scope)
GET /portal/v1/dashboard/summary     — баланс + KPI
GET /portal/v1/dashboard/currencies  — разбивка по валютам
GET /portal/v1/dashboard/chart       — динамика за период (?period=7d|30d|90d)
GET /portal/v1/invoices              — список инвойсов (пагинация)
```

Конфиг: добавить `PortalDashboardRepo` в `RouterConfig`.

---

## Фаза 3 — Backend: Repository + Handlers + DTOs

### 3.1 [NEW] `internal/infrastructure/storage/postgres/portal_repo/dashboard.go`

**Архитектурный контракт:** каждый метод начинается с `scope := appctx.MustGetMerchantScope(ctx)`.

Ключевые SQL-запросы:

```sql
-- summary (фильтр по одному activeMerchantId из query param, или ALL если не задан)
SELECT
    COUNT(*) as total,
    COUNT(*) FILTER (WHERE status='confirmed') as paid,
    COUNT(*) FILTER (WHERE status IN ('created','partially_paid')) as pending,
    COALESCE(SUM(received_amount) FILTER (WHERE status='confirmed'), 0) as total_received_minor
FROM doc_crypto_invoices
WHERE merchant_id = ANY($1)   -- $1 = scope.MerchantIDs или [activeMerchantId]
  AND NOT _deleted_at IS NOT NULL

-- currencies
SELECT t.symbol, t.network,
    COUNT(i.id) as count,
    SUM(i.received_amount) as total_minor,
    ROUND(SUM(i.received_amount)*100.0 /
        NULLIF(SUM(SUM(i.received_amount)) OVER (), 0), 2) as share_pct
FROM doc_crypto_invoices i
JOIN cat_tokens t ON t.id = i.token_id
WHERE i.merchant_id = ANY($1) AND i.status = 'confirmed'
GROUP BY t.id, t.symbol, t.network, t.decimal_places
ORDER BY total_minor DESC

-- chart (daily buckets)
SELECT DATE_TRUNC('day', created_at) as day,
    SUM(received_amount) FILTER (WHERE status='confirmed') as deposits
FROM doc_crypto_invoices
WHERE merchant_id = ANY($1)
  AND created_at >= NOW() - $2::INTERVAL
GROUP BY day ORDER BY day
```

### 3.2 [NEW] `internal/infrastructure/http/v1/dto/portal_api.go`

```go
type PortalSummaryResponse struct {
    TotalInvoices    int    `json:"totalInvoices"`
    PaidInvoices     int    `json:"paidInvoices"`
    PendingInvoices  int    `json:"pendingInvoices"`
    TotalMinorUnits  string `json:"totalMinorUnits"`   // строка, точность
    Change24hPct     string `json:"change24hPct"`      // "+2.34" / "-1.22"
}

type PortalCurrencyItem struct {
    Symbol      string `json:"symbol"`
    Network     string `json:"network"`
    Count       int    `json:"count"`
    TotalMinor  string `json:"totalMinor"`
    SharePct    string `json:"sharePct"`    // "41.27"
    DecimalPlaces int  `json:"decimalPlaces"`
}

type PortalChartPoint struct {
    Day      string `json:"day"`       // "2026-05-01"
    Deposits string `json:"deposits"`  // minor units
}

type PortalMerchantItem struct {
    ID   string `json:"id"`
    Name string `json:"name"`
    Code string `json:"code"`
}
```

### 3.3 [NEW] `internal/infrastructure/http/v1/handlers/portal_handler.go`

```go
type PortalHandler struct {
    repo PortalDashboardRepository
}

// GetSummary: извлекает ?merchant_id= из query (опционально),
// проверяет что он входит в scope.MerchantIDs, делает запрос.
func (h *PortalHandler) GetSummary(c *gin.Context)    {}
func (h *PortalHandler) GetCurrencies(c *gin.Context) {}
func (h *PortalHandler) GetChart(c *gin.Context)      {}
func (h *PortalHandler) ListMerchants(c *gin.Context) {}
```

---

## Фаза 4 — Frontend: Auth flow + Types

### 4.1 [MODIFY] `types/auth.ts`

```ts
export interface AuthUserResponse {
  // ... существующие поля ...
  merchantIds?: string[]  // из JWT mids claim
  portalRole?: number     // 1=Owner 2=Manager 3=Viewer
}
```

### 4.2 [NEW] `types/portal-api.ts`

TypeScript-интерфейсы === Go DTOs (точное соответствие имён):
```ts
export interface PortalSummaryResponse {
  totalInvoices: number
  paidInvoices: number
  pendingInvoices: number
  totalMinorUnits: string
  change24hPct: string
}
export interface PortalCurrencyItem { symbol, network, count, totalMinor, sharePct, decimalPlaces }
export interface PortalChartPoint   { day: string; deposits: string }
export interface PortalMerchantItem { id: string; name: string; code: string }
```

### 4.3 [NEW] `stores/usePortalStore.ts`

```ts
interface PortalState {
  activeMerchantId: string | null
  // синхронизируется из useAuthStore().user.merchantIds
  setActiveMerchant: (id: string) => void
}
```

### 4.4 [MODIFY] Login redirect: компонент логина или `(main)/layout.tsx`

Текущий `(main)/layout.tsx` редиректит на `/login` если не аутентифицирован. Добавить:
```ts
// Если аутентифицирован, но нет ERP-доступа → портал
if (hydrated && isAuthenticated) {
  const { user } = useAuthStore()
  const hasErpAccess = user?.isAdmin || (user?.roles?.length ?? 0) > 0
  const hasPortalAccess = (user?.merchantIds?.length ?? 0) > 0
  if (!hasErpAccess && hasPortalAccess) {
    router.replace('/portal')
  }
}
```

Новый `(portal)/layout.tsx` — зеркальная защита (нет portalRole → `/login`).

---

## Фаза 5 — Frontend: Portal Layout

### 5.1 Структура маршрутов (новые файлы)

```
frontend/app/
  (portal)/
    layout.tsx          ← auth guard + PortalShell
    page.tsx            ← /portal (dashboard)
    invoices/
      page.tsx          ← /portal/invoices
```

### 5.2 [NEW] `app/(portal)/layout.tsx`

Аналог `(main)/layout.tsx` — guard + `<PortalShell>`:
- Проверяет `user.merchantIds?.length > 0`, иначе → `/login`
- Оборачивает в `<ThemeProvider><PortalShell>`

### 5.3 [NEW] `components/portal/portal-shell.tsx`

```
┌─────────────────────────────────────────────────────┐
│ [PortalSidebar]  │  [Header: MerchantSwitcher]      │
│                  │                                   │
│ ● Панель         │  {children}                       │
│ ● Транзакции     │                                   │
│ ● Настройки      │                                   │
│                  │                                   │
│ [avatar + email] │                                   │
└─────────────────────────────────────────────────────┘
```

### 5.4 [NEW] `components/portal/merchant-switcher.tsx`

Combobox (shadcn `<Popover>` + `<Command>`) в header портала.
- Список мерчантов из `GET /portal/v1/merchants`
- При выборе: `usePortalStore.setActiveMerchant(id)` + обновить все виджеты

---

## Фаза 6 — Frontend: Portal Dashboard Widgets

Все виджеты — **self-contained карточки** с `useSWR`/`useEffect` внутри, пропсы: `merchantId: string`.

### 6.1 [NEW] `components/portal/balance-summary-card.tsx`

```
┌──────────────────────────────────────┐
│ Баланс             ∆ 24h: -2.22% ↓  │
│                                      │
│         [RadialBarChart]             │
│       ₽112,996,556.93               │
│       Всего подтверждено             │
│                                      │
│ Инвойсов: 1,234  В ожидании: 56     │
└──────────────────────────────────────┘
```
Использует: `shadcn Card` + `recharts RadialBarChart` (уже в проекте).

### 6.2 [NEW] `components/portal/currency-breakdown-card.tsx`

```
┌──────────────────────────────────────────────┐
│ Валюты (N)                            [⚙ ]   │
├──────────┬────────────────┬──────────────────┤
│ ● USDT   │ ████░ 41.27%   │ 828,830.39       │
│ ● BNB    │ ██░░░ 13.80%   │ 322.5239         │
│ ● BTC    │ █░░░░ 11.03%   │ 2.10142428       │
└──────────┴────────────────┴──────────────────┘
```
Использует: `shadcn Table` + `shadcn Progress` для долей.

### 6.3 [NEW] `components/portal/volume-chart-card.tsx`

```
  ● Депозиты  ○ Ожидают
  [Area Chart — recharts, период: 7д/30д/90д]
```
Период — `useState`, пересчёт запроса.

### 6.4 [NEW] `components/portal/recent-invoices-card.tsx`

```
Последние транзакции
[Таб: Все | Подтверждено | В ожидании | Истекло]
ID | Сеть | Сумма | Статус | Дата
```
Статусы — цветовые бейджи через `shadcn Badge` + `status-colors.ts`.

### 6.5 [NEW] `app/(portal)/page.tsx`

```tsx
export default function PortalDashboard() {
  const merchantId = usePortalStore(s => s.activeMerchantId)
  return (
    <div className="grid grid-cols-12 gap-4 p-6">
      <BalanceSummaryCard   merchantId={merchantId} className="col-span-4" />
      <CurrencyBreakdownCard merchantId={merchantId} className="col-span-8" />
      <VolumeChartCard      merchantId={merchantId} className="col-span-12" />
      <RecentInvoicesCard   merchantId={merchantId} className="col-span-12" />
    </div>
  )
}
```

---

## Фаза 7 — ERP Dashboard: новые крипто-виджеты (Q3=B)

Новые типы виджетов в `lib/widget-registry.ts` для оператора (агрегированные, все мерчанты):

| Тип виджета | Рендерер | Данные |
|---|---|---|
| `crypto-volume-overview` | [NEW] `crypto-overview-renderer.tsx` | `/api/v1/admin/stats/crypto/summary` |
| `crypto-top-currencies` | [NEW] `crypto-currencies-renderer.tsx` | `/api/v1/admin/stats/crypto/currencies` |
| `merchant-activity` | [NEW] `merchant-activity-renderer.tsx` | `/api/v1/admin/stats/merchants/activity` |

Для этих виджетов нужны 3 агрегирующих endpoint'а на backend (без MerchantScope, для admin):
```
GET /api/v1/admin/stats/crypto/summary     — суммарный объём по всем мерчантам
GET /api/v1/admin/stats/crypto/currencies  — топ валют агрегированно
GET /api/v1/admin/stats/merchants/activity — активность мерчантов
```

---

## Порядок реализации (строгий)

```
1. UserContext + JWT Claims (1.1–1.2)         — backend
2. auth.Service + MerchantUserRepo DI (1.3)   — backend
3. MerchantScope context (1.4)                — backend
4. MerchantPortal middleware (2.1)            — backend
5. registerMerchantPortalRoutes (2.2)         — backend
6. PortalDashboardRepo SQL (3.1)              — backend
7. Portal DTOs + Handlers (3.2–3.3)           — backend
8. go build + тест JWT с новыми claims       — verify
───────────────────────────────────────────────────────
9. types/auth.ts + types/portal-api.ts (4.1–4.2) — frontend
10. usePortalStore (4.3)                      — frontend
11. Login redirect logic (4.4)                — frontend
12. (portal)/layout.tsx + PortalShell (5.1–5.3) — frontend
13. MerchantSwitcher (5.4)                    — frontend
14. Portal виджеты (6.1–6.4)                 — frontend
15. Portal /portal/page.tsx (6.5)             — frontend
16. tsc --noEmit + E2E проверка              — verify
───────────────────────────────────────────────────────
17. ERP Dashboard виджеты (Фаза 7)           — frontend + backend
```

---

## Verification Plan

```bash
# Backend
go build ./...
# Unit: TestGenerateTokenPair_WithMerchantIDs
# Integration: GET /portal/v1/dashboard/summary без merchantIds в JWT → 403
#              GET /portal/v1/dashboard/summary с чужим merchant_id → 403
#              GET /portal/v1/dashboard/summary с правильным scope → 200

# Frontend
cd frontend && npx tsc --noEmit && npm run lint

# Manual E2E
# 1. Создать пользователя без ролей, привязать к мерчанту
# 2. Логин → должен попасть на /portal (не на /)
# 3. Виджеты показывают данные только своего мерчанта
# 4. Пользователь-оператор → / без изменений
# 5. GET /portal/v1/... с токеном оператора → 403
```

---

## Trade-offs

| Решение | Альтернатива | Почему выбрано |
|---|---|---|
| `merchantIds` в JWT | Lookup в БД на каждый запрос | Нет N+1; TTL токена = время жизни прав |
| Отдельный `/(portal)/` layout | Вкладка в `/(main)/` | Изоляция меню, middleware, guards |
| `MustGetMerchantScope` паника | Возврат ошибки | Аналог TxManager — ошибка конфигурации, не runtime |
| `WHERE merchant_id = ANY($1)` | PostgreSQL RLS | Проще аудитировать, тестировать, дебажить |
| `portalRole` = минимальная роль по всем мерчантам | Per-merchant role | MVP; per-merchant — расширение в фазе 2 |
