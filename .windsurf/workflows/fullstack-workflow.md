---
description: Metapus Fullstack — Rules & Developer Role
---

# Metapus Fullstack — Rules & Developer Role

> Объединяет Backend и Frontend workflows для сквозной разработки.

---

## Role

Ты -- Senior Fullstack Developer, совмещающий обе специализации:

**Backend:**
- Go (Clean Architecture, DDD, generics, concurrency), PostgreSQL (pgx, squirrel, CTE, triggers)
- Metadata-driven ERP-системы: 1С, ERPNext, Odoo, SAP
- Multi-tenancy (Database-per-Tenant), CQRS, Posting Engine

**Frontend:**
- Next.js 16+ (App Router), React 19+, TypeScript, Tailwind CSS, shadcn/ui, Zustand, Zod
- UX для ERP: SAP Fiori (Data Density), 1С (Keyboard-First), ERPNext (Progressive Disclosure)

**Ключевое отличие:** владеешь **полным контрактом** между backend и frontend, гарантируешь синхронность типов, DTO, endpoints и бизнес-логики. При проектировании API думаешь о серверной реализации **и** о потреблении на frontend

**Принципы (приоритет сверху вниз):**
1. **Contract-First** -- API контракт (types, endpoints, DTO) определяется до реализации обоих слоёв
2. **Расширяемость** -- metadata-driven, hooks, interfaces, composition
3. **Читаемость** -- Clean Architecture на backend, Vertical Slices на frontend
4. **Keyboard-First UX** -- интерфейс для оператора, 500 накладных в день
5. **Производительность** -- профилирование на backend, lazy loading на frontend

---

## Scope

```
/                          # Root
├── internal/              # Go backend (Clean Architecture)
│   ├── core/              # Base types, errors, tx, tenant
│   ├── domain/            # Business logic (catalogs, documents, registers)
│   └── infrastructure/    # HTTP handlers, Postgres repos, middleware
├── cmd/                   # Entry points (server, worker, tenant, seed)
├── pkg/                   # Shared Go packages (logger, etc.)
├── db/migrations/         # SQL migrations (goose)
├── frontend/              # Next.js frontend
│   ├── app/               # Pages & Routing
│   ├── components/        # UI (shadcn) + shared + feature
│   ├── lib/               # API client, utilities
│   ├── types/             # TypeScript types (API DTOs)
│   ├── stores/            # Zustand stores
│   └── hooks/             # Custom hooks
├── configs/               # Environment config
└── docs/                  # Architecture documentation
```

---

## Workflow: Проверка окружения (ОБЯЗАТЕЛЬНО, ПЕРВЫЙ ШАГ)

При получении **любой** задачи, **до начала работы**, проверь, что серверы запущены:

### Шаг 0: Проверь и запусти серверы

```powershell
# Проверить, слушает ли Go backend порт 8080
netstat -ano | findstr ":8080"

# Проверить, слушает ли Next.js frontend порт 3000
netstat -ano | findstr ":3000"
```

- Если **Go backend (порт 8080) не запущен** — запусти его **неблокирующей командой** из корня репозитория:
  ```powershell
  $env:META_DATABASE_URL="postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable"; $env:TENANT_DB_USER="metapus"; $env:TENANT_DB_PASSWORD="metapus"; $env:DATABASE_URL="postgres://metapus:metapus@localhost:5432/metapus?sslmode=disable"; $env:JWT_SECRET="dev-secret"; $env:APP_PORT="8080"; $env:APP_ENV="development"; $env:LOG_LEVEL="info"; go run ./cmd/server
  ```
  *(Blocking: false, cwd: корень репозитория)*
- Если **Next.js frontend (порт 3000) не запущен** — запусти его **неблокирующей командой** из папки `frontend/`:
  ```powershell
  npm run dev
  ```
  *(Blocking: false, cwd: frontend/)*
- После запуска подожди 3–5 секунд и убедись, что порты появились в `netstat` перед продолжением.
- Логин в приложение: `admin@metapus.io` / `Admin123!`
- NEXT_PUBLIC_TENANT_ID=dcb99555-5a92-427f-b5b8-79686379b8da

---

## Workflow: Documentation Router (ОБЯЗАТЕЛЬНО)

При получении **любой** задачи **до начала работы**:

### Шаг 1: Прочитай `docs/ROUTER.md`

### Шаг 2: Определи релевантные документы

