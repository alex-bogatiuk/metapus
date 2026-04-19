# 26. Подсистема Автоматизации (Automation Engine v2)

Подсистема Автоматизации — отказоустойчивый, изолированный по тенантам механизм реагирования на бизнес-события. Версия 2 вводит трёхуровневую модель доставки (**Account → Channel → Subscriber**), двухфазный движок, CRON-планировщик и AES-256-GCM шифрование учётных данных.

## 1. Архитектура

```
┌────────────────────────────────────────────────────────────────┐
│  CRUD Pipeline / Posting Engine                                │
│  ─────────────────────────────────                             │
│  Генерирует событие в Outbox:                                  │
│  { doc: {...}, action: "posted", entityType: "goods_receipt" } │
└────────────────────┬───────────────────────────────────────────┘
                     │
                     ▼
┌────────────────────────────────────────────────────────────────┐
│  Background Worker (Outbox Consumer)                           │
│  ─────────────────────────────────                             │
│  1. Вычитывает событие из Outbox                               │
│  2. Инициализирует tenant-контекст                             │
│  3. Вызывает Engine.HandleEvent(ctx, eventType, payload)       │
└────────────────────┬───────────────────────────────────────────┘
                     │
         ┌───────────┴───────────┐
         ▼                       ▼
  Phase 1: EVALUATE        Phase 2: DELIVER
  (CPU-bound)              (I/O-bound)
  ─ Fetch active rules     ─ Resolve subscribers
  ─ Check cooldowns        ─ Execute adapters
  ─ Evaluate CEL           ─ Record history
  ─ Render templates       ─ Update rule stats
```

### Гарантии:
- **At-Least-Once Delivery** через Postgres Outbox.
- **Tenant Isolation**: каждый тенант имеет свою БД-схему.
- **Immutable Ledger**: записи истории не обновляются, только вставляются.

## 2. Трёхуровневая модель доставки

### Account (Аккаунт)
Централизованное хранилище учётных данных отправителя.

| Поле | Описание |
|------|----------|
| `code` | Уникальный код (например, `tg_main`) |
| `name` | Человекочитаемое имя |
| `account_type` | `telegram`, `email`, `webhook`, `rocketchat`, `slack` |
| `config` | JSONB параметры (base_url, timeout и т.д.) |
| `credentials_enc` | Зашифрованные учётные данные (AES-256-GCM) |
| `status` | `active`, `error`, `disabled` |
| `version` | Optimistic Locking |

**Один Account** (один Bot Token) → **много Channels** (много Chat ID).

### Channel (Канал)
Конкретный адрес доставки, ссылающийся на Account для получения учётных данных.

| Поле | Описание |
|------|----------|
| `code` | Уникальный код (например, `ch_finance_alerts`) |
| `name` | Человекочитаемое имя |
| `account_id` | FK → Account (наследует тип и credentials) |
| `destination` | JSONB: `{ "chat_id": "-100xxx" }` / `{ "email": "..." }` / `{ "url": "..." }` |
| `version` | Optimistic Locking |

### Subscriber (Подписчик)
Полиморфная привязка правила к целевому получателю. Хранится как **табличная часть** правила.

| Тип | Описание | Ключевое поле |
|-----|----------|---------------|
| `channel` | Внешний канал (Telegram, Email, Webhook) | `channel_id` |
| `user` | Конкретный пользователь (UI-нотификация) | `user_id` |
| `role` | Все пользователи с данной ролью | `role_name` |
| `doc_field` | ID пользователя из поля документа | `doc_field_path` |

## 3. Rule (Правило автоматизации)

Правило описывает цепочку: **событие → условие → реакция**.

### Ключевые поля:

| Поле | Описание |
|------|----------|
| `trigger_type` | `entity_event`, `business_event`, `scheduled`, `incoming_webhook` |
| `event_type` | Тип события (например, `document.goods_receipt.posted`) |
| `condition_cel` | CEL-выражение фильтрации (опционально) |
| `reaction_type` | `notify`, `webhook_call`, `chain`, `create_record` |
| `message_format` | `text`, `html`, `markdown` |
| `action_template` | Go Template шаблон сообщения |
| `priority` | Порядок выполнения (0–100) |
| `max_retries` | Лимит повторов при ошибке (по умолчанию 3) |
| `cooldown_seconds` | Минимальный интервал между срабатываниями |
| `chain_rule_ids` | UUID[] для цепных реакций (`reaction_type = chain`) |

### CRON-правила:
Для `trigger_type = scheduled` поле `event_type` должно начинаться с `cron:`:
```
cron:0 */5 * * * *    — каждые 5 минут (6-field формат с секундами)
cron:0 0 9 * * 1-5    — в 09:00 по будням
```

Планировщик (`robfig/cron/v3`) динамически обновляет расписание каждый час.

## 4. Двухфазный движок

### Phase 1: Evaluate (CPU-bound)
```go
func (e *Engine) Evaluate(ctx context.Context, eventType string, payload map[string]any) []EvalResult
```

1. Загружает все активные правила по `event_type`.
2. Проверяет cooldown: `now - last_executed_at > cooldown_seconds`.
3. Вычисляет CEL-условие через кэшированную AST-программу.
4. Рендерит Go Template (`text/template`) с payload-данными.
5. Возвращает `[]EvalResult` — список готовых к доставке сообщений.

### Phase 2: Deliver (I/O-bound)
```go
func (e *Engine) Deliver(ctx context.Context, results []EvalResult) []DeliveryResult
```

