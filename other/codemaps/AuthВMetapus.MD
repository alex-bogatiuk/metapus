# Аутентификация и Авторизация в Metapus: От логина до проверки прав доступа

Карта показывает полный путь аутентификации (JWT), авторизации (roles/permissions), управления сессиями (refresh tokens) и защиты от атак (brute-force, tenant mismatch).

**Ключевые точки:**
- Login: email/password → bcrypt → JWT + refresh token (1a)
- Token validation: Bearer → JWT parse → UserContext в context (2a)
- Permission check: middleware → JWT claims → allow/deny (3a)
- Refresh flow: refresh token → rotate → new pair (4a)
- Brute-force protection: failed attempts → account lock (5a)
- Access scope: organization-level filtering (6a)

---

## Trace 1: Login — Аутентификация пользователя

Полный путь от email/password до пары токенов (access + refresh).

```
POST /api/v1/auth/login
Body: { "email": "user@acme.com", "password": "secret" }
│
├── AuthHandler.Login(c) → Service.Login(ctx, creds, info)
│   │
│   ├── requireTenantID(ctx) <-- 1a
│   │   └── tenant.GetTenantID(ctx)
│   │       └── если пуст → "tenant is required" (400)
│   │
│   ├── userRepo.GetByEmail(ctx, email) <-- 1b
│   │   └── SELECT * FROM auth_users WHERE email = $1
│   │       ├── not found → "invalid credentials" (401)
│   │       └── found → User{ID, Email, PasswordHash, IsActive, ...}
│   │
│   ├── user.CanLogin() <-- 1c
│   │   ├── !IsActive → "account is disabled" (403)
│   │   └── IsLocked() → "account is temporarily locked" (403)
│   │
│   ├── bcrypt.CompareHashAndPassword(hash, password) <-- 1d
│   │   ├── ошибка → RecordFailedLogin() + "invalid credentials" (401)
│   │   └── успех → продолжаем
│   │
│   ├── Загрузка ролей и разрешений <-- 1e
│   │   ├── userRepo.LoadRoles(ctx, userID)
│   │   │   └── SELECT r.* FROM auth_roles r
│   │   │       JOIN auth_user_roles ur ON ...
│   │   ├── userRepo.LoadPermissions(ctx, userID)
│   │   │   └── SELECT DISTINCT p.code FROM auth_permissions p
│   │   │       JOIN auth_role_permissions rp ON ...
│   │   │       JOIN auth_user_roles ur ON ...
│   │   └── userRepo.LoadOrganizations(ctx, userID)
│   │       └── SELECT organization_id FROM auth_user_organizations
│   │
│   ├── generateTokenPair(ctx, user, info) <-- 1f
│   │   ├── Access token (JWT, 15 мин) <-- 1g
│   │   │   └── Claims{uid, tid, email, roles, perms, orgs, adm}
│   │   │       └── jwt.SignedString(HS256, secret)
│   │   │
│   │   └── Refresh token (random, 7 дней) <-- 1h
│   │       ├── generateRandomToken(32) → hex string
│   │       ├── SHA256(token) → tokenHash
│   │       └── tokenRepo.SaveRefreshToken(ctx, RefreshToken{
│   │           ID, UserID, TokenHash, ExpiresAt, UserAgent, IPAddress
│   │       })
│   │
│   ├── user.RecordSuccessfulLogin() <-- 1i
│   │   └── FailedLoginAttempts = 0, LockedUntil = nil, LastLoginAt = now
│   │
│   └── return TokenPair{AccessToken, RefreshToken, ExpiresAt, "Bearer"}
```

