# ABAC + CEL Security Architecture — Expert Analysis & Implementation Plan

## Экспертная оценка

Задача оценена с точки зрения текущей архитектуры Metapus. Ниже — замечания, риски и рекомендации, затем — детальный поэтапный план.

---

## Замечания и рекомендации

### ✅ Что хорошо в предложенном решении

1. **ABAC вместо RBAC** — верное направление для ERP, где роли недостаточны
2. **CEL** — Google-продукт, безопасный для evaluation (нет side-effects), хорошо типизирован, есть Go-библиотека `cel-go`
3. **Fail-closed** — правильная дефолтная политика для ERP
4. **Разделение RLS и FLS** — два ортогональных измерения, которые правильно разделять

### ⚠️ Замечания и риски

#### 1. CEL для RLS-трансляции в SQL — самый рискованный момент

> [!CAUTION]
> CEL-выражения типа `doc.organization_id in user.allowed_organizations` **нельзя напрямую транслировать в SQL**. CEL работает в рантайме Go, а SQL — это другой язык. Нужен отдельный транслятор CEL → squirrel-conditions.

**Рекомендация:** На первом этапе (MVP) **не писать CEL→SQL транслятор**. Вместо этого:
- Для RLS-**фильтрации списков** (List): использовать декларативные `DataScope` правила, которые напрямую генерируют squirrel-условия (текущий `AccessScope.FilterOrgIDs` уже делает подобное, просто расширяем)
- CEL использовать только для **point-evaluation** (проверка единичной записи: CanRead, CanWrite)
- CEL→SQL транслятор — это фича фазы 2, когда правила станут по-настоящему сложными

#### 2. FLS через рефлексию — потенциальная проблема производительности

> [!IMPORTANT]
> Рефлексия в Go дорогая. Если кэшировать при старте — ОК, но нужно чётко описать механизм кэширования. Предлагаю использовать **struct tag `sec:"field_group"`** + кэшированный `reflect.Type` → `map[string]FieldMeta` (один раз при регистрации entity в metadata registry).

#### 3. FLS: сравнение "изменённых полей" на чтение/запись

Текущий подход из задачи: «если в payload пришло изменённое поле, на которое нет прав записи → 403».

**Проблема:** Нужно сравнивать payload с текущим состоянием из БД, чтобы отличить "поле пришло, но не изменилось" от "поле пришло и отличается".

**Рекомендация:** Два подхода:
- **Approach A (проще):** Запрещаем даже **присутствие** запрещённого поля в payload (JSON). Handler делает `json.Decoder` + проверяет ключи. Проще, но строже — клиент не может отправить полный объект.
- **Approach B (как в 1С):** Загружаем текущую сущность, сравниваем поля, блокируем только **фактические изменения**. Корректнее, но сложнее.

Для ERP (где фронтенд всегда отправляет полный DTO) **Approach B правильнее**.

#### 4. [UserContext](file:///c:/Users/user/go/src/metapus/internal/core/context/user.go#9-19) — нужно расширять

