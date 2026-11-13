# Аутентификация и Безопасность

> **TL;DR:** Доступ контролируется на четырёх уровнях: JWT-аутентификация (кто это), RBAC-роли (какие эндпоинты доступны), RLS (какие записи в таблице видны) и FLS (какие поля внутри записи можно читать/писать). Плюс CEL-движок для кастомных правил.

> **Тип:** Concept
> **Аудитория:** Developer, Admin
> **Связанные:** [multi-tenancy.md](multi-tenancy.md)

---

## 1. Токены и Аутентификация (JWT + Session State)

Metapus использует короткоживущий JWT как bearer-токен, но серверная сессия
`auth_sessions` остаётся источником истины для revocation, MFA marker и
ограничений admin-сессий.

| Токен | Срок жизни | Описание |
|-------|------------|----------|
| **Access Token** | 15 минут | Подписан HS256. Хранит роли, permissions и `tid` (Tenant ID); после проверки подписи проверяется серверное состояние сессии (с кешем). |
| **Refresh Token** | 7 дней | Случайная 32-байтовая строка. Хешируется SHA256 и лежит в базе. При каждом обновлении (Refresh Token Flow) ротируется (старый отзывается, выдаётся новый). |

**Защита от Brute-Force:** Неудачные попытки входа увеличивают счётчик в БД. При достижении лимита (5) аккаунт блокируется на 15 минут (проверяется динамически, без фоновых джобов).

### 1.1. Серверный отзыв Access Token

Access Token остаётся короткоживущим JWT, но больше не является единственным источником истины. Каждый access token несёт:

- `sid` — ID серверной сессии из `auth_sessions`.
- `uv` — текущее значение `auth_users.auth_version` на момент выпуска токена.
- `pv` — tenant-local версия `auth_policy_state.version` на момент выпуска токена.

HTTP `Auth` middleware проверяет подпись JWT, затем сверяет `sid/uv/pv` с серверным auth-state через in-memory cache для single-instance режима. Параллельные загрузки одного ключа объединяются, поэтому одновременные запросы по одной сессии не создают N одинаковых запросов к БД. Кэш явно инвалидируется при logout, назначении/отзыве роли, изменении permissions роли и изменении доступа пользователя к merchant.

Auth-state и security profiles используют общий `internal/core/cache.Loading` с TTL и ограничением числа записей. Ключ auth-state включает tenant, пользователя и сессию; ключ профиля — tenant и пользователя. Просроченные записи удаляются при обращениях к кешу; при достижении лимита вытесняется самая давно добавленная запись (FIFO). Фоновых goroutine нет, при простое память остаётся ограниченной лимитом записей. Ошибки загрузки не кешируются; отсутствие назначенного профиля кешируется как `nil`. Опубликованные значения считаются неизменяемыми.

Инвалидация удаляет сохранённые значения и отсоединяет незавершённые загрузки соответствующих ключей. Их результат не может повторно заполнить кеш: запрос повторяет чтение после инвалидации. Ожидающие запросы могут отменять ожидание своим context; при отмене ведущего запроса остальные повторяют загрузку со своим context.

| ENV | По умолчанию | Назначение |
|-----|-------------|------------|
| `AUTH_STATE_CACHE_TTL` | `5m` | Срок хранения auth-state. |
| `AUTH_STATE_CACHE_MAX_ENTRIES` | `10000` | Максимум сохранённых auth-state на процесс. |
| `SECURITY_PROFILE_CACHE_TTL` | `5m` | Срок хранения security profile. |
| `SECURITY_PROFILE_CACHE_MAX_ENTRIES` | `10000` | Максимум сохранённых профилей на процесс. |

TTL и лимиты задаются положительными значениями. Лимиты относятся к сохранённым значениям; выполняющиеся запросы ограничиваются жизненным циклом их context.

Refresh tokens по-прежнему хранятся хэшированными в `auth_refresh_tokens`, но каждый refresh token теперь привязан к `auth_sessions.id`. Refresh rotation блокирует текущую строку refresh token через `FOR UPDATE`; повторное использование уже отозванного refresh token отзывает всю auth session.

Семантика отзыва:

- Logout отзывает все refresh tokens и auth sessions пользователя.
- Назначение/отзыв роли увеличивает `auth_users.auth_version` конкретного пользователя.
- Добавление/удаление merchant-доступа или смена merchant-роли увеличивает `auth_users.auth_version` конкретного пользователя.
- Изменение permissions роли и удаление роли увеличивает `auth_policy_state.version` tenant-а.
- Устаревший access token возвращает `401 TOKEN_STALE`; frontend может один раз вызвать `/auth/refresh`.
- Отозванная сессия возвращает `401 SESSION_REVOKED`; frontend должен выполнить logout без retry loop.