| ID | Описание | Файл |
|----|----------|------|
| **1a** | Проверка tenant. Login требует `X-Tenant-ID` — пользователи изолированы по тенантам (разные БД) | `auth/service.go:79` |
| **1b** | Поиск пользователя. Email уникален в пределах БД тенанта. При не найденном пользователе возвращается generic "invalid credentials" (без раскрытия существования email) | `auth/service.go:166` |
| **1c** | Проверка возможности входа. Проверяется `IsActive` (не деактивирован) и `IsLocked` (не заблокирован после brute-force) | `auth/model.go:68` |
| **1d** | Проверка пароля. bcrypt с DefaultCost. При неверном пароле — инкремент `FailedLoginAttempts` | `auth/service.go:179` |
| **1e** | Загрузка ролей/разрешений. Роли и разрешения загружаются из связанных таблиц и будут включены в JWT claims | `auth/service.go:187` |
| **1f** | Генерация пары токенов. Access (JWT, 15 мин) + Refresh (random hex, 7 дней) | `auth/service.go:386` |
| **1g** | Access token. JWT HS256 с claims: `uid`, `tid` (tenant), `email`, `roles`, `perms`, `orgs`, `adm`. TTL = 15 минут | `auth/jwt.go:52` |
| **1h** | Refresh token. Случайный 32-байтовый hex. В БД хранится SHA256 хеш (не сам токен). Включает `UserAgent` и `IPAddress` для аудита | `auth/service.go:404` |
| **1i** | Успешный логин. Сбрасывает счётчик неудачных попыток и записывает время последнего входа | `auth/model.go:88` |

---

## Trace 2: Token Validation — Проверка JWT при каждом запросе

Middleware `Auth` валидирует JWT из заголовка `Authorization` и создаёт `UserContext` в request context.

```
HTTP Request
Headers: Authorization: Bearer eyJhbGci...
│
├── middleware.Auth(validator) <-- auth.go:19
│   │
│   ├── Извлечение токена <-- 2a
│   │   ├── authHeader := c.GetHeader("Authorization")
│   │   │   └── если пуст → "missing authorization header" (401)
│   │   └── strings.SplitN(authHeader, " ", 2)
│   │       └── parts[0] != "bearer" → "invalid format" (401)
│   │
│   ├── validator.ValidateToken(tokenString) <-- 2b
│   │   └── JWTService.ValidateToken()
│   │       ├── jwt.ParseWithClaims(token, &Claims{}, keyFunc)
│   │       │   ├── Проверка алгоритма: HMAC only
│   │       │   ├── Проверка подписи: secret key
│   │       │   └── Проверка срока: exp > now
│   │       └── return &UserContext{
│   │           UserID, TenantID, Email,
│   │           Roles, Permissions, OrgIDs, IsAdmin
│   │       }
│   │
│   ├── Tenant match check <-- 2c
│   │   ├── resolvedTenantID := tenant.GetTenantID(ctx)
│   │   │   └── (из TenantDB middleware, выполненного ранее)
│   │   └── if resolvedTenantID != user.TenantID
│   │       └── "tenant mismatch" (403)
│   │           └── details: {header_tenant_id, token_tenant_id}
│   │
│   └── Инжекция в context <-- 2d
│       ├── ctx = appctx.WithUser(ctx, user)
│       │   └── UserContext{UserID, TenantID, Email, Roles, Permissions, ...}
│       ├── c.Set("user_id", user.UserID)
│       └── c.Set("permissions", user.Permissions)
```

| ID | Описание | Файл |
|----|----------|------|
| **2a** | Извлечение токена. Парсинг заголовка `Authorization: Bearer <token>`. Строгая проверка формата | `middleware/auth.go:22` |
| **2b** | Валидация JWT. Проверяется подпись (HS256 + secret), срок действия (`exp`), и структура claims. Возвращается `UserContext` | `auth/jwt.go:86` |
| **2c** | Tenant match. Критическая проверка: `X-Tenant-ID` из заголовка должен совпадать с `tid` из JWT. Предотвращает подмену тенанта — пользователь не может получить доступ к данным чужого тенанта даже с валидным токеном | `middleware/auth.go:46` |
| **2d** | Инжекция UserContext. Вся информация о пользователе (ID, роли, разрешения, организации) доступна через `appctx.GetUser(ctx)` во всех нижележащих слоях | `middleware/auth.go:58` |

---

