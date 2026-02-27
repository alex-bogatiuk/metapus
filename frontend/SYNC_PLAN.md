# Metapus: План синхронизации Backend ↔ Frontend

> Документ описывает все расхождения между реализованным бэкэндом (Go/Gin) и фронтэндом (Next.js),
> а также пошаговый план приведения фронта в рабочее состояние с реальным API.

---

## Текущее состояние

### Backend — полностью реализован
| Группа | Эндпоинты | Путь API |
|--------|-----------|----------|
| **Auth** | login, register, refresh, logout, me, assign-role, revoke-role, list-roles, list-permissions | `/api/v1/auth/*` |
| **Catalogs** | CRUD + deletion-mark + tree для: counterparties, nomenclature, warehouses, units, currencies, organizations, vat-rates, contracts | `/api/v1/catalog/{entity}` |
| **Documents** | CRUD + post/unpost + deletion-mark для: goods-receipt, goods-issue | `/api/v1/document/{entity}` |
| **Registers** | stock: balances, movements, turnovers, availability | `/api/v1/registers/stock/*` |
| **Reports** | stock-balance, stock-turnover, document-journal | `/api/v1/reports/*` |
| **Meta** | list-entities, get-entity | `/api/v1/meta` |

### Frontend — UI-каркас с заглушками
- Страницы: dashboard, nomenclature (list/new/[id]), goods-receipts (list/new/[id]), purchases, settings, login
- Все данные — **hardcoded mock**. Реальных API-вызовов (кроме login) **нет**.
- Типы (`types/`) **не соответствуют** backend DTO.
- Пути в `api.ts` **не соответствуют** backend routes (напр. `/catalogs/nomenclature` вместо `/catalog/nomenclature`).

---

## Фаза 0: Инфраструктура (prerequisites)

### 0.1 ✅ Авторизация (DONE)
- [x] `apiFetch` отправляет `X-Tenant-ID` и `Authorization`
- [x] `next.config.mjs` — rewrite proxy `/api/*` → `localhost:8080`
- [x] `.env.local` — `NEXT_PUBLIC_TENANT_ID`
- [x] Login page работает end-to-end
- [x] Auth guards на `(main)` и `(auth)` layouts

### 0.2 Token refresh & auto-logout
- [ ] **Interceptor**: если `apiFetch` получает `401` — попытка `api.auth.refresh()`, при неудаче → `logout()` + redirect `/login`
- [ ] **Proactive refresh**: если `expiresAt` ≤ 2 мин до истечения — фоновый refresh
- [ ] Кнопка "Выйти" в sidebar/header → `api.auth.logout()` + `useAuthStore.logout()` + redirect

### 0.3 Общие типы и хелперы
- [ ] **`types/common.ts`** — переписать `PaginatedResponse` под реальный формат:
  ```ts
  interface ListResponse<T> {
    items: T[]
    totalCount: number
    limit: number
    offset: number
  }
  ```
- [ ] **`types/common.ts`** — добавить `BaseEntity`, `Attributes` (Record<string, unknown>), `SetDeletionMarkRequest`
- [ ] **`lib/api.ts`** — добавить generic helpers:
  ```ts
  function buildListParams(params: ListParams): URLSearchParams
  ```

---

## Фаза 1: Типы — привести в соответствие backend DTO

Каждый файл `types/*.ts` должен **зеркалировать** соответствующий `dto/*.go`.

### 1.1 `types/catalog.ts` — полная переделка
Текущее состояние: только `BaseCatalogItem { id, name }` и mock `NomenclatureItem`.