Single-instance deployment использует in-memory cache. Для multi-instance deployment нужен distributed cache или pub/sub invalidation поверх той же модели.

### 1.2 Admin MFA, CIDR и Step-Up

`ListUsers` загружает роли и публичные MFA-статусы пакетно. Непустая страница при подключённом MFA-репозитории выполняет четыре SQL-запроса: COUNT, пользователи, роли и MFA-статусы. Пустая страница не запускает обогащение. Запрос статусов читает только `user_id`, `enabled`, `required_after`, без TOTP-секретов. Отсутствие записи означает, что MFA не настроена; ошибка БД прерывает операцию и не выдаётся за выключенную MFA. Это правило действует также при обогащении одного пользователя.

Admin/security_admin login проходит как цепочка:

1. Загрузка пользователя по email.
2. Для admin/security_admin — CIDR allowlist до bcrypt.
3. Проверка пароля.
4. TOTP MFA или enrollment challenge.
5. Финальная `auth_sessions` + refresh cookie выдаются только после завершения flow, кроме grace-сессий.

Конфигурация:

| ENV | Описание |
|-----|----------|
| `ADMIN_MFA_MODE=off|optional|required` | Режим MFA для admin/security_admin login. `off` отключает login-MFA, но не отключает sensitive step-up. |
| `ADMIN_MFA_GRACE_PERIOD_HOURS=168` | Grace period для admin без TOTP в `required` mode. |
| `ADMIN_STEP_UP_TTL_SECONDS=300` | TTL fresh confirmation для sensitive actions. |
| `AUTH_MFA_ENCRYPTION_KEY` | Base64 от 32 bytes. Обязателен для TOTP enrollment и step-up; TOTP secret хранится только AES-256-GCM encrypted. |
| `ADMIN_ALLOWED_CIDRS` | Comma-separated CIDR/IP allowlist для admin/security_admin. |
| `ADMIN_SECURITY_MODE=off|warn|enforce` | CIDR mode. Empty allowlist в `enforce` блокирует admin login/routes. |
| `TRUSTED_PROXIES` | Comma-separated trusted proxies для корректного `ClientIP()`. Empty = no trusted proxy. |

Login API:

- `POST /auth/login` возвращает `200 LoginResponse` только для завершённого входа.
- Для enrolled TOTP возвращает `202 { status: "mfa_required", challengeId, expiresAt }`.
- Для enrollment после grace возвращает `202 { status: "mfa_enrollment_required", challengeId, expiresAt }`.
- Grace-сессия помечается `mfa_pending=true` и должна показывать persistent enrollment UI.

MFA endpoints:

- `POST /auth/mfa/verify` завершает login challenge.
- `POST /auth/mfa/enroll/start` и `/confirm` обслуживают pre-auth enrollment challenge.
- `POST /auth/mfa/enroll/start-session` создаёт enrollment challenge для текущей authenticated session.
- `POST /auth/mfa/backup-code` разрешён только для login recovery.
- `POST /auth/mfa/backup-codes/regenerate` требует fresh step-up.
- `POST /auth/mfa/reset` требует другого `security_admin` и fresh step-up.

Sensitive actions защищаются `SensitiveActionGuard(actionCode)`. Guard ищет fresh
confirmation в `auth_step_up_confirmations` по `session_id`, `user_id`,
`tenant_id`, `action_code`, `expires_at`. `POST /auth/step-up` требует пароль и
TOTP одновременно; backup code не заменяет TOTP для step-up. Успешный step-up
также помечает текущую session как MFA-verified, чтобы старые admin-сессии могли
быть подняты до нового уровня без повторного password-login.

Money movement approval использует object-bound action code
`money_movement.approve:<intent_digest>`. Такой step-up нельзя переиспользовать
для другой суммы, адреса, токена или заявки: admin approval JWS подписывается
только для frozen intent digest.

`System admin` уже общего admin security boundary. Его определяет
`isAdmin=true` или роль `admin`. Роль `security_admin` проходит общие
CIDR/MFA-контроли, но не получает аварийный доступ к бухгалтерским данным.
`RequireSystemAdmin` проверяется поверх RBAC и повторяется доменным сервисом для
прямых вызовов.


TOTP policy:

- 30s period, drift `-1..+1`.
- `last_used_time_step` запрещает replay того же кода.
- 5 неверных попыток за 15 минут блокируют verifier на 15 минут.
- Backup codes: 10 одноразовых кодов по 128 бит entropy, в БД только bcrypt hash.

