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
│ 2. Transaction / savepoint                           │
│    ├── RLS/CEL, Validate, BeforeCreate, hierarchy      │
│    ├── repo.Create()                                  │
│    └── TransactionCreate (mandatory audit/outbox)     │
│ 3. tx.OnCommit(AfterCreate)   // Пост-обработка        │
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
1. Нормализация и чистая **`Validate(ctx)`** до обращения к БД.
2. **Транзакция/savepoint**: RLS/CEL, повторная валидация, `BeforeCreate` и проверка иерархии. Ошибка откатывает операцию.
3. **`repo.Create`**, затем обязательный **`TransactionCreate`**: аудит и durable side effects сохраняются атомарно с записью.
4. **`AfterCreate`** регистрируется через `tx.OnCommit` и выполняется после коммита внешней транзакции. Ошибки и panic здесь логируются.

## 4. Generic Repository

`BaseCatalogRepo[T]` — универсальная обёртка над `pgx` и `squirrel`.

**Особенности:**
- Использует рефлексию Go (`db` теги), чтобы динамически конвертировать структуру в Map для вставки/обновления.
- Поддерживает Optimistic Locking по умолчанию (если у сущности есть `version`).
- `SetDeletionMark` меняет пометку удаления; `Delete` физически удаляет запись с отдельными проверками прав/ссылок. Это разные операции.

---

## 5. Единый lifecycle документов

`BaseHeaderDocumentService[T]` и `BaseDocumentService[T,L]` выполняют `Before*` и обязательные `Transaction*` в одной транзакции/savepoint с записью. Нумерация также входит в эту границу. `After*` получают снимок после успешного внешнего коммита; rollback отменяет callback. Ошибки `Transaction*` откатывают операцию. Внешние RPC внутри этих хуков запрещены: публикуйте durable outbox.

Граница действует для Create, Update, Delete, PostAndSave, UpdateAndRepost и пометки удаления. Проведение/отмена проведения используют lifecycle Posting Engine; пометка имеет `TransactionSetDeletionMark`/`TransactionClearDeletionMark`.

Валютное разрешение включается только при реализации `CurrencyAwareDoc`. Общий document contract не требует этого интерфейса.

## 6. Системные документы только для чтения

Документ, который создаёт и проводит только свой workflow, регистрируется с
capabilities `List/Read/ExportList/Movements` и обслуживается
`handlers.NewReadOnlyDocumentHandler` с DTO-презентером. Маршруты создания,
изменения и проведения для него не монтируются.

## Файловая карта
```path
internal/infrastructure/http/v1/handlers/catalog.go   — Generic Handler
internal/domain/service.go                            — Generic Catalog Service
internal/infrastructure/storage/postgres/catalog_repo/base.go — Generic Repo
```

## Связанные документы
- [04-request-lifecycle.md](../guide/04-request-lifecycle.md) — как middleware обрабатывает запрос до того, как он попадёт в CRUD pipeline.

## Единая регистрация

Встроенные сущности и extensions используют domain `entity.Definition` и typed registration generic. Справочники — `CatalogRegistrationFor[Model,Create,Update,Read]`; документы — `EntityRegistration[Model]` плюс явный capabilities-aware HTTP bundle. Factory registry требует эти контракты и используется сервером и HTTP codegen. Глобальный registry иерархии и отдельные списки стандартных entity routes удалены.