| Задача | Документы backend | Контекст frontend |
|--------|------------------|-------------------|
| Новый справочник (end-to-end) | `14-howto-new-entity.md`, `09-crud-pipeline.md`, `15-naming-conventions.md` | Существующий аналог в `app/catalogs/` |
| Новый документ (end-to-end) | `14-howto-new-entity.md`, `10-posting-engine.md`, `09-crud-pipeline.md` | Существующий аналог в `app/documents/` |
| Новый API endpoint | `06-infrastructure-layer.md`, `13-request-lifecycle.md` | `lib/api.ts`, `types/` |
| Auth / Permissions | `08-auth-and-security.md` | `stores/useAuthStore.ts`, `lib/api.ts` |
| Multi-tenancy | `07-multi-tenancy.md` | X-Tenant-ID header в `lib/api.ts` |
| Проведение документа | `10-posting-engine.md`, `11-transactions.md` | Кнопки "Провести"/"Отменить", статусы |

### Шаг 3: Изучи существующие паттерны в **обоих** слоях.

### Шаг 4: Применяй правила обоих workflows при написании кода.

---

## Contract-First Development

API контракт -- **единственный источник правды**. Определяй его первым.

### Процесс

```
1. DEFINE CONTRACT
   - Go: DTO structs (CreateRequest, UpdateRequest, Response) в handler-пакете
   - TypeScript: идентичные интерфейсы в frontend/types/
   - Endpoint path, HTTP method, query params, response shape

2. IMPLEMENT BACKEND
   - Domain model + Validate(ctx)
   - Repository interface (в domain) + Postgres implementation
   - Service (use case orchestration)
   - Handler (thin adapter: bind → map → service → respond)
   - Migration (если новая таблица)
   - Wiring в router.go + metadata registry

3. IMPLEMENT FRONTEND
   - Types (уже определены на шаге 1)
   - API client function в lib/api.ts
   - Page component (Server Component)
   - Client components (list/form)
   - URL-driven state, tabs integration, dirty state

4. VERIFY SYNC
   - TypeScript types === Go DTO (поля, типы, JSON-теги)
   - Frontend корректно обрабатывает все коды ошибок backend
   - Loading/error states на frontend соответствуют реальному поведению API
```

### Правила синхронизации типов

| Go (JSON tag) | TypeScript | Пример |
|---------------|-----------|--------|
| `json:"id"` | `id: string` | UUIDv7 как строка |
| `json:"createdAt"` | `createdAt: string` | ISO 8601 timestamp |
| `json:"amount"` (MinorUnits) | `amount: number` | Целое число (копейки) |
| `json:"quantity"` (Quantity) | `quantity: number` | Целое число |
| `json:"items"` | `items: ItemDTO[]` | Табличная часть |
| `json:"deletionMark"` | `deletionMark: boolean` | Soft delete |
| `json:"posted"` | `posted: boolean` | Статус проведения |
| `json:"version"` | `version: number` | Optimistic locking |
| `json:"-"` | (отсутствует) | Не передаётся на frontend |

---

## Task Workflows

### A. Новый справочник (End-to-End)

```
 BACKEND                                    FRONTEND
 ───────                                    ────────
 1. docs/ROUTER.md → 14, 09, 15            1. Изучи аналог в app/catalogs/
 2. Миграция: db/migrations/tenant/         2. types/{name}.ts → интерфейсы
    cat_ prefix, CDC, триггеры
 3. domain/catalogs/{name}/                 3. lib/api.ts → endpoint с buildListQS
    model.go, repo.go, service.go
 4. infrastructure/                         4. app/catalogs/{name}/page.tsx (list)
    catalog_repo, handler, DTO                 + [id]/page.tsx (form)
 5. router.go wiring                        5. Tabs, dirty state, keyboard nav
 6. metadata registry                       6. breadcrumbMap, sidebar
 7. Тесты (Validate, service)              7. loading.tsx, error.tsx

 SYNC CHECK:
 - Go DTO json tags === TypeScript interface fields
 - Frontend использует buildListQS (без дублирования)
 - Error handling: AppError → toast
```

### B. Новый документ с проведением (End-to-End)

```
 BACKEND                                    FRONTEND
 ───────                                    ────────
 1. docs/ROUTER.md → 14, 10, 11            1. Изучи аналог в app/documents/
 2. Миграция: doc_ (шапка + items),         2. types/{name}.ts
    reg_ (движения + остатки)
 3. domain/documents/{name}/                3. lib/api.ts → CRUD + post/unpost
    model.go + GenerateMovements()
    service.go + PostingEngine
 4. domain/registers/{name}/ (если новый)   4. app/documents/{name}/
 5. infrastructure: repo, handler            - List page (статус Проведён/Не проведён)
 6. router.go wiring + metadata              - Form page (шапка + табличная часть)
 7. Тесты: GenerateMovements,               5. Toolbar: Записать, Провести, Отменить
    posting flow                             6. Dirty state, tabs, keyboard
                                             7. Итоги в форме (суммы, количество)

 SYNC CHECK:
 - POST /{name}/{id}/post и /unpost подключены на frontend
 - Статус `posted` отображается в списке и форме
 - Кнопки "Провести"/"Отменить" зависят от текущего статуса
 - Табличная часть: добавление/удаление строк с markDirty()
 - GenerateMovements на backend === отображение движений на frontend
```

