# Generic CRUD Pipeline в Metapus: CatalogService, HookRegistry и Generic Handlers

Карта показывает полный путь generic CRUD: от generic handler с типовыми параметрами до generic repository, система хуков (аналог подписок на события из 1С), и паттерн композиции сервисов.

**Ключевые точки:**
- Generic handler: `CatalogHandler[T, CreateDTO, UpdateDTO]` (1a)
- Generic service: `CatalogService[T]` с hook pipeline (2a)
- HookRegistry: event-based lifecycle hooks (3a)
- Композиция сервисов: embedding + entity-specific hooks (4a)
- Generic repository: `BaseCatalogRepo[T]` с StructToMap (5a)
- Wiring: связка handler → service → repo в router (6a)

---

## Trace 1: Generic Handler — Типизированные HTTP handlers

`CatalogHandler[T, CreateDTO, UpdateDTO]` — generic handler, который параметризуется типом сущности и DTO. Один код обслуживает все справочники.

```
CatalogHandler[T, CreateDTO, UpdateDTO] <-- catalog.go:20
│
├── Типовые параметры <-- 1a
│   ├── T         = *counterparty.Counterparty (domain entity)
│   ├── CreateDTO = dto.CreateCounterpartyRequest
│   └── UpdateDTO = dto.UpdateCounterpartyRequest
│
├── Mapper functions (инъекция через config) <-- 1b
│   ├── mapCreateDTO: func(CreateDTO) T
│   │   └── dto → domain entity (без tenantID — Database-per-Tenant)
│   ├── mapUpdateDTO: func(UpdateDTO, existing T) T
│   │   └── dto + existing → updated domain entity
│   └── mapToDTO: func(T) any
│       └── domain entity → response DTO
│
├── Стандартные методы (один код для всех справочников) <-- 1c
│   ├── List(c)   → service.List(ctx, filter) → []DTO
│   ├── Get(c)    → service.GetByID(ctx, id)  → DTO
│   ├── Create(c) → BindJSON → mapCreateDTO → service.Create → DTO
│   ├── Update(c) → BindJSON → GetByID → mapUpdateDTO → service.Update → DTO
│   ├── Delete(c) → service.Delete(ctx, id) → 204
│   ├── SetDeletionMark(c) → service.SetDeletionMark → success
│   └── GetTree(c) → service.GetTree(ctx, rootID) → []DTO
│
└── Document variant: BaseDocumentHandler[T, CreateDTO, UpdateDTO] <-- 1d
    └── Дополнительные методы:
        ├── Post(c)   → service.Post(ctx, id)
        ├── Unpost(c) → service.Unpost(ctx, id)
        └── Create: isPostImmediately → PostAndSave vs Create
```

| ID | Описание | Файл |
|----|----------|------|
| **1a** | Generic параметры. Go generics позволяют одному handler обслуживать все справочники. Тип сущности и DTO задаются при создании | `handlers/catalog.go:20` |
| **1b** | Mapper functions. Три функции маппинга инжектируются через config. Это decouples handler от конкретных типов | `handlers/catalog.go:26` |
| **1c** | Стандартные CRUD. 7 методов покрывают все операции. Каждый метод ~15 строк — binding, delegation, response | `handlers/catalog.go:56-261` |
| **1d** | Document handler. Расширяет catalog handler методами Post/Unpost и логикой `PostImmediately` | `handlers/document.go:28` |

---

## Trace 2: Generic Service — CatalogService[T] pipeline

`CatalogService[T]` реализует стандартный pipeline: Validate → Before hooks → Transaction(repo) → After hooks.

