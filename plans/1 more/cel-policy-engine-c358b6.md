# CEL Policy Engine (Phase 3)

Реализация движка бизнес-правил на базе Google CEL (`github.com/google/cel-go`), хранящихся в БД и привязанных к SecurityProfile. Покрывает все операции: CRUD, Post/Unpost, List filtering. Без фронтенда — только backend + REST API.

---

## Архитектура

```
SecurityProfile ──1:N──> PolicyRule (DB: security_policy_rules)
                              │
                              ▼
                     CEL expression (string)
                     compiled → cel.Program (cached)
                              │
                              ▼
              PolicyEngine.Evaluate(ctx, action, entity) → allow/deny
```

**Переменные CEL-среды** (доступны в выражениях):

| Переменная | Тип | Описание |
|---|---|---|
| `doc` | `map[string]dyn` | Поля документа/справочника (json-представление) |
| `user` | `map[string]dyn` | `{id, email, roles, orgIds, isAdmin}` |
| `action` | `string` | `create`, `read`, `update`, `delete`, `post`, `unpost` |
| `now` | `timestamp` | Текущее время (для правил вроде "не старше недели") |

**Пример выражения:**
```cel
action == "update" && doc.status == "draft" && doc.total_amount < 1000000 && user.roles.exists(r, r == "manager")
```

---

## Шаги реализации

### Step 1 — Миграция БД

**Файл:** `db/migrations/00034_security_policy_rules.sql`

```sql
CREATE TABLE security_policy_rules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id  UUID         NOT NULL REFERENCES security_profiles(id) ON DELETE CASCADE,
    name        VARCHAR(200) NOT NULL,
    description TEXT,
    entity_name VARCHAR(100) NOT NULL,  -- "*" = all entities, "goods_receipt", etc.
    actions     TEXT[]       NOT NULL DEFAULT '{*}', -- ["create","update"] or ["*"]
    expression  TEXT         NOT NULL,  -- CEL expression, must return bool
    effect      VARCHAR(10)  NOT NULL DEFAULT 'deny', -- 'deny' or 'allow'
    priority    INT          NOT NULL DEFAULT 0,       -- higher = evaluated first
    enabled     BOOLEAN      NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT spr_effect_check CHECK (effect IN ('deny', 'allow'))
);

CREATE INDEX idx_spr_profile_id ON security_policy_rules(profile_id);
CREATE INDEX idx_spr_entity_action ON security_policy_rules(entity_name, enabled);
```

**Семантика effect:**
- `deny` (по умолчанию) — если выражение = `true`, операция **запрещена**
- `allow` — если выражение = `true`, операция **разрешена** (для whitelist-правил)

**Порядок вычисления:** по `priority` DESC → первое сработавшее правило побеждает. Если ни одно не сработало — операция разрешена (default-allow, RLS/FLS уже отфильтровали).

---

### Step 2 — Domain Model

**Файл:** `internal/domain/security_profile/policy_rule.go`

```go
type PolicyRule struct {
    ID          id.ID
    ProfileID   id.ID
    Name        string
    Description string
    EntityName  string   // "*" or specific entity
    Actions     []string // ["create","update"] or ["*"]
    Expression  string   // raw CEL source
    Effect      string   // "deny" or "allow"
    Priority    int
    Enabled     bool
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

- `Validate()` — проверяет обязательные поля + **компилирует CEL** для валидации синтаксиса при сохранении
- `MatchesAction(action string) bool` — проверяет, подходит ли правило для данного action

**Добавить в `SecurityProfile`:**
```go
PolicyRules []*PolicyRule `db:"-" json:"policyRules,omitempty"`
```

---

### Step 3 — CEL Engine (ядро)

**Файл:** `internal/core/security/cel_engine.go`

Основной компонент — `PolicyEngine`:

```go
type PolicyEngine struct {
    env          *cel.Env           // shared CEL environment
    programCache sync.Map           // rule ID → compiled cel.Program
}

func NewPolicyEngine() (*PolicyEngine, error)

// Compile validates and compiles a CEL expression. Used at rule save time.
func (e *PolicyEngine) Compile(expression string) error

// Evaluate runs all applicable rules against the entity.
// Returns nil if allowed, apperror.Forbidden if denied.
func (e *PolicyEngine) Evaluate(ctx context.Context, rules []*PolicyRule, action string, entity any) error