Текущий [UserContext](file:///c:/Users/user/go/src/metapus/internal/core/context/user.go) содержит `OrgIDs []string`, но **не содержит**:
- `AllowedCounterpartyIDs` — для RLS по контрагентам
- Скомпилированные CEL-программы
- FLS-конфигурацию

**Рекомендация:** Создать отдельную структуру `SecurityProfile`, которая загружается при логине/refresh и кладётся в context рядом с [UserContext](file:///c:/Users/user/go/src/metapus/internal/core/context/user.go#9-19):

```go
type SecurityProfile struct {
    Policies     map[string]*EntityPolicy // entity_name → policy
    CompiledCEL  map[string]cel.Program   // policy_key → pre-compiled CEL
}
```

#### 5. Scope в Repository — нужно делать через интерфейс

Текущий [BaseCatalogRepo[T]](file:///c:/Users/user/go/src/metapus/internal/infrastructure/storage/postgres/catalog_repo/base.go): метод [List(ctx, filter)](file:///c:/Users/user/go/src/metapus/internal/domain/document_service.go#386-390) не принимает Scope. Нужно **расширить [ListFilter](file:///c:/Users/user/go/src/metapus/internal/domain/repository.go#17-45)** дополнительным полем `DataScope`, а не менять сигнатуру [List](file:///c:/Users/user/go/src/metapus/internal/domain/document_service.go#386-390).

**Trade-off:** Добавление `DataScope` в [ListFilter](file:///c:/Users/user/go/src/metapus/internal/domain/repository.go#17-45) vs отдельный аргумент:
- В [ListFilter](file:///c:/Users/user/go/src/metapus/internal/domain/repository.go#17-45) — проще, не ломает интерфейс `CatalogRepository[T]`
- Отдельный аргумент ([List(ctx, filter, scope)](file:///c:/Users/user/go/src/metapus/internal/domain/document_service.go#386-390)) — явнее, но **ломает все существующие реализации**

**Рекомендация:** `DataScope` в [ListFilter](file:///c:/Users/user/go/src/metapus/internal/domain/repository.go#17-45) + compile-time guard (если `nil` и не admin → panic/error).

#### 6. Не нужен `PurchaseInvoice` — используйте `GoodsReceipt`

В кодовой базе уже есть документ `GoodsReceipt` (Приходная накладная). Тестовые сценарии лучше строить на **реально существующей** сущности, а не создавать `PurchaseInvoice` с нуля.

#### 7. Хранение политик: БД vs YAML

> [!NOTE]
> Для MVP рекомендую **YAML/Go-config**, не БД. Хранение в БД добавляет:
> - Миграции для таблиц политик
> - CRUD для управления политиками
> - Кэш-инвалидацию при изменении правил
> - Это не критично для первой фазы

---

## Фазы реализации

### Фаза 0: Подготовка (без CEL)

Расширяем существующий [AccessScope](file:///c:/Users/user/go/src/metapus/internal/core/security/scope.go#44-61) до полноценного `DataScope` без CEL. Это закрывает 80% кейсов из сценариев.

### Фаза 1: RLS — Row-Level Security

Декларативная фильтрация по `organization_id` и `counterparty_id` на уровне Repository.

### Фаза 2: FLS — Field-Level Security

Маскировка полей на чтение и защита полей на запись.

### Фаза 3: CEL Policy Engine (опционально)

Компилируемые CEL-выражения для сложных правил (зависимости между полями, условные права).

---

## Proposed Changes

---

### Фаза 0: Data Scope Infrastructure

#### [MODIFY] [user.go](file:///c:/Users/user/go/src/metapus/internal/core/context/user.go)

Расширить [UserContext](file:///c:/Users/user/go/src/metapus/internal/core/context/user.go#9-19):
- Добавить `CounterpartyIDs []string` — разрешённые контрагенты
- Добавить `SecurityProfile *SecurityProfile` — ссылка на загруженный профиль безопасности (nil = legacy-поведение без FLS/RLS)

#### [NEW] `internal/core/security/data_scope.go`

Новый тип `DataScope` — декларативное описание видимости данных:

```go
type DataScope struct {
    IsAdmin             bool
    AllowedOrgIDs       []string // пусто = без доступа (если не admin)
    AllowedCounterpartyIDs []string // пусто = все (если orgs позволяют)
    ReadOnly            bool     // нет прав на any мутации
}
```

Методы:
- `ApplyToSelect(builder squirrel.SelectBuilder, tableName string) squirrel.SelectBuilder` — добавляет WHERE условия
- `CanAccessRecord(orgID, counterpartyID string) bool` — point-check для GetByID/Update
- `CanMutate() error` — проверка на ReadOnly

#### [MODIFY] [scope.go](file:///c:/Users/user/go/src/metapus/internal/core/security/scope.go)

Интегрировать `DataScope` в AccessScope или заменить [AccessScope](file:///c:/Users/user/go/src/metapus/internal/core/security/scope.go#44-61) на `DataScope`. Сохранить обратную совместимость через `NewDataScope(ctx)`.

---

### Фаза 1: RLS Integration

#### [MODIFY] [repository.go](file:///c:/Users/user/go/src/metapus/internal/domain/repository.go)

Добавить `DataScope *security.DataScope` в [ListFilter](file:///c:/Users/user/go/src/metapus/internal/domain/repository.go#17-45):

```go
type ListFilter struct {
    // ... existing fields ...
    DataScope *security.DataScope // RLS scope (required for non-admin)
}
```

#### [MODIFY] [base.go](file:///c:/Users/user/go/src/metapus/internal/infrastructure/storage/postgres/catalog_repo/base.go)

В [buildWhereConditions](file:///c:/Users/user/go/src/metapus/internal/infrastructure/storage/postgres/catalog_repo/base.go#209-248) добавить применение `DataScope`:

```go
if f.DataScope != nil && !f.DataScope.IsAdmin {
    if len(f.DataScope.AllowedOrgIDs) > 0 {
        conditions = append(conditions, squirrel.Eq{"organization_id": f.DataScope.AllowedOrgIDs})
    }
    if len(f.DataScope.AllowedCounterpartyIDs) > 0 {
        conditions = append(conditions, squirrel.Eq{"counterparty_id": f.DataScope.AllowedCounterpartyIDs})
    }
}
```

Аналогично в `document_repo`.

#### [MODIFY] [service.go](file:///c:/Users/user/go/src/metapus/internal/domain/service.go)

В `CatalogService.List` и `CatalogService.GetByID` — достать `DataScope` из context и:
- [List](file:///c:/Users/user/go/src/metapus/internal/domain/document_service.go#386-390): прокинуть в `ListFilter.DataScope`
- [GetByID](file:///c:/Users/user/go/src/metapus/internal/domain/service.go#144-152): после получения записи проверить `scope.CanAccessRecord(entity.OrgID, entity.CounterpartyID)`, вернуть 404 если нет доступа

#### [MODIFY] [document_service.go](file:///c:/Users/user/go/src/metapus/internal/domain/document_service.go)

Аналогичные RLS-проверки в [GetByID](file:///c:/Users/user/go/src/metapus/internal/domain/service.go#144-152), [Update](file:///c:/Users/user/go/src/metapus/internal/domain/document_service.go#189-221), [Post](file:///c:/Users/user/go/src/metapus/internal/infrastructure/http/v1/route_helpers.go#30-31), [Unpost](file:///c:/Users/user/go/src/metapus/internal/domain/document_service.go#304-317), [Delete](file:///c:/Users/user/go/src/metapus/internal/domain/service.go#202-237):
1. Загрузить `DataScope` из ctx
2. Проверить `CanAccessRecord` перед мутацией
3. Для сценария 2.3 (подмена OrgID): проверить `DataScope.CanAccessRecord` **с новым значением** orgID после маппинга DTO → entity, но _до_ сохранения

#### [MODIFY] [middleware/](file:///c:/Users/user/go/src/metapus/internal/infrastructure/http/v1/middleware)

Addить middleware `DataScopeInjector`, который:
1. После [Auth](file:///c:/Users/user/go/src/metapus/internal/infrastructure/http/v1/router.go#118-138) и [UserContext](file:///c:/Users/user/go/src/metapus/internal/core/context/user.go#9-19) middleware
2. Загружает `SecurityProfile` для user (из кэша/БД)
3. Создаёт `DataScope` и кладёт в context

---

### Фаза 2: FLS — Field-Level Security

#### [NEW] `internal/core/security/field_policy.go`

```go
type FieldPolicy struct {
    EntityName string
    Action     string // "read" | "write"
    AllowedFields  []string // `["*"]` = all, `["-status", "-org_id"]` = all except
    TableParts map[string][]string // "items" → ["quantity", "price"]
}

// IsFieldAllowed checks if a specific field is allowed by this policy
func (p *FieldPolicy) IsFieldAllowed(field string) bool
```

#### [NEW] `internal/core/security/field_masker.go`

`FieldMasker` — кэшированный рефлектор:
- При регистрации entity: `RegisterEntity(entityName string, sampleValue any)` — кэширует `reflect.Type`, json-теги, field-offsets
- `MaskForRead(entity any, policy *FieldPolicy) any` — зануляет запрещённые поля
- `ValidateWrite(oldEntity, newEntity any, policy *FieldPolicy) error` — сравнивает поля, и если изменённое поле запрещено → `apperror.Forbidden("Field 'counterparty_id' is read-only")`

#### [MODIFY] Handlers (document handlers)

В [Update](file:///c:/Users/user/go/src/metapus/internal/domain/document_service.go#189-221) handler перед вызовом `service.Update`:
1. Загрузить текущую сущность из БД (`service.GetByID`)
2. Маппить DTO в новую сущность
3. Вызвать `FieldMasker.ValidateWrite(old, new, writePolicy)`
4. Если ошибка → 403

В [Get](file:///c:/Users/user/go/src/metapus/internal/infrastructure/http/v1/route_helpers.go#27-28) handler перед отправкой ответа:
1. `FieldMasker.MaskForRead(entity, readPolicy)`

---

### Фаза 3 (Future): CEL Policy Engine

> [!NOTE]
> Эта фаза не входит в MVP. Описана для планирования.

- Добавить зависимость `github.com/google/cel-go`
- Создать `PolicyEngine` с предкомпиляцией CEL-программ при старте
- Заменить декларативные `DataScope` правила на CEL-evaluation для сложных кейсов
- Реализовать CEL→SQL транслятор для subset-выражений (org IN, counterparty IN)

---

## Миграции БД

#### [NEW] Миграция: таблицы безопасности

```sql
-- Роли пользователей (расширение существующей sys_roles)
ALTER TABLE sys_users ADD COLUMN IF NOT EXISTS allowed_counterparty_ids UUID[] DEFAULT '{}';

-- Политики безопасности (для Фазы 3, пока конфигурация в Go)
CREATE TABLE IF NOT EXISTS sys_security_policies (
    id UUID PRIMARY KEY,
    role_name TEXT NOT NULL,
    entity_name TEXT NOT NULL,
    rls_read TEXT, -- CEL expression
    rls_write TEXT,
    fls_config JSONB DEFAULT '{}',
    version INT DEFAULT 1,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(role_name, entity_name)
);
```

---

## Тестовая матрица (Seed Data)

Используем **существующие** справочники (Organization, Counterparty, Contract) + существующий документ **GoodsReceipt** вместо PurchaseInvoice.

| Пользователь | Роль | Org access | Counterparty access | FLS |
|---|---|---|---|---|
| Admin | admin | * | * | * |
| Manager_Alpha | manager | Org1 (Альфа) | * | full |
| Manager_CP1 | manager | * | CP1 (Поставщик А) | full |
| Warehouse_Worker | operator | Org1 | * | items only (no header) |
| Auditor | auditor | * | * | read-only |

---

## Verification Plan

### Automated Tests

На данный момент в проекте unit-тесты минимальны (7 файлов `*_test.go`). Для этой фичи нужен **новый набор тестов**.

#### Unit-тесты (не требуют БД)

1. **`internal/core/security/data_scope_test.go`** — тест `DataScope.ApplyToSelect`
   - Проверяет, что squirrel builder получает правильные WHERE-условия для org/counterparty фильтрации
   - Команда: `go test ./internal/core/security/... -run TestDataScope -v`

2. **`internal/core/security/field_masker_test.go`** — тесты `FieldMasker`
   - `MaskForRead`: зануление запрещённых полей
   - `ValidateWrite`: обнаружение изменённых запрещённых полей, пропуск неизменённых
   - Команда: `go test ./internal/core/security/... -run TestFieldMasker -v`

3. **`internal/core/security/field_policy_test.go`** — тесты `FieldPolicy.IsFieldAllowed`
   - Wildcard `*`, исключения `-status`, table parts
   - Команда: `go test ./internal/core/security/... -run TestFieldPolicy -v`

#### Integration-тесты (требуют БД)

> [!IMPORTANT]
> Текущий проект не имеет интеграционных тестов с БД. Для RLS-сценариев нужен `testcontainers-go` или реальная тестовая БД. **Рекомендую обсудить с вами подход к тестовой инфраструктуре перед реализацией.**

Альтернативный вариант — **ручное тестирование через API** (curl/Postman).

### Manual Verification

После реализации Фаз 0–2 — тестирование через HTTP API:

1. Запустить backend: `go run ./cmd/server` (с env-переменными из workflow)
2. Создать тестовых пользователей через seed-скрипт или API
3. Выполнить сценарии 1.1–3.2 из задачи через curl, подставляя JWT-токены разных пользователей
4. Убедиться, что:
   - Списки фильтруются корректно (проверить количество документов)
   - Прямой доступ к чужим записям возвращает 404
   - ReadOnly (Auditor) блокирует мутации (403)
   - FLS блокирует изменение запрещённых полей (403)

> [!TIP]
> Рекомендую обсудить, нужен ли вам seed-скрипт для тестовых данных (SQL-миграция с тестовыми пользователями и документами), или вы предпочитаете создавать данные через API в тестах.