```
CatalogService[T].Create(ctx, entity) <-- service.go:89
│
├── Pipeline (единый для всех сущностей):
│
│   ┌───────────────────────────────────────────────────────────────┐
│   │  1. VALIDATE                                                   │
│   │  └── entity.Validate(ctx) <-- 2a                              │
│   │      └── T implements entity.Validatable                      │
│   │          └── Validate(ctx) error                               │
│   │      └── ошибка → normalizeValidationErr → apperror (400)     │
│   ├───────────────────────────────────────────────────────────────┤
│   │  2. BEFORE HOOKS (вне транзакции) <-- 2b                      │
│   │  └── hooks.RunBeforeCreate(ctx, entity)                       │
│   │      ├── hook1: prepareForCreate (код, уникальность)          │
│   │      ├── hook2: audit.EnrichCreatedBy (CreatedBy, UpdatedBy)  │
│   │      └── hookN: ... (любая бизнес-логика)                    │
│   │      └── ошибка в любом hook → прерывание (не создаём)       │
│   ├───────────────────────────────────────────────────────────────┤
│   │  3. TRANSACTION <-- 2c                                         │
│   │  └── txm.RunInTransaction(ctx, func(ctx) {                    │
│   │      └── repo.Create(ctx, entity)                             │
│   │  })                                                            │
│   │  └── ошибка → rollback → return error                         │
│   ├───────────────────────────────────────────────────────────────┤
│   │  4. AFTER HOOKS (вне транзакции) <-- 2d                       │
│   │  └── hooks.RunAfterCreate(ctx, entity)                        │
│   │      └── ошибки логируются, НЕ прерывают операцию            │
│   │          └── сущность УЖЕ создана в БД                       │
│   └───────────────────────────────────────────────────────────────┘
│
├── Update pipeline (аналогичный):
│   └── Validate → BeforeUpdate → tx{repo.Update} → AfterUpdate
│
└── Delete pipeline:
    └── GetByID → BeforeDelete → tx{repo.Delete} → AfterDelete <-- 2e
```

| ID | Описание | Файл |
|----|----------|------|
| **2a** | Validation. Каждая сущность реализует `Validatable` interface. Валидация — первый шаг, до хуков и транзакции | `service.go:91` |
| **2b** | Before hooks. Выполняются ДО транзакции. Типичные задачи: автонумерация, проверка уникальности, обогащение полей. Ошибка hook прерывает операцию | `service.go:96` |
| **2c** | Transaction. Собственно запись в БД. Обёрнута в `RunInTransaction`. При ошибке — автоматический rollback | `service.go:105` |
| **2d** | After hooks. Выполняются ПОСЛЕ коммита транзакции. Ошибки не прерывают операцию, но логируются через `logger.Warn`. Для событий, уведомлений, кэш-инвалидации | `service.go:116` |
| **2e** | Delete pipeline. Дополнительно: GetByID перед удалением — нужен для before-delete hooks (передаёт entity) | `service.go:177` |

---

## Trace 3: HookRegistry — Event-based lifecycle hooks

`HookRegistry[T]` — generic реестр хуков, параметризованный типом сущности. Аналог подписок на события объектов в 1С.

```
HookRegistry[T] <-- repository.go:115
│
├── Структура <-- 3a
│   └── type HookRegistry[T any] struct {
│       └── hooks map[HookEvent][]Hook[T]
│   }
│   └── type Hook[T any] func(ctx context.Context, entity T) error
│
├── Events (6 lifecycle events) <-- 3b
│   ├── BeforeCreate  — до создания (автонумерация, уникальность)
│   ├── AfterCreate   — после создания (уведомления, события)
│   ├── BeforeUpdate  — до обновления (audit, валидация бизнес-правил)
│   ├── AfterUpdate   — после обновления (инвалидация кэша)
│   ├── BeforeDelete  — до удаления (проверка связей)
│   └── AfterDelete   — после удаления (cleanup)
│
├── Регистрация хуков <-- 3c
│   ├── registry.On(BeforeCreate, hookFn)      // generic
│   ├── registry.OnBeforeCreate(hookFn)        // convenience alias
│   └── Можно регистрировать несколько хуков на одно событие
│       └── Выполняются в порядке регистрации
│
└── Выполнение хуков <-- 3d
    └── registry.Run(ctx, BeforeCreate, entity)
        └── for _, hook := range hooks[event] {
            ├── err := hook(ctx, entity) 
            ├── err != nil → return err (прерывание цепочки)
            └── err == nil → следующий hook
        }
```