// EvaluateForList filters a slice of entities, removing those denied by rules.
// Used for post-fetch filtering in List operations.
func (e *PolicyEngine) EvaluateForList(ctx context.Context, rules []*PolicyRule, entities []any) []any
```

**CEL Environment setup:**
- Объявляем переменные: `doc` (map), `user` (map), `action` (string), `now` (timestamp)
- Кастомные функции (опционально): `hasRole(role)`, `daysSince(timestamp)`
- Compiled programs кешируются по rule ID в `sync.Map`

**Entity → CEL map conversion:**
- Переиспользуем существующий `entityToMap()` из `field_masker.go` (уже есть reflection + кеш)
- `UserContext` → `map[string]any` конвертер

---

### Step 4 — Контекст: PolicyRules в request context

**Файл:** `internal/core/security/cel_context.go`

```go
func WithPolicyRules(ctx context.Context, rules []*PolicyRule) context.Context
func GetPolicyRules(ctx context.Context) []*PolicyRule
// Convenience: get rules for specific entity+action
func GetApplicableRules(ctx context.Context, entityName, action string) []*PolicyRule
```

---

### Step 5 — Repository для PolicyRules

**Файл:** `internal/infrastructure/storage/postgres/security_repo/policy_rule.go`

- CRUD: `Create`, `GetByID`, `Update`, `Delete`
- `ListByProfileID(ctx, profileID) → []*PolicyRule`
- `ListByProfileIDs(ctx, profileIDs) → map[id.ID][]*PolicyRule` — для batch-загрузки при построении контекста

**Обновить `security_repo/profile.go`:**
- `GetByUserID` дополнительно загружает `PolicyRules` для профиля (JOIN или отдельный запрос)

---

### Step 6 — Кеш + Provider

**Обновить:** `internal/domain/security_profile/provider.go`

- `CachedProfileProvider.GetUserProfile()` уже кеширует `SecurityProfile`
- Нужно убедиться, что `PolicyRules` загружаются как часть профиля и попадают в кеш
- `BuildSecurityContext()` дополнительно возвращает `[]*PolicyRule`

---

### Step 7 — Middleware: инъекция PolicyRules в контекст

**Обновить:** `internal/infrastructure/http/v1/middleware/security_context.go`

- После `BuildSecurityContext()` → также записывает `PolicyRules` в контекст через `security.WithPolicyRules()`
- Admin bypass: правила не применяются

---

### Step 8 — Интеграция в BaseDocumentService

**Обновить:** `internal/domain/document_service.go`

Добавить поле `PolicyEngine *security.PolicyEngine` в `BaseDocumentService`.

**Точки интеграции:**

| Метод | Действие |
|---|---|
| `Create` | `engine.Evaluate(ctx, rules, "create", doc)` после валидации, перед транзакцией |
| `GetByID` | `engine.Evaluate(ctx, rules, "read", doc)` после fetch + RLS check |
| `Update` | `engine.Evaluate(ctx, rules, "update", doc)` после FLS check, перед `CanModify` |
| `Delete` | `engine.Evaluate(ctx, rules, "delete", doc)` после fetch + RLS check |
| `Post` | `engine.Evaluate(ctx, rules, "post", doc)` после fetch |
| `Unpost` | `engine.Evaluate(ctx, rules, "unpost", doc)` после fetch |
| `UpdateAndRepost` | `engine.Evaluate(ctx, rules, "update", doc)` |
| `List` | `engine.EvaluateForList(ctx, rules, items)` — post-fetch filtering |

**Обёрнуть в хелпер:**
```go
func (s *BaseDocumentService[T,L]) checkCELPolicy(ctx context.Context, action string, doc T) error {
    if s.PolicyEngine == nil { return nil }
    rules := security.GetApplicableRules(ctx, s.EntityName, action)
    if len(rules) == 0 { return nil }
    return s.PolicyEngine.Evaluate(ctx, rules, action, doc)
}
```

---

### Step 9 — Интеграция в CatalogService

**Обновить:** `internal/domain/service.go`

Аналогично: добавить `PolicyEngine` + `checkCELPolicy()` в те же точки (Create, GetByID, Update, Delete, List).

---

### Step 10 — CRUD API для PolicyRules

**Новые файлы:**
- `internal/infrastructure/http/v1/dto/policy_rule.go` — request/response DTO
- `internal/infrastructure/http/v1/handlers/policy_rule.go` — handler

**Эндпоинты:**

| Method | Path | Описание |
|---|---|---|
| `POST` | `/api/v1/security/profiles/:profileId/rules` | Создать правило (валидация CEL при сохранении) |
| `GET` | `/api/v1/security/profiles/:profileId/rules` | Список правил профиля |
| `GET` | `/api/v1/security/profiles/:profileId/rules/:ruleId` | Получить правило |
| `PUT` | `/api/v1/security/profiles/:profileId/rules/:ruleId` | Обновить правило |
| `DELETE` | `/api/v1/security/profiles/:profileId/rules/:ruleId` | Удалить правило |
| `POST` | `/api/v1/security/rules/validate` | Валидация CEL-выражения (без сохранения) |

**При сохранении:**
1. `PolicyEngine.Compile(expression)` — если ошибка → 400 с описанием синтаксической ошибки
2. Сохранить в БД
3. `ProfileProvider.Invalidate(userID)` для всех пользователей с этим профилем

---

### Step 11 — Wiring в main.go + router

- Создать `PolicyEngine` в `main.go`
- Прокинуть в `RouterConfig`
- Прокинуть в `BaseDocumentService` и `CatalogService` через конфиг
- Зарегистрировать маршруты API правил

---

### Step 12 — Тесты

| Тест | Что проверяем |
|---|---|
| `cel_engine_test.go` | Компиляция, Evaluate: allow/deny/priority, невалидные выражения, пустые правила |
| `cel_context_test.go` | WithPolicyRules / GetApplicableRules: фильтрация по entity+action |
| `policy_rule_test.go` | Validate, MatchesAction, модельные тесты |
| `service integration` | Мок PolicyEngine → проверка что Create/Update/Delete блокируются deny-правилом |

**Примеры тестовых CEL-выражений:**
- `doc.total_amount > 1000000` → deny update (лимит суммы)
- `doc.status != "draft"` → deny delete (нельзя удалять не-черновики)
- `!user.roles.exists(r, r == "accountant")` → deny post (только бухгалтер проводит)
- `timestamp(doc.date) < now - duration("168h")` → deny read (документы старше недели)

---

### Step 13 — Документация

**Обновить:** `docs/08-auth-and-security.md`

Добавить секцию "CEL Policy Engine" с:
- Описание архитектуры
- Доступные переменные и функции
- Семантика effect/priority
- Примеры выражений
- API эндпоинты

---

## Порядок выполнения и зависимости

```
Step 1  (миграция)          — нет зависимостей
Step 2  (domain model)      — нет зависимостей
Step 3  (CEL engine)        — зависит от Step 2
Step 4  (context helpers)   — зависит от Step 2
Step 5  (repository)        — зависит от Step 1, 2
Step 6  (cache/provider)    — зависит от Step 2, 5
Step 7  (middleware)        — зависит от Step 4, 6
Step 8  (document service)  — зависит от Step 3, 4
Step 9  (catalog service)   — зависит от Step 3, 4
Step 10 (CRUD API)          — зависит от Step 3, 5
Step 11 (wiring)            — зависит от Step 7, 8, 9, 10
Step 12 (тесты)             — зависит от Step 3, 4, 5
Step 13 (документация)      — после всех шагов
```

**Параллелизуемые группы:**
1. Steps 1 + 2 + 4 — параллельно
2. Steps 3 + 5 — после группы 1
3. Steps 6 + 8 + 9 + 10 — после группы 2
4. Steps 7 + 11 + 12 — после группы 3
5. Step 13 — финал

---

## Оценка сложности

| Шаг | Файлы | Оценка |
|---|---|---|
| Migration | 1 | S |
| Domain model | 1 | S |
| CEL Engine | 1 | **L** (ядро) |
| Context helpers | 1 | S |
| Repository | 1-2 | M |
| Cache/Provider update | 1 | S |
| Middleware update | 1 | S |
| Document service | 1 | M |
| Catalog service | 1 | M |
| CRUD API | 2-3 | M |
| Wiring | 2 | S |
| Tests | 3-4 | **L** |
| Docs | 1 | S |

**Итого: ~15-18 файлов, ~1200-1500 строк нового кода**