Нужно добавить:
- [ ] `NomenclatureResponse` (зеркало `dto/nomenclature.go:NomenclatureResponse`) — 20+ полей
- [ ] `CreateNomenclatureRequest`, `UpdateNomenclatureRequest`
- [ ] `CounterpartyResponse`, `CreateCounterpartyRequest`, `UpdateCounterpartyRequest`
- [ ] `WarehouseResponse`, `CreateWarehouseRequest`, `UpdateWarehouseRequest`
- [ ] `UnitResponse`, `CreateUnitRequest`, `UpdateUnitRequest`
- [ ] `CurrencyResponse`, `CreateCurrencyRequest`, `UpdateCurrencyRequest`
- [ ] `OrganizationResponse`, `CreateOrganizationRequest`, `UpdateOrganizationRequest`
- [ ] `VATRateResponse`, `CreateVATRateRequest`, `UpdateVATRateRequest`
- [ ] `ContractResponse`, `CreateContractRequest`, `UpdateContractRequest`
- [ ] Enum-типы: `NomenclatureType`, `CounterpartyType`, `LegalForm`, `WarehouseType`, `UnitType`, `ContractType`

### 1.2 `types/document.ts` — полная переделка
Текущее состояние: mock `GoodsReceiptDoc` с display-строками.

Нужно:
- [ ] `GoodsReceiptResponse`, `GoodsReceiptLineResponse`
- [ ] `CreateGoodsReceiptRequest`, `GoodsReceiptLineRequest`
- [ ] `UpdateGoodsReceiptRequest`
- [ ] `GoodsIssueResponse`, `GoodsIssueLineResponse`
- [ ] `CreateGoodsIssueRequest`, `GoodsIssueLineRequest`
- [ ] `UpdateGoodsIssueRequest`

### 1.3 `types/register.ts` — новый файл
- [ ] `StockBalanceResponse`, `StockMovementResponse`, `StockTurnoverResponse`

### 1.4 `types/report.ts` — новый файл
- [ ] `StockBalanceReportResponse`, `StockBalanceReportItemResponse`
- [ ] `StockTurnoverReportResponse`, `StockTurnoverReportItemResponse`
- [ ] `DocumentJournalResponse`, `DocumentJournalItemResponse`, `DocumentTypeSummaryResponse`

### 1.5 `types/settings.ts` — ревизия
- [ ] `UserRecord` — привести к реальному `dto.UserResponse` (backend auth)
- [ ] `RoleRecord` — привести к реальному `dto.RoleResponse`
- [ ] Убрать/пометить `SystemSettings` (нет эндпоинта на бэке — `/settings` не зарегистрирован)

---

## Фаза 2: API-клиент — исправить пути и типизацию

### 2.1 Исправить пути (критичные ошибки)
| Frontend `api.ts` | Backend реальный путь | Статус |
|---|---|---|
| `/catalogs/nomenclature` | `/catalog/nomenclature` | ❌ **Неверный путь** |
| `/purchases/goods-receipts` | `/document/goods-receipt` | ❌ **Неверный путь** |
| `/settings` | Не существует | ❌ **Нет эндпоинта** |
| `/users` | Не существует (есть `/auth/me`) | ❌ **Нет эндпоинта** |
| `/roles` | `/auth/roles` | ❌ **Неверный путь** |

### 2.2 Добавить недостающие catalog endpoints
- [ ] `api.counterparties` → `/catalog/counterparties` (list, get, create, update, delete, deletionMark, tree)
- [ ] `api.nomenclature` → исправить путь на `/catalog/nomenclature` + добавить update, delete, deletionMark, tree
- [ ] `api.warehouses` → `/catalog/warehouses`
- [ ] `api.units` → `/catalog/units`
- [ ] `api.currencies` → `/catalog/currencies`
- [ ] `api.organizations` → `/catalog/organizations`
- [ ] `api.vatRates` → `/catalog/vat-rates`
- [ ] `api.contracts` → `/catalog/contracts`

### 2.3 Исправить document endpoints
- [ ] `api.goodsReceipts` → исправить путь на `/document/goods-receipt` + добавить update, delete, unpost, deletionMark
- [ ] `api.goodsIssues` → `/document/goods-issue` (новый, полный CRUD + post/unpost)

### 2.4 Добавить register/report endpoints
- [ ] `api.stock` → `balances`, `movements`, `turnovers`, `availability`
- [ ] `api.reports` → `stockBalance`, `stockTurnover`, `documentJournal`