| ID | Описание | Файл |
|----|----------|------|
| **3a** | Generic registry. `HookRegistry[T]` параметризован типом сущности. Hooks получают типизированную entity, не `any` | `repository.go:115` |
| **3b** | 6 lifecycle events. Before/After для Create/Update/Delete. Before hooks могут прервать операцию, After — нет | `repository.go:98` |
| **3c** | Множественные хуки. На одно событие можно зарегистрировать несколько хуков. Выполняются последовательно в порядке регистрации | `repository.go:127` |
| **3d** | Fail-fast. Первый Before-hook с ошибкой прерывает всю цепочку и операцию. After-hooks — ошибки игнорируются | `repository.go:132` |

---

## Trace 4: Композиция сервисов — Entity-specific logic

Entity-specific сервисы используют embedding `CatalogService[T]` для наследования стандартного CRUD и добавляют свои хуки.

```
Композиция: Counterparty Service <-- counterparty/service.go:16
│
├── Embedding <-- 4a
│   └── type Service struct {
│       ├── *domain.CatalogService[*Counterparty]  // embedded (CRUD)
│       ├── repo Repository                         // entity-specific repo
│       └── numerator numerator.Generator           // shared dependency
│   }
│
├── Конструктор: NewService() <-- 4b
│   ├── base := domain.NewCatalogService(config)
│   ├── svc := &Service{CatalogService: base, ...}
│   ├── base.Hooks().OnBeforeCreate(svc.prepareForCreate)  ◄── hook 1
│   └── base.Hooks().OnBeforeUpdate(svc.prepareForUpdate)  ◄── hook 2
│
├── Entity-specific hooks <-- 4c
│   ├── prepareForCreate(ctx, cp):
│   │   ├── if cp.Code == "" → numerator.GetNextNumber → cp.Code
│   │   └── if cp.INN != "" → checkINNExists → conflict if exists
│   └── prepareForUpdate(ctx, cp):
│       └── if cp.INN != "" → checkINNExists (exclude self ID)
│
├── Внешние hooks (из router.go) <-- 4d
│   └── service.Hooks().OnBeforeCreate(func(ctx, doc) {
│       └── audit.EnrichCreatedByDirect(ctx, &doc.CreatedBy, &doc.UpdatedBy)
│   })
│
└── Итоговая цепочка для Create: <-- 4e
    1. Validate(ctx)
    2. hook: prepareForCreate (код, уникальность INN)
    3. hook: audit.EnrichCreatedBy (CreatedBy, UpdatedBy)
    4. tx { repo.Create(ctx, entity) }
    5. (after hooks — если есть)
```

| ID | Описание | Файл |
|----|----------|------|
| **4a** | Embedding. Entity-specific сервис embed-ит `CatalogService[T]` — получает бесплатно все CRUD методы | `counterparty/service.go:17` |
| **4b** | Hook registration в конструкторе. Entity-specific хуки регистрируются при создании сервиса | `counterparty/service.go:43` |
| **4c** | Entity-specific logic. Автонумерация, проверка уникальности INN — бизнес-логика конкретной сущности | `counterparty/service.go:50` |
| **4d** | External hooks. Router может добавлять дополнительные хуки (audit, logging) через `service.Hooks()` | `router.go:244` |
| **4e** | Порядок хуков. Хуки выполняются в порядке регистрации: сначала entity-specific (из конструктора), потом внешние (из router) | — |

---

## Trace 5: Generic Repository — BaseCatalogRepo[T]

`BaseCatalogRepo[T]` реализует CRUD через reflection (`StructToMap`) и SQL builder (`squirrel`).

