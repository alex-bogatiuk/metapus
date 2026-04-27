# Конвейер CRUD (Generic CRUD Pipeline)

> **TL;DR:** CRUD операции (создание, чтение, обновление, удаление) реализованы через параметризованные generic-компоненты (Handler → Service → Repository), чтобы избежать дублирования кода для каждого справочника.

> **Тип:** Concept
> **Аудитория:** Developer
> **Связанные:** [04-request-lifecycle.md](../guide/04-request-lifecycle.md)

---

## 1. Архитектурная схема

Код работает по схеме композиции: специфичная логика сущности инжектируется в универсальный pipeline через конфигурации и хуки.

```
┌── HANDLER (CatalogHandler[T, CreateDTO, UpdateDTO]) ──┐
│ ├── BindJSON(req)                                     │
│ ├── mapCreateDTO(req) → Entity (T)                    │
│ └── service.Create(ctx, Entity)                       │
└───────────────────────────────────────────────────────┘
                     │
                     ▼
┌── SERVICE (CatalogService[T]) ────────────────────────┐
│ 1. entity.Validate(ctx)       // Проверка инвариантов │
│ 2. hooks.RunBeforeCreate()    // Бизнес-хуки          │
│ 3. txm.RunInTransaction()     // Транзакция           │
│    └── repo.Create()                                  │
│ 4. hooks.RunAfterCreate()     // Пост-обработка       │
└───────────────────────────────────────────────────────┘
                     │
                     ▼
┌── REPOSITORY (BaseCatalogRepo[T]) ────────────────────┐
│ ├── tableName: "cat_counterparties"                   │
│ └── StructToMap → squirrel → pgx.Exec                 │
└───────────────────────────────────────────────────────┘
```

## 2. Generic Handler

`CatalogHandler[T, CreateDTO, UpdateDTO]` — обрабатывает HTTP-запросы.
Для подключения нового справочника разработчик не пишет роуты и хэндлеры с нуля, а вызывает фабрику.

**Что делает Handler:**
- Десериализация DTO.
- Вызов mapper-функций (например, `mapCreateDTO`), чтобы превратить DTO во внутреннюю сущность.
- Вызов метода сервиса.
- Преобразование сущности обратно в JSON через `mapToDTO`.

## 3. Generic Service и система хуков

`CatalogService[T]` оркеструет процесс. Вся кастомная бизнес-логика (проверки в БД, генерация номеров) выносится в **Хуки** (Hooks).

### Жизненный цикл `Create`
1. **`Validate(ctx)`** — вызывается метод самой сущности (Domain invariants). Не имеет доступа к базе данных.
2. **`RunBeforeCreate`** — выполняются хуки. Например: `checkINNExists` или `generateCode`. Если хук возвращает ошибку, транзакция не начинается.
3. **`RunInTransaction`** — открывается транзакция через `TxManager`.
4. Внутри транзакции вызывается **`repo.Create`**.
5. **`RunAfterCreate`** — выполняются хуки после коммита. Ошибки здесь только логируются, они не откатывают транзакцию.

## 4. Generic Repository

`BaseCatalogRepo[T]` — универсальная обёртка над `pgx` и `squirrel`.

**Особенности:**
- Использует рефлексию Go (`db` теги), чтобы динамически конвертировать структуру в Map для вставки/обновления.
- Поддерживает Optimistic Locking по умолчанию (если у сущности есть `version`).
- Поддерживает Soft Delete (удаление ставит `deletion_mark = true` вместо `DELETE`).

---

## Файловая карта
```path
internal/infrastructure/http/v1/handlers/catalog.go   — Generic Handler
internal/domain/service.go                            — Generic Catalog Service
internal/infrastructure/storage/postgres/catalog_repo/base.go — Generic Repo
```

## Связанные документы
- [04-request-lifecycle.md](../guide/04-request-lifecycle.md) — как middleware обрабатывает запрос до того, как он попадёт в CRUD pipeline.