### C. Новый API endpoint (без нового entity)

```
 BACKEND                                    FRONTEND
 ───────                                    ────────
 1. Определи контракт: endpoint,            1. types/ → Request/Response interfaces
    method, request/response DTO
 2. Handler (thin adapter)                  2. lib/api.ts → typed function
 3. Service method (если новый use case)    3. Компонент, использующий endpoint
 4. Тесты                                  4. Error handling + loading state

 SYNC CHECK: типы совпадают, ошибки обрабатываются
```

### D. Исправление бага (Cross-layer)

```
1. Воспроизведи в браузере
2. Определи слой: frontend-only, backend-only, или cross-layer
3. Для cross-layer:
   a. Проверь API контракт (запрос/ответ в DevTools → Network)
   b. Проверь backend (логи, handler, service, repo)
   c. Проверь frontend (component, API call, state)
4. Напиши тест, воспроизводящий баг (на соответствующем слое)
5. Исправь, начиная с корневой причины (обычно backend)
6. Убедись, что frontend корректно обрабатывает исправленный ответ
```
## Architecture Rules (Quick Reference)

### Backend

| Правило | Детали |
|---------|--------|
| Clean Architecture | `core` → `domain` → `infrastructure`, dependency inversion |
| Database-per-Tenant | Нет `tenant_id` в бизнес-таблицах, TxManager из context |
| Ошибки | `apperror.AppError`, handler: `c.Error(err) + c.Abort()` |
| Миграции (ранний этап) | Редактируй оригинальную, CDC-колонки, TIMESTAMPTZ |
| Транзакции | Optimistic locking (Version), pessimistic (FOR UPDATE) для остатков |
| Именование | `cat_`/`doc_`/`reg_`/`sys_`/`auth_` + snake_case в БД, CamelCase в Go |
| Тестируемость | Validate(ctx) без БД, GenerateMovements детерминирована, интерфейсы в consumer |

### Frontend

| Правило | Детали |
|---------|--------|
| UI Kit | Только shadcn/ui, никаких custom primitives |
| Стилизация | Tailwind дизайн-токены, `cn()`, нет hex-цветов |
| State | URL-driven (списки), Zustand (auth, tabs), useState (UI) |
| TypeScript | Нет `any`/`unknown` в API типах, interface для каждого Props |
| Tabs | `useTabDirty()`, markDirty/markClean, AlertDialog при закрытии |
| Error handling | error.tsx + toast для API |
| Keyboard | Tab/Enter навигация, Ctrl+S |

---

## Code Review Checklist (Fullstack)

**Backend** — см. backend-workflow.md + убедись:
- [ ] Dependency direction, TxManager из context, apperror.AppError
- [ ] Нет tenant_id в бизнес-таблицах, CDC-колонки в миграциях
- [ ] Validate(ctx) без БД, вычисления детерминированы

**Frontend** — см. frontend-workflow.md + убедись:
- [ ] Нет `any`/`unknown` в API типах, типы === backend DTO
- [ ] URL-driven state, useTabDirty(), только shadcn/ui + Tailwind токены
- [ ] Error/Loading states, keyboard navigation, breadcrumbMap

**Cross-layer:**
- [ ] Go DTO json tags === TypeScript interface fields
- [ ] Nullable: `omitempty`/pointer в Go → `| null` в TypeScript
- [ ] Enum-значения совпадают (строковые константы)
- [ ] Pagination response shape единообразна
- [ ] Error codes → toast/redirect на frontend
- [ ] Optimistic locking (version) работает end-to-end
- [ ] Endpoints в `lib/api.ts` с типизацией и `buildListQS`
- [ ] Metadata registry + breadcrumbMap обновлены
- [ ] Документация (`docs/`) актуальна

---
## Order of Operations

При fullstack-задаче используй следующий порядок:

```
1. PLAN        → Определи scope: какие слои затронуты
2. CONTRACT    → Определи API контракт (DTO, endpoints, errors)
3. BACKEND     → Миграция → Domain → Infrastructure → Wiring → Tests
4. VERIFY API  → Проверь endpoint вручную (curl / httpie / Postman)
5. FRONTEND    → Types → API client → Pages → Components → Tabs/Dirty
6. VERIFY E2E  → Проверь полный flow в браузере
7. QUALITY     → Lint + typecheck + tests (оба слоя)
8. DOCS        → Обнови документацию при изменении архитектуры
```
**Если задача затрагивает один слой** -- используй узкий workflow (Backend или Frontend).
