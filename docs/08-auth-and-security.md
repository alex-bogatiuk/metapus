# Аутентификация и Безопасность

> JWT-аутентификация, RBAC-авторизация, refresh tokens, brute-force protection и организационная фильтрация данных.

---

## Login Flow — от email/password до пары токенов

```
POST /api/v1/auth/login { email, password }
│
├── requireTenantID(ctx) — Login требует X-Tenant-ID
├── userRepo.GetByEmail(ctx, email)
│   └── не найден → "invalid credentials" (без раскрытия существования email)
├── user.CanLogin()
│   ├── !IsActive → "account is disabled" (403)
│   └── IsLocked() → "account is temporarily locked" (403)
├── bcrypt.CompareHashAndPassword(hash, password)
│   ├── ошибка → RecordFailedLogin() + "invalid credentials" (401)
│   └── успех → продолжаем
├── Загрузка ролей, разрешений, организаций
├── generateTokenPair(ctx, user, info)
│   ├── Access token (JWT HS256, 15 мин)
│   └── Refresh token (random 32-byte hex, 7 дней, SHA256 в БД)
├── user.RecordSuccessfulLogin() — сброс счётчика неудач
└── return TokenPair{AccessToken, RefreshToken, ExpiresAt, "Bearer"}
```

---

## JWT Token — структура claims

```json
{
  "iss": "metapus",
  "sub": "user-uuid",
  "exp": 1707000900,
  "uid": "user-uuid",
  "tid": "tenant-uuid",
  "email": "u@acme.com",
  "roles": ["accountant"],
  "perms": ["catalog:warehouse:read", "document:goods_receipt:post"],
  "orgs": ["org-1", "org-2"],
  "adm": false
}
```

| Claim | Использование |
|-------|--------------|
| `uid` | `security.GetUserID()`, audit logging |
| `tid` | Tenant match check (JWT vs X-Tenant-ID) |
| `roles` | `RequireRole()` middleware |
| `perms` | `RequirePermission()` middleware — O(1) set lookup |
| `orgs` | `AccessScope` — фильтрация по организациям |
| `adm` | Admin bypass — все разрешения автоматически |

---

## Token Validation — проверка при каждом запросе

Auth middleware (`middleware/auth.go`):

1. **Извлечение токена** из `Authorization: Bearer <token>`
2. **Валидация JWT** — подпись (HS256 + secret), срок (`exp`), структура claims
3. **Tenant match** — `X-Tenant-ID` (заголовок) == `tid` (JWT). При несовпадении → 403
4. **Инжекция UserContext** в context — доступно через `appctx.GetUser(ctx)`

---

## Permission Check — проверка прав на endpoint

Каждый маршрут защищён конкретным разрешением. Формат: `{entity_type}:{entity_name}:{action}`

```
RequirePermission("document:goods_receipt:post")
├── user := appctx.GetUser(ctx)
│   └── nil → "authentication required" (401)
├── if user.IsAdmin → пропускаем (admin bypass)
└── permSet lookup (O(1) map[string]struct{})
    └── не найдено → "insufficient permissions" (403)
```

**Примеры permissions:**
- `catalog:warehouse:read`
- `catalog:counterparty:create`
- `document:goods_receipt:post`
- `register:stock:read`

---

## Refresh Token Flow

```
POST /api/v1/auth/refresh { refreshToken }
│
├── tokenHash := SHA256(refreshToken)
├── tokenRepo.GetRefreshToken(ctx, tokenHash)
├── token.IsValid() — не отозван, не истёк
├── userRepo.GetByID + CanLogin() — пользователь может быть деактивирован
├── Загрузка АКТУАЛЬНЫХ ролей/разрешений (могли измениться)
├── Ревокация старого refresh token (reason: "refreshed")
└── generateTokenPair → новая пара (ротация)
```

**Ключевые моменты:**
- Refresh token в БД хранится как **SHA256 хеш** (не сам токен)
- При каждом refresh старый токен **отзывается** (ротация)
- Загружаются **актуальные** разрешения (не из предыдущего JWT)
- Включает UserAgent и IPAddress для аудита

---

## Brute-Force Protection