Audit/security journal — `sys_event_log`, category `security`, event types
`security.admin_ip_denied`, `security.admin_ip_warn`, `security.password_denied`,
`security.mfa_required`, `security.mfa_denied`,
`security.mfa_bruteforce_suspected`, `security.mfa_enrolled`,
`security.mfa_reset`, `security.backup_code_used`,
`security.step_up_required`, `security.step_up_verified`,
`security.step_up_denied`.

### 1.3 Роли мерчантов в портале

Access token для портала хранит список доступных мерчантов в `mids` и роли по каждому мерчанту в `mrs` (`merchant_id -> role`, где `1=Owner`, `2=Manager`, `3=Viewer`). Агрегированная роль вида `portalRole`/`prl` не выпускается, потому что она не имеет корректного смысла при доступе к нескольким мерчантам. Middleware `MerchantPortal` строит scope из `mids/mrs`, а `RequirePortalRole` проверяет роль именно для `merchant_id` из текущего запроса.

Для endpoint-ов, защищённых `RequirePortalRole`, `merchant_id` обязателен. Если `merchant_id` отсутствует, запрос отклоняется до handler-а; система не использует fallback вида "у пользователя есть нужная роль в каком-то другом мерчанте". Это инвариант для write/sensitive операций портала.

Чувствительные действия в портале ограничены ролью в конкретном мерчанте:

- API-ключи: только Owner.
- Просмотр и ротация webhook-secret: только Owner.
- Инвойсы, выводы, платёжные ссылки, тест webhook и редактируемые настройки: Manager или Owner.
- Read-only данные портала остаются ограничены списком мерчантов из `mids`.

При добавлении, удалении или изменении роли доступа к мерчанту увеличивается `auth_users.auth_version`, поэтому старые токены с устаревшим `mrs` получают `TOKEN_STALE` и обновляются перед следующим portal-запросом.

## 2. RBAC (Role-Based Access Control)

Доступ к каждому HTTP-эндпоинту закрыт разрешением (Permission String).
Формат entity permissions: `сущность:имя:действие` (например, `document:crypto_invoice:post`); административные permissions также используют отдельные коды, например `admin.users`.
JWT содержит список разрешений пользователя. Middleware `RequirePermission` делает проверку за `O(1)` (Set lookup). Роль `admin` игнорирует RBAC проверки.

## 3. Security Profiles (RLS и FLS)

Security Profile — это набор тонких политик безопасности, назначаемый пользователю.

### RLS (Row-Level Security)
Ограничивает доступ к **строкам** в базе данных по "Измерениям" (Dimensions).
Например, измерение `organization`. Если в профиле разрешена только Организация А, то репозитории (через метод `ApplyConditions`) автоматически добавят в SQL-запрос условие `WHERE organization_id IN ('Орг А')`. Доступ к остальным записям будет заблокирован как при чтении списка, так и при прямом запросе (point-check выдаст 403 Forbidden).

### FLS (Field-Level Security)
Ограничивает доступ к **полям** внутри сущности.
Например, профиль может скрыть поле `unit_price` для менеджера.
- При чтении (GET) `FieldMasker` обнуляет запрещённые поля перед отправкой JSON клиенту.
- При записи (PUT/PATCH) система загружает старую версию документа из БД и проверяет: если изменены скрытые поля — отбрасывает изменения или выдаёт ошибку.

## 4. CEL Policy Engine (Кастомные правила)

Когда RBAC и RLS недостаточно, администратор пишет выражения на CEL (Common Expression Language от Google). 

**Пример правила:** "Запретить редактирование документа, если его сумма > 1 000 000 и статус = черновик".

1. CEL-программы компилируются и кэшируются в памяти.
2. Перед любой бизнес-операцией (`Create`, `Post`) Сервис передаёт сущность (структуру Go) в PolicyEngine.
3. Если выражение (эффект `deny`) возвращает `true` — операция прерывается (403).

---

## Файловая карта
```path
internal/core/security/                 — RLS (data_scope), FLS (field_masker), CEL (cel_engine)
internal/domain/security_profile/       — Модель профилей безопасности
internal/infrastructure/http/v1/middleware/auth.go — Проверка JWT
internal/infrastructure/http/v1/middleware/admin_security.go — admin CIDR/MFA, step-up и RequireSystemAdmin
```

## Связанные документы
- [multi-tenancy.md](multi-tenancy.md) — как `X-Tenant-ID` в HTTP валидируется на совпадение с `tid` в токене.
