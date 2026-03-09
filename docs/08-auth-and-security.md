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

## Правила безопасности

- **Least Privilege** — минимальные необходимые разрешения
- **Functional Scopes** — гранулярные разрешения (`product:view_cost`, `invoice:post`)
- **Mandatory Audit** — запись кто, что, когда менял для высокоценных сущностей
- **NO** bypassing security checks в production коде
- **NO** sensitive data (пароли, PII) в audit логах или сообщениях об ошибках
- Tenant isolation — через Database-per-Tenant, не через фильтрацию в SQL

---

## Связанные документы

- [07-multi-tenancy.md](07-multi-tenancy.md) — tenant isolation и tenant match
- [13-request-lifecycle.md](13-request-lifecycle.md) — место Auth в middleware chain
- [06-infrastructure-layer.md](06-infrastructure-layer.md) — middleware порядок