```
Неверный пароль
├── user.RecordFailedLogin(maxAttempts=5, lockDuration=15min)
│   ├── FailedLoginAttempts++
│   └── if >= 5 → LockedUntil = now + 15min
│
Следующая попытка (аккаунт заблокирован)
├── user.CanLogin() → IsLocked() → true → 403
│
После истечения lockDuration
├── IsLocked() → false → попытка разрешена
│
Успешный вход
└── FailedLoginAttempts = 0, LockedUntil = nil
```

Автоматическая разблокировка по времени, без background job.

---

## Access Scope — организационная фильтрация

`AccessScope` ограничивает видимость данных на уровне организаций внутри тенанта:

```go
scope := security.NewAccessScope(ctx)
// scope.CanAccessOrg(orgID)     — проверка доступа к организации
// scope.FilterOrgIDs(requested) — пересечение с разрешёнными
// scope.RequirePermission(...)  — программная проверка в service layer
```

- **Admin** видит все организации
- Обычный пользователь видит только `AllowedOrgIDs` из JWT claims
- `FilterOrgIDs` защищает от передачи чужих orgID в query params

---

## Row-Level Security (RLS) — фильтрация строк по измерениям

RLS ограничивает видимость записей на основе **измерений** (dimensions) — логических категорий доступа.

### Архитектура

```
SecurityContextMiddleware
  → BuildDataScope(jwtOrgIDs, profileDimensions)
    → DataScope { IsAdmin, Dimensions: map[string][]string, ReadOnly }

ParseListFilter(c)
  → filter.DataScope = security.GetDataScope(ctx)
    → BaseCatalogRepo / BaseDocumentRepo
      → buildWhereConditions()
        → DataScope.ApplyConditions(rlsDimensions)
          → WHERE organization_id IN ($1,$2) AND supplier_id IN ($3)
```

### Ключевые компоненты

| Компонент | Файл | Назначение |
|-----------|------|-----------|
| `DataScope` | `internal/core/security/data_scope.go` | Хранит разрешённые ID по измерениям |
| `RLSDimensionable` | `internal/core/security/dimensionable.go` | Интерфейс для point-checks на сущностях |
| `RegisterRLSDimension` | `document_repo/base.go`, `catalog_repo/base.go` | Маппинг dimension → DB column |
| `ParseListFilter` | `handlers/base.go` | Автоматическая инъекция DataScope в фильтр |

### Как добавить новое измерение

1. В конструкторе репозитория: `repo.RegisterRLSDimension("warehouse", "warehouse_id")`
2. Override `GetRLSDimensions()` на сущности:
```go
func (g *GoodsReceipt) GetRLSDimensions() map[string]string {
    dims := g.Document.GetRLSDimensions()  // organization
    dims["counterparty"] = g.SupplierID.String()
    return dims
}
```
3. В Security Profile администратор добавляет dimension с allowed_ids

### Point-checks (GetByID / Update / Delete)

`BaseDocumentService.checkRLSAccess(ctx, doc)`:
- Извлекает `DataScope` из контекста
- Вызывает `scope.CanAccessRecord(doc.GetRLSDimensions())`
- Если запись не проходит проверку → `403 Forbidden`

### CanMutate guard

Все мутирующие операции (`Create`, `Update`, `Delete`, `UpdateAndRepost`) проверяют `DataScope.CanMutate()` — если scope read-only → `403`.

---

## Field-Level Security (FLS) — контроль доступа к полям

FLS контролирует какие поля видны при чтении и какие можно изменять при записи.

### Архитектура

```
SecurityContextMiddleware
  → WithFieldPolicies(ctx, profile.FieldPolicies)

Read path (Get/List):
  Handler → security.GetFieldPolicy(ctx, entityName, "read")
         → FieldMasker.MaskForRead(entity, policy)  // обнуляет запрещённые поля
         → mapToDTO(entity)
         → JSON response

Write path (Update):
  Service → security.GetFieldPolicy(ctx, entityName, "write")
         → oldDoc = Repo.GetByID(...)
         → FieldMasker.ValidateWrite(oldDoc, newDoc, policy)  // блокирует изменение запрещённых
```

### FieldPolicy Mini-DSL