## Trace 3: Permission Check — Проверка прав на endpoint

Middleware `RequirePermission` проверяет конкретное разрешение из JWT claims перед выполнением handler.

```
Проверка прав доступа
│
├── RegisterDocumentRoutes() <-- route_helpers.go:68
│   └── POST "/:id/post" + RequirePermission("document:goods_receipt:post")
│
├── RequirePermission("document:goods_receipt:post") <-- 3a
│   │
│   ├── user := appctx.GetUser(ctx) <-- 3b
│   │   └── если nil → "authentication required" (401)
│   │
│   ├── Проверка admin <-- 3c
│   │   └── if user.IsAdmin → c.Next() (пропускаем)
│   │
│   └── Проверка permission <-- 3d
│       ├── permSet := c.Get("permissions_set") → map[string]struct{}
│       │   └── O(1) lookup (set создаётся в Auth middleware)
│       ├── _, ok = permSet[permission] → c.Next()
│       ├── fallback: for _, perm := range permissions ([]string)
│       └── не найдено → "insufficient permissions" (403)
│           └── details: {required_permission: "document:goods_receipt:post"}
│
├── Формат permission string:
│   └── "{entity_type}:{entity_name}:{action}"
│       ├── "catalog:warehouse:read"
│       ├── "catalog:counterparty:create"
│       ├── "document:goods_receipt:post"
│       └── "register:stock:read"
```

| ID | Описание | Файл |
|----|----------|------|
| **3a** | Per-endpoint permission. Каждый маршрут защищён конкретным разрешением. Формат: `{type}:{name}:{action}` | `route_helpers.go:74` |
| **3b** | Проверка аутентификации. Если `UserContext` отсутствует в context — пользователь не аутентифицирован | `middleware/permission.go:15` |
| **3c** | Admin bypass. Администраторы (`IsAdmin=true` в JWT) автоматически имеют все разрешения | `middleware/permission.go:23` |
| **3d** | Permission lookup. Auth middleware строит `map[string]struct{}` из JWT claims и сохраняет в gin context как `permissions_set`. `RequirePermission` выполняет O(1) lookup по set, с fallback на линейный поиск по `[]string` | `middleware/permission.go:30` |

---

## Trace 4: Refresh Token — Обновление пары токенов

Refresh token позволяет получить новую пару токенов без повторного ввода пароля.

```
POST /api/v1/auth/refresh
Body: { "refreshToken": "a1b2c3d4..." }
│
├── Service.RefreshToken(ctx, refreshToken, info) <-- service.go:223
│   │
│   ├── tokenHash := SHA256(refreshToken) <-- 4a
│   │
│   ├── tokenRepo.GetRefreshToken(ctx, tokenHash) <-- 4b
│   │   └── SELECT * FROM auth_refresh_tokens WHERE token_hash = $1
│   │       ├── not found → "invalid refresh token" (401)
│   │       └── found → RefreshToken{ID, UserID, ExpiresAt, RevokedAt, ...}
│   │
│   ├── token.IsValid() <-- 4c
│   │   ├── RevokedAt != nil → "expired or revoked" (401)
│   │   └── time.Now().After(ExpiresAt) → "expired or revoked" (401)
│   │
│   ├── userRepo.GetByID(ctx, token.UserID) <-- 4d
│   │   └── Загрузка пользователя + проверка CanLogin()
│   │
│   ├── Загрузка ролей/разрешений (актуальные данные) <-- 4e
│   │   └── LoadRoles + LoadPermissions + LoadOrganizations
│   │
│   ├── Ревокация старого refresh token <-- 4f
│   │   └── tokenRepo.RevokeRefreshToken(ctx, token.ID, "refreshed")
│   │       └── UPDATE auth_refresh_tokens SET revoked_at = NOW(),
│   │           revoked_reason = 'refreshed' WHERE id = $1
│   │
│   └── generateTokenPair(ctx, user, info) <-- 4g
│       └── Новый access token + новый refresh token
│           └── (ротация: старый refresh удалён, новый создан)
```