```
BaseCatalogRepo[T] <-- catalog_repo/base.go
│
├── Конфигурация <-- 5a
│   └── struct {
│       ├── tableName  string    // "cat_counterparties"
│       ├── selectCols []string  // ["id","code","name","version",...]
│       └── // getTxManager получает TxManager из context
│   }
│
├── Create(ctx, entity) <-- 5b
│   ├── StructToMap(entity) → map[string]any (через db tags)
│   ├── filter by selectCols (только известные колонки)
│   ├── squirrel.Insert(tableName).SetMap(data)
│   └── querier.Exec(ctx, sql, args...)
│
├── Update(ctx, entity) — с optimistic locking <-- 5c
│   ├── StructToMap → filter (exclude id, version)
│   ├── .Set("version", Expr("version + 1"))
│   ├── .Where(Eq{"id": id, "version": version})
│   ├── .Suffix("RETURNING version")
│   └── ErrNoRows → ConcurrentModification (409)
│
├── GetByID(ctx, id) <-- 5d
│   ├── squirrel.Select(selectCols...).From(tableName)
│   ├── .Where(Eq{"id": id})
│   └── pgxscan.Get → entity T
│
├── List(ctx, filter) — с пагинацией и фильтрацией <-- 5e
│   ├── squirrel.Select(selectCols...).From(tableName)
│   ├── WHERE: search, parentId, isFolder, deletionMark, advancedFilters
│   ├── ORDER BY, LIMIT, OFFSET
│   └── COUNT(*) для totalCount (отдельный запрос)
│
└── Delete(ctx, id) — soft delete <-- 5f
    └── UPDATE SET deletion_mark = true WHERE id = $1
```

| ID | Описание | Файл |
|----|----------|------|
| **5a** | Конфигурация. `tableName` и `selectCols` задаются при создании конкретного репозитория (e.g. `NewCounterpartyRepo()`) | `catalog_repo/base.go` |
| **5b** | Create. `StructToMap` через reflection читает `db` теги. `selectCols` фильтрует — только известные колонки попадают в INSERT | `catalog_repo/base.go:80` |
| **5c** | Update + OL. Optimistic locking через `WHERE version = $N` + `RETURNING version`. При конфликте — 409 | `catalog_repo/base.go:113` |
| **5d** | GetByID. Стандартный SELECT с `pgxscan.Get` для автоматического маппинга в struct | `catalog_repo/base.go` |
| **5e** | List. Поддерживает search, parent filter, advanced JSON filters, pagination, sorting | `catalog_repo/base.go` |
| **5f** | Soft delete. `deletion_mark = true` вместо физического удаления | `catalog_repo/base.go` |

---

## Trace 6: Wiring — Связка компонентов в Router

Все компоненты собираются в `router.go`: repo → service → hooks → handler → routes.

```
registerCatalogRoutes(rg, cfg) <-- router.go:151
│
├── Для каждого справочника (пример: counterparty): <-- 6a
│   │
│   ├── 1. Создание repo <-- 6b
│   │   └── repo := catalog_repo.NewCounterpartyRepo()
│   │       └── BaseCatalogRepo с tableName и selectCols
│   │
│   ├── 2. Создание service <-- 6c
│   │   └── service := counterparty.NewService(repo, cfg.Numerator)
│   │       ├── Внутри: domain.NewCatalogService(config)
│   │       └── Внутри: hooks.OnBeforeCreate(prepareForCreate)
│   │
│   ├── 3. Создание handler <-- 6d
│   │   └── handler := handlers.NewCounterpartyHandler(base, service)
│   │       └── CatalogHandler[*Counterparty, CreateDTO, UpdateDTO]
│   │           с mapCreateDTO, mapUpdateDTO, mapToDTO
│   │
│   └── 4. Регистрация маршрутов <-- 6e
│       └── RegisterCatalogRoutes(group, handler, "catalog:counterparty")
│           ├── GET  ""     + RequirePermission → List
│           ├── POST ""     + RequirePermission → Create
│           ├── GET  "/:id" + RequirePermission → Get
│           ├── PUT  "/:id" + RequirePermission → Update
│           └── DELETE "/:id" + RequirePermission → Delete
│
└── Для документов: аналогично + shared dependencies <-- 6f
    ├── stockRepo, stockService, postingEngine — shared
    ├── service.Hooks().OnBeforeCreate(audit.EnrichCreatedBy)
    └── RegisterDocumentRoutes(group, handler, "document:xxx")
```