```go
AllowedFields: ["*"]                          // всё разрешено
AllowedFields: ["*", "-unit_price", "-amount"] // всё кроме цен
AllowedFields: ["number", "date", "posted"]    // только перечисленные
AllowedFields: []                              // ничего (полный блок)

TableParts: {"lines": ["*", "-unit_price", "-amount"]}  // FLS для табличных частей
```

### Ключевые компоненты

| Компонент | Файл | Назначение |
|-----------|------|-----------|
| `FieldPolicy` | `internal/core/security/field_policy.go` | Правила доступа к полям |
| `FieldMasker` | `internal/core/security/field_masker.go` | Маскировка (read) и валидация (write) |
| `field_context.go` | `internal/core/security/field_context.go` | Context helpers: `WithFieldPolicies`, `GetFieldPolicy` |

---

## Security Profiles — подсистема профилей безопасности

Security Profile — именованный набор RLS-измерений и FLS-политик, назначаемый пользователям.

### Таблицы БД

```
security_profiles              — профили (code, name, is_system)
security_profile_dimensions    — RLS: profile_id, dimension_name, allowed_ids UUID[]
security_profile_field_policies — FLS: profile_id, entity_name, action, allowed_fields, table_parts
user_security_profiles         — связь user ↔ profile (M:N)
```

Миграция: `db/migrations/00033_security_profiles.sql`

### Системные профили (seed)

| Код | Описание |
|-----|----------|
| `full_access` | Без ограничений (admin-level) |
| `viewer` | Read-only, скрыты финансовые поля (цены, суммы, НДС) |

### Domain Model

```go
// internal/domain/security_profile/model.go
type SecurityProfile struct {
    ID, Code, Name, Description, IsSystem
    Dimensions    map[string][]string           // RLS
    FieldPolicies map[string]*security.FieldPolicy  // FLS (key: "entity:action")
}
```

### Кэширование

`CachedProfileProvider` (in-memory, TTL 5 мин, конфигурируется через `SECURITY_PROFILE_CACHE_TTL`):
- `GetUserProfile(ctx, userID)` — cache hit или fallback в PostgreSQL
- `Invalidate(userID)` — сброс кэша при изменении профиля
- `InvalidateAll()` — полный сброс

### Repository

`internal/infrastructure/storage/postgres/security_repo/profile.go`:
- `GetByUserID` — основной метод (JOIN user_security_profiles)
- CRUD: `GetByID`, `Create`, `Update`, `Delete`, `List`
- `AssignToUser`, `RemoveFromUser`

---

## SecurityContext Middleware

Выполняется **после** Auth + UserContext. Собирает полный контекст безопасности из JWT + Security Profile.

### Цепочка middleware

```
TenantDB → Auth → UserContext → SecurityContext → [routes]
```

### Логика (`middleware/security_context.go`)

1. Извлекает `UserContext` (JWT claims)
2. **Admin** → `DataScope{IsAdmin: true}`, без FLS
3. Загружает `SecurityProfile` через `ProfileProvider` (кэш)
4. Строит `DataScope`: пересечение JWT OrgIDs ∩ profile dimensions
5. Инжектирует `security.WithDataScope(ctx, scope)` + `security.WithFieldPolicies(ctx, policies)`

### Fail-closed принцип

- Ошибка загрузки профиля → restrictive scope только из JWT orgs
- Невалидный UserID → то же самое
- Нет профиля у пользователя → DataScope из JWT orgs, без FLS

---

## Правила безопасности

- **Least Privilege** — минимальные необходимые разрешения
- **Functional Scopes** — гранулярные разрешения (`product:view_cost`, `invoice:post`)
- **Mandatory Audit** — запись кто, что, когда менял для высокоценных сущностей
- **NO** bypassing security checks в production коде
- **NO** sensitive data (пароли, PII) в audit логах или сообщениях об ошибках
- Tenant isolation — через Database-per-Tenant, не через фильтрацию в SQL

---

---

## CEL Policy Engine — тонкая авторизация на основе выражений

### Назначение

CEL (Common Expression Language) правила позволяют задавать **сложные условия доступа**, которые нельзя выразить через RBAC/RLS/FLS. Например:

> «Менеджер может редактировать заявку ТОЛЬКО если: сумма < 1 000 000 ₽ И статус == draft И дата документа не старше недели»

### Модель данных (`security_policy_rules`)