| ID | Описание | Файл |
|----|----------|------|
| **4a** | Хеширование токена. Refresh token хранится в БД как SHA256 хеш — даже при утечке БД, сами токены не скомпрометированы | `auth/service.go:225` |
| **4b** | Поиск по хешу. Токен ищется в `auth_refresh_tokens` по хешу | `auth/service.go:228` |
| **4c** | Валидация токена. Проверяется: не отозван (`RevokedAt = null`) и не истёк (`ExpiresAt > now`) | `auth/model.go:178` |
| **4d** | Проверка пользователя. Пользователь может быть деактивирован или заблокирован после выдачи refresh token | `auth/service.go:242` |
| **4e** | Актуальные разрешения. При refresh загружаются АКТУАЛЬНЫЕ роли/разрешения (могли измениться после выдачи предыдущего access token) | `auth/service.go:256` |
| **4f** | Ротация токенов. Старый refresh token отзывается с причиной "refreshed". Это предотвращает повторное использование | `auth/service.go:264` |
| **4g** | Новая пара. Генерируется полностью новая пара: access + refresh. Клиент должен сохранить новый refresh token | `auth/service.go:267` |

---

## Trace 5: Brute-Force Protection — Защита от перебора паролей

Автоматическая блокировка аккаунта после N неудачных попыток входа.

```
Brute-Force Protection Flow
│
├── Login attempt с неверным паролем
│   └── bcrypt.CompareHashAndPassword → error <-- 5a
│       │
│       ├── user.RecordFailedLogin(maxAttempts=5, lockDuration=15min)
│       │   ├── FailedLoginAttempts++ <-- 5b
│       │   └── if FailedLoginAttempts >= 5
│       │       └── LockedUntil = now + 15min <-- 5c
│       │
│       ├── userRepo.Update(ctx, user) — сохранение в БД
│       └── return "invalid credentials" (401)
│
├── Следующая попытка входа (аккаунт заблокирован)
│   └── user.CanLogin() <-- 5d
│       └── IsLocked() → time.Now().Before(LockedUntil)
│           └── true → "account is temporarily locked" (403)
│
├── После истечения lockDuration
│   └── user.CanLogin() → IsLocked() → false <-- 5e
│       └── Попытка входа разрешена
│
└── Успешный вход
    └── user.RecordSuccessfulLogin() <-- 5f
        └── FailedLoginAttempts = 0, LockedUntil = nil
```

| ID | Описание | Файл |
|----|----------|------|
| **5a** | Неверный пароль. bcrypt возвращает ошибку при несовпадении хешей | `auth/service.go:179` |
| **5b** | Инкремент счётчика. Каждая неудачная попытка увеличивает `FailedLoginAttempts` | `auth/model.go:80` |
| **5c** | Блокировка аккаунта. После 5 неудач (default) аккаунт блокируется на 15 минут | `auth/model.go:82` |
| **5d** | Проверка блокировки. `IsLocked()` сравнивает `LockedUntil` с текущим временем | `auth/model.go:60` |
| **5e** | Автоматическая разблокировка. Блокировка снимается по истечении `LockDuration` (нет background job) | `auth/model.go:64` |
| **5f** | Сброс при успехе. Успешный вход сбрасывает счётчик и снимает блокировку | `auth/model.go:88` |

---

## Trace 6: Access Scope — Организационная фильтрация данных

`AccessScope` ограничивает видимость данных на уровне организаций внутри тенанта.