| ID | Описание | Файл |
|----|----------|------|
| **6a** | Per-entity block. Каждый справочник — отдельный блок `{}` в `registerCatalogRoutes` | `router.go:158` |
| **6b** | Repo creation. Конкретный repo знает свою таблицу и колонки. Не требует TxManager — получит из context | `router.go:159` |
| **6c** | Service creation. Entity-specific сервис с embedded CatalogService. Хуки регистрируются в конструкторе | `router.go:160` |
| **6d** | Handler creation. Generic handler с mapper функциями. Один handler class для всех справочников | `router.go:161` |
| **6e** | Route registration. `RegisterCatalogRoutes` автоматически подключает permission middleware к каждому endpoint | `router.go:162` |
| **6f** | Document wiring. Документы дополнительно получают shared `postingEngine` и audit hooks из router | `router.go:223` |

---

## Trace 7: Сводная диаграмма — Полная CRUD архитектура

```
┌─────────────────────────────────────────────────────────────────────────┐
│               GENERIC CRUD PIPELINE: ОТ HANDLER ДО БД                    │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  Router (wiring):                                                        │
│  repo := NewCounterpartyRepo()                                          │
│  service := NewService(repo, numerator)    ← hooks registered here      │
│  handler := NewHandler(base, service)      ← mappers injected here      │
│  RegisterCatalogRoutes(group, handler)     ← permissions set here       │
│                                                                          │
│  ┌──────────── HANDLER (generic) ──────────────────────────┐           │
│  │ CatalogHandler[*Counterparty, CreateDTO, UpdateDTO]      │           │
│  │ ├── BindJSON(c, &req)           → CreateDTO              │           │
│  │ ├── mapCreateDTO(req)           → *Counterparty          │           │
│  │ ├── service.Create(ctx, entity) → PIPELINE               │           │
│  │ └── mapToDTO(entity)            → response JSON          │           │
│  └──────────────────────────────────────────────────────────┘           │
│                          │                                               │
│                          ▼                                               │
│  ┌──────────── SERVICE (generic + entity-specific) ────────┐           │
│  │ CatalogService[*Counterparty]                            │           │
│  │                                                           │           │
│  │ ┌─────────────── PIPELINE ──────────────────────────┐   │           │
│  │ │ 1. entity.Validate(ctx)                             │   │           │
│  │ │    └── обязательные поля, бизнес-правила            │   │           │
│  │ │                                                      │   │           │
│  │ │ 2. hooks.RunBeforeCreate(ctx, entity)               │   │           │
│  │ │    ├── [hook1] numerator → cp.Code = "CP-2024-001" │   │           │
│  │ │    ├── [hook2] checkINNExists → conflict            │   │           │
│  │ │    └── [hook3] audit.EnrichCreatedBy                │   │           │
│  │ │                                                      │   │           │
│  │ │ 3. txm.RunInTransaction {                           │   │           │
│  │ │    └── repo.Create(ctx, entity)                     │   │           │
│  │ │        └── StructToMap → squirrel.Insert → Exec     │   │           │
│  │ │    }                                                 │   │           │
│  │ │                                                      │   │           │
│  │ │ 4. hooks.RunAfterCreate(ctx, entity)                │   │           │
│  │ │    └── (уведомления, события — ошибки игнорируются) │   │           │
│  │ └──────────────────────────────────────────────────────┘   │           │
│  └──────────────────────────────────────────────────────────┘           │
│                          │                                               │
│                          ▼                                               │
│  ┌──────────── REPOSITORY (generic) ──────────────────────┐            │
│  │ BaseCatalogRepo[*Counterparty]                          │            │
│  │ ├── tableName: "cat_counterparties"                     │            │
│  │ ├── selectCols: ["id","code","name","inn",...]          │            │
│  │ └── StructToMap → squirrel → GetQuerier(ctx) → Exec    │            │
│  └──────────────────────────────────────────────────────────┘           │
│                                                                          │
│  Добавление нового справочника:                                         │
│  1. model.go   — struct + Validate()                                    │
│  2. repo.go    — NewXxxRepo() с tableName/selectCols                    │
│  3. service.go — NewService() + entity-specific hooks                   │
│  4. handler    — NewXxxHandler() + mapper functions                     │
│  5. router.go  — 4 строки wiring                                       │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```