| Поле | Тип | Описание |
|------|-----|----------|
| `profile_id` | UUID | К какому Security Profile привязано правило |
| `entity_name` | string | Сущность (`goods_receipt`, `*` = все) |
| `actions` | []string | Действия (`create`, `read`, `update`, `delete`, `post`, `unpost`, `*`) |
| `expression` | string | CEL-выражение, возвращающее `bool` |
| `effect` | string | `deny` — заблокировать, `allow` — явно разрешить |
| `priority` | int | Порядок проверки (DESC) |
| `enabled` | bool | Можно отключить без удаления |

### Переменные CEL-выражений

| Переменная | Тип | Содержимое |
|-----------|-----|-----------|
| `doc` | `map<string, any>` | Поля сущности (по db/json-тегам) |
| `user` | `map<string, any>` | `{id, email, roles, orgIds, isAdmin}` |
| `action` | `string` | Текущее действие |
| `now` | `timestamp` | Текущее UTC-время |

### Примеры выражений

```cel
// Запрет на редактирование проведённых документов
doc.posted == true

// Запрет на суммы > 1 000 000 (в minor units, RUB×100)
doc.total_amount > 100000000

// Разрешить только для admin-ов
user.isAdmin == true

// Запрет на документы старше 7 дней
now - doc.date > duration("168h")

// Запрет, если статус не draft
doc.status != "draft"
```

### Семантика (порядок применения)

1. Правила сортируются по `priority DESC`
2. Первое правило, для которого CEL-выражение вернуло `true`, **применяется**:
   - `effect=deny` → операция запрещена (403 Forbidden)
   - `effect=allow` → операция явно разрешена
3. Если ни одно правило не совпало → **default allow** (RLS/FLS уже отфильтровали)

### Архитектура

```
SecurityProfile.PolicyRules
         │
         ▼
middleware/security_context.go
  → security.WithPolicyRules(ctx, rules)
         │
         ▼
CatalogService / BaseDocumentService
  → checkCELPolicy(ctx, action, entity)
     → security.GetApplicableRules(ctx, entityName, action)
     → PolicyEngine.Evaluate(ctx, rules, action, entity)
```

### Ключевые файлы

| Файл | Роль |
|------|------|
| `internal/core/security/cel_engine.go` | `PolicyEngine` — компиляция, кэш, вычисление |
| `internal/core/security/cel_context.go` | `WithPolicyRules`, `GetApplicableRules` |
| `internal/domain/security_profile/policy_rule.go` | Доменная модель `PolicyRule` |
| `internal/domain/security_profile/repository.go` | `PolicyRuleRepository` интерфейс |
| `internal/infrastructure/storage/postgres/security_repo/policy_rule.go` | PostgreSQL реализация |
| `internal/domain/document_service.go` | `checkCELPolicy` в `BaseDocumentService` |
| `internal/domain/service.go` | `checkCELPolicy` в `CatalogService` |

### REST API

Все эндпоинты требуют роль `admin`.

```
GET    /api/v1/security/profiles/:profileId/rules
POST   /api/v1/security/profiles/:profileId/rules
GET    /api/v1/security/profiles/:profileId/rules/:ruleId
PUT    /api/v1/security/profiles/:profileId/rules/:ruleId
DELETE /api/v1/security/profiles/:profileId/rules/:ruleId
POST   /api/v1/security/rules/validate   — валидация CEL-выражения без сохранения
```

### Добавление нового правила

```json
POST /api/v1/security/profiles/{profileId}/rules
{
  "name": "Block large amounts for managers",
  "entityName": "goods_receipt",
  "actions": ["create", "update"],
  "expression": "doc.total_amount > 100000000",
  "effect": "deny",
  "priority": 100,
  "enabled": true
}
```

### Производительность

- CEL-программы **кэшируются** в `sync.Map` после первой компиляции
- После обновления правила через API кэш инвалидируется (`InvalidateCache(ruleID)`)
- При загрузке профиля из базы загружаются только `enabled=true` правила

---

## Связанные документы

- [07-multi-tenancy.md](07-multi-tenancy.md) — tenant isolation и tenant match
- [13-request-lifecycle.md](13-request-lifecycle.md) — место Auth в middleware chain
- [06-infrastructure-layer.md](06-infrastructure-layer.md) — middleware порядок