### 2.5 Исправить auth endpoints
- [ ] `api.auth.roles` → `GET /auth/roles`
- [ ] `api.auth.permissions` → `GET /auth/permissions`
- [ ] `api.auth.assignRole` → `POST /auth/assign-role`
- [ ] `api.auth.revokeRole` → `POST /auth/revoke-role`
- [ ] Удалить `api.users.*`, `api.roles.*`, `api.settings.*` (нет таких эндпоинтов)

### 2.6 Типизировать все методы
- [ ] Заменить все `unknown` и `unknown[]` на реальные типы из `types/`
- [ ] `ListResponse<T>` — generic обёртка для всех list-ответов

---

## Фаза 3: Страницы — подключить реальные данные

### 3.1 Dashboard (`(main)/page.tsx`)
- [ ] KPI-виджеты — решить источник данных (отдельный эндпоинт или агрегация существующих)
- [ ] Пока можно: показывать `api.reports.stockBalance` для "Товары", кол-во документов и т.д.
- [ ] `RecentActivity` — подключить к `api.reports.documentJournal` (последние 10 документов)
- [ ] `CurrentTasks` — определить источник (бэкенд пока не поддерживает задачи)

### 3.2 Номенклатура — list (`catalogs/nomenclature/page.tsx`)
- [ ] Заменить mock-данные на `api.nomenclature.list(params)`
- [ ] URL-driven state: `?search=&limit=50&offset=0&orderBy=name`
- [ ] DataTable — использовать реальные колонки из `NomenclatureResponse`
- [ ] Фильтрация, сортировка, пагинация через query params

### 3.3 Номенклатура — form (`catalogs/nomenclature/new/page.tsx`, `[id]/page.tsx`)
- [ ] Форма создания: Zod-схема из `CreateNomenclatureRequest`
- [ ] Форма редактирования: загрузка `api.nomenclature.get(id)`, Zod из `UpdateNomenclatureRequest`
- [ ] Справочные поля (baseUnitId, defaultVatRateId) — комбобоксы с подгрузкой из `api.units.list()`, `api.vatRates.list()`
- [ ] Optimistic locking: отправлять `version` при update

### 3.4 Поступление товаров — list (`purchases/goods-receipts/page.tsx`)
- [ ] Заменить mock на `api.goodsReceipts.list(params)`
- [ ] Колонки: номер, дата, поставщик, склад, сумма, статус (проведён/черновик)
- [ ] Кнопки: Провести / Отменить проведение
- [ ] URL-driven пагинация

### 3.5 Поступление товаров — form (`purchases/goods-receipts/new/page.tsx`, `[id]/page.tsx`)
- [ ] Табличная часть (Lines) с inline-редактированием
- [ ] Шапка: организация, поставщик, склад, валюта, дата — комбобоксы с подгрузкой
- [ ] Автоподсчёт итогов (totalAmount, totalVAT, totalQuantity)
- [ ] Кнопка "Провести" / "Провести и закрыть"
- [ ] Optimistic locking

### 3.6 Настройки (`settings/page.tsx`)
- [ ] **Вкладка "Пользователи и роли"**: подключить к `api.auth.roles`, `api.auth.permissions`
- [ ] Управление ролями: assign/revoke через `api.auth.assignRole/revokeRole`
- [ ] **Вкладки "Организация", "Учёт", "Интерфейс"**: пока нет бэкенд-эндпоинтов → оставить как UI-заглушки или убрать
- [ ] Альтернатива: Организация → подключить к `api.organizations.list/get/update`

### 3.7 Sidebar navigation
- [ ] Добавить навигацию для недостающих разделов:
  - Справочники: Контрагенты, Склады, Единицы измерения, Валюты, Организации, Ставки НДС, Договоры
  - Документы: Расход товаров (Goods Issue)
  - Отчёты: Остатки, Обороты, Журнал документов
- [ ] Каждый раздел — отдельный route в `(main)/`

---

## Фаза 4: Новые страницы (backend есть, frontend нет)