1. Для каждого subscriber резолвит Channel → Account → Credentials.
2. Вызывает соответствующий адаптер.
3. Записывает `HistoryEntry` с `duration_ms`, `status`, `error_text`.
4. Обновляет статистику правила (`IncrementStats`).
5. Обновляет статус аккаунта (`UpdateLastResult`).
6. При `reaction_type = chain` публикует новое событие через `OutboxPublisher`.

## 5. Адаптеры

Каждый адаптер реализует интерфейс v2:

```go
type Adapter interface {
    Deliver(ctx context.Context, destination, accountConfig map[string]any,
            credentials []byte, payload string) error
}
```

### Встроенные адаптеры:

| Адаптер | Описание | destination |
|---------|----------|-------------|
| `TelegramAdapter` | Отправка через Telegram Bot API | `chat_id`, `disable_web_page_preview` |
| `EmailAdapter` | Отправка через SMTP (`net/smtp`) | `email`, `subject` (или 1-я строка payload) |
| `WebhookAdapter` | HTTP POST/PUT/GET к внешнему URL | `url`, `method`, `headers`, `auth_type` |
| `InternalNotificationAdapter` | Запись в `sys_notifications` (UI) | — (используется для `user`/`role` subscribers) |

## 6. Шифрование учётных данных

Все credentials шифруются **AES-256-GCM** перед записью в БД.

```
Формат хранения: nonce (12 bytes) || ciphertext || auth_tag (16 bytes)
```

- Ключ: переменная окружения `AUTOMATION_ENCRYPTION_KEY` (ровно 32 байта).
- Nonce генерируется `crypto/rand` при каждом шифровании (гарантия уникальности).
- GCM обеспечивает как шифрование, так и проверку целостности (AEAD).

**Важно**: Credentials никогда не возвращаются в API-ответах. Для обновления пользователь отправляет новый plaintext через `PATCH /credentials`.

## 7. CEL (Common Expression Language)

[CEL](https://github.com/google/cel-spec) — быстрый, безопасный, Тьюринг-неполный язык выражений от Google.

### Окружение:
| Переменная | Тип | Описание |
|------------|-----|----------|
| `doc` | `dyn` | DTO документа/сущности |
| `action` | `string` | Тип действия (`created`, `posted`, ...) |
| `entityType` | `string` | Системное имя сущности |

### Примеры:
```cel
doc.totalAmount > 100000 && action == "posted"
doc.counterpartyId != "" && doc.status == "approved"
has(doc.lines) && size(doc.lines) > 0
```

CEL-программы кэшируются. При обновлении правила кэш инвалидируется через `InvalidateCELCache()`.

## 8. История и Обсервабилити

Каждое срабатывание записывается в `sys_automation_history`:

| Поле | Описание |
|------|----------|
| `rule_id` | FK → Rule |
| `subscriber_id` | FK → Subscriber |
| `event_type` | Тип события |
| `aggregate_id` | ID документа/сущности |
| `status` | `success`, `error`, `skipped` |
| `request_payload` | Отрендеренный текст сообщения |
| `error_text` | Текст ошибки (если `status = error`) |
| `duration_ms` | Время выполнения адаптера |
| `attempt` | Номер попытки (1..max_retries) |

> **Архитектурное решение**: записи истории **иммутабельны** (Immutable Ledger). При ошибке адаптера транзакция Outbox не откатывается — создаётся запись с `status = error`, пользователь видит её в логах и может настроить Retries.

## 9. Миграции БД

| Миграция | Таблица | Описание |
|----------|---------|----------|
| `00025` | `sys_automation_accounts` | Аккаунты с зашифрованными credentials |
| `00026` | `sys_automation_rules` | Правила v2 (trigger_type, reaction_type, ...) |
| `00027` | `sys_automation_channels` | Каналы доставки |
| `00029` | `sys_automation_subscribers` | Полиморфные подписчики |
| `00030` | `sys_automation_history` | История выполнений |
| `00031` | (seed) | Права доступа (CRUD) для ролей Admin/Accountant |

## 10. API Endpoints

```
# Accounts
GET    /api/v1/automation/accounts
POST   /api/v1/automation/accounts
GET    /api/v1/automation/accounts/:id
PUT    /api/v1/automation/accounts/:id
DELETE /api/v1/automation/accounts/:id
PATCH  /api/v1/automation/accounts/:id/credentials

# Channels
GET    /api/v1/automation/channels
POST   /api/v1/automation/channels
GET    /api/v1/automation/channels/:id
PUT    /api/v1/automation/channels/:id
DELETE /api/v1/automation/channels/:id

# Rules (subscribers are inline)
GET    /api/v1/automation/rules
POST   /api/v1/automation/rules
GET    /api/v1/automation/rules/:id
PUT    /api/v1/automation/rules/:id
DELETE /api/v1/automation/rules/:id
PATCH  /api/v1/automation/rules/:id/toggle
POST   /api/v1/automation/rules/test

# History
GET    /api/v1/automation/history

# Meta (enum values for UI)
GET    /api/v1/automation-meta
```

## 11. Переменные окружения

| Переменная | Обязательна | Описание |
|------------|-------------|----------|
| `AUTOMATION_ENCRYPTION_KEY` | Да (для worker) | 32 байта для AES-256-GCM |

## 12. Тестирование

Unit-тесты покрывают:
- `internal/core/crypto/aes_test.go` — Round-trip шифрование, уникальность nonce, ошибки ключа, повреждение ciphertext.
- `internal/domain/automations/validation_test.go` — Валидация всех моделей (Account, Channel, Rule, Subscriber) с позитивными и негативными сценариями.

```bash
go test -v ./internal/core/crypto/...
go test -v ./internal/domain/automations/...
```