```
Access Scope: организационная фильтрация
│
├── Создание scope из context <-- 6a
│   └── security.NewAccessScope(ctx)
│       └── user := appctx.GetUser(ctx)
│           └── AccessScope{
│               TenantID:      user.TenantID,
│               UserID:        user.UserID,
│               IsAdmin:       user.IsAdmin,
│               AllowedOrgIDs: user.OrgIDs, // ["org-1", "org-2"]
│           }
│
├── Проверка доступа к организации <-- 6b
│   └── scope.CanAccessOrg(orgID)
│       ├── IsAdmin → true (всегда)
│       └── orgID in AllowedOrgIDs → true/false
│
├── Фильтрация в запросах <-- 6c
│   └── scope.FilterOrgIDs(requestedOrgs)
│       ├── IsAdmin → return requestedOrgs (без фильтрации)
│       ├── requestedOrgs пуст → return AllowedOrgIDs (все свои)
│       └── intersection(requestedOrgs, AllowedOrgIDs)
│
└── Проверка разрешения <-- 6d
    └── scope.RequirePermission("document", PermissionPost)
        └── if !HasPermission → apperror.NewForbidden(...)
```

| ID | Описание | Файл |
|----|----------|------|
| **6a** | Создание scope. `AccessScope` извлекает данные из `UserContext` в context. Содержит tenant, user, admin flag, allowed organizations | `security/scope.go:63` |
| **6b** | Проверка организации. Пользователь видит только документы/справочники своих организаций. Admin видит всё | `security/scope.go:78` |
| **6c** | Фильтрация orgIDs. Пересечение запрошенных организаций с разрешёнными. Защита от передачи чужих orgID в query params | `security/scope.go:117` |
| **6d** | Проверка разрешения. Программная проверка разрешений в service layer (дополнительно к middleware) | `security/scope.go:106` |

---

## Trace 7: Сводная диаграмма — JWT Claims и их использование

```
┌─────────────────────────────────────────────────────────────────────────┐
│               JWT TOKEN: СТРУКТУРА И ИСПОЛЬЗОВАНИЕ                       │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  JWT Claims (auth/jwt.go:30)                                            │
│  ┌──────────────────────────────────────────────────────────┐          │
│  │  {                                                         │          │
│  │    "iss": "metapus",           // issuer                   │          │
│  │    "sub": "user-uuid",         // subject (user ID)        │          │
│  │    "iat": 1707000000,          // issued at                │          │
│  │    "exp": 1707000900,          // expires (15 min)         │          │
│  │    "uid": "user-uuid",         // → security.GetUserID()   │          │
│  │    "tid": "tenant-uuid",       // → tenant match check     │          │
│  │    "email": "u@acme.com",      // → audit logging          │          │
│  │    "roles": ["accountant"],    // → RequireRole()           │          │
│  │    "perms": [                  // → RequirePermission()     │          │
│  │      "catalog:warehouse:read",                              │          │
│  │      "document:goods_receipt:post"                          │          │
│  │    ],                                                       │          │
│  │    "orgs": ["org-1", "org-2"], // → AccessScope filter     │          │
│  │    "adm": false                // → admin bypass            │          │
│  │  }                                                         │          │
│  └──────────────────────────────────────────────────────────┘          │
│                                                                          │
│  Где используется:                                                      │
│  ┌────────────────────────────────────────────────────┐                │
│  │  Middleware Layer                                    │                │
│  │  ├── Auth      → валидация подписи + expiry         │                │
│  │  ├── Auth      → tenant match (tid vs X-Tenant-ID)  │                │
│  │  ├── Permission→ perms[] проверка                   │                │
│  │  └── Role      → roles[] проверка                   │                │
│  ├────────────────────────────────────────────────────┤                │
│  │  Domain Layer                                       │                │
│  │  ├── AccessScope → orgs[] фильтрация                │                │
│  │  ├── Audit       → uid + email для created_by       │                │
│  │  └── Service     → adm flag для business rules      │                │
│  └────────────────────────────────────────────────────┘                │
│                                                                          │
│  Refresh Token (не JWT, random hex)                                     │
│  ┌──────────────────────────────────────────────────────┐              │
│  │  Клиент: "a1b2c3d4e5f6..."  (plain text)             │              │
│  │  БД:     SHA256("a1b2c3...")  (хеш)                   │              │
│  │  TTL:    7 дней                                        │              │
│  │  Ротация: при каждом refresh старый отзывается         │              │
│  └──────────────────────────────────────────────────────┘              │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```