### 4.1 Справочники — нужны страницы list + form
| Справочник | Backend | Frontend | Приоритет |
|---|---|---|---|
| Контрагенты | ✅ `/catalog/counterparties` | ❌ нет | 🔴 Высокий |
| Склады | ✅ `/catalog/warehouses` | ❌ нет | 🔴 Высокий |
| Организации | ✅ `/catalog/organizations` | ❌ нет | 🔴 Высокий |
| Единицы измерения | ✅ `/catalog/units` | ❌ нет | 🟡 Средний |
| Валюты | ✅ `/catalog/currencies` | ❌ нет | 🟡 Средний |
| Ставки НДС | ✅ `/catalog/vat-rates` | ❌ нет | 🟡 Средний |
| Договоры | ✅ `/catalog/contracts` | ❌ нет | 🟡 Средний |

### 4.2 Документы
| Документ | Backend | Frontend | Приоритет |
|---|---|---|---|
| Расход товаров | ✅ `/document/goods-issue` | ❌ нет | 🔴 Высокий |

### 4.3 Отчёты
| Отчёт | Backend | Frontend | Приоритет |
|---|---|---|---|
| Остатки на складе | ✅ `/reports/stock-balance` | ❌ нет | 🔴 Высокий |
| Обороты товаров | ✅ `/reports/stock-turnover` | ❌ нет | 🟡 Средний |
| Журнал документов | ✅ `/reports/document-journal` | ❌ нет | 🟡 Средний |

### 4.4 Регистры
| Регистр | Backend | Frontend | Приоритет |
|---|---|---|---|
| Остатки (raw) | ✅ `/registers/stock/balances` | ❌ нет | 🟢 Низкий (есть отчёт) |
| Движения (raw) | ✅ `/registers/stock/movements` | ❌ нет | 🟢 Низкий |

---

## Порядок выполнения (рекомендуемый)

### Итерация 1: Базовая работоспособность
1. **Фаза 0.2** — Token refresh + logout кнопка
2. **Фаза 0.3** — Общие типы (ListResponse, BaseEntity)
3. **Фаза 1.1** (только Nomenclature) — типы для номенклатуры
4. **Фаза 2.1** — исправить путь `/catalog/nomenclature`
5. **Фаза 3.2** — Номенклатура list с реальными данными
6. **Фаза 3.3** — Номенклатура form с реальными данными

### Итерация 2: Документы
7. **Фаза 1.2** — типы для Goods Receipt
8. **Фаза 2.3** — исправить путь `/document/goods-receipt`
9. **Фаза 3.4** — Goods Receipt list
10. **Фаза 3.5** — Goods Receipt form

### Итерация 3: Ключевые справочники
11. **Фаза 1.1** (Counterparty, Warehouse, Organization) — типы
12. **Фаза 2.2** — API endpoints
13. **Фаза 4.1** — Страницы: Контрагенты, Склады, Организации

### Итерация 4: Отчёты и остальные справочники
14. **Фаза 1.3, 1.4** — типы регистров и отчётов
15. **Фаза 2.4** — API endpoints
16. **Фаза 4.3** — Страницы отчётов
17. **Фаза 4.1** (оставшиеся) — Units, Currencies, VATRates, Contracts

### Итерация 5: Goods Issue + Dashboard
18. **Фаза 4.2** — Goods Issue (list + form)
19. **Фаза 3.1** — Dashboard с реальными данными
20. **Фаза 3.6** — Settings с реальными данными

---

## Критические несоответствия (чеклист)

- [ ] ❌ `api.ts` пути не совпадают с backend routes
- [ ] ❌ Frontend types — mock-заглушки, не зеркалируют backend DTO
- [ ] ❌ Нет token refresh interceptor (сессия умрёт через 15 мин)
- [ ] ❌ Нет кнопки logout
- [ ] ❌ Нет CRUD-страниц для 7 из 8 справочников
- [ ] ❌ Нет страницы Goods Issue
- [ ] ❌ Нет страниц отчётов
- [ ] ❌ Все list-страницы используют hardcoded данные
- [ ] ❌ Все form-страницы используют hardcoded данные
- [ ] ❌ `api.settings`, `api.users`, `api.roles` — вызывают несуществующие эндпоинты
