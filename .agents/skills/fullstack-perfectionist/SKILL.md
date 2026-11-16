---
name: fullstack-perfectionist
description: Главный инженер и архитектор транзакционных систем Metapus (Go, Next.js, FinTech/Crypto, Ledger)
---

# Metapus Fullstack & Transactional Systems Engineer

Скилл для разработки сквозного функционала платформы **Metapus**: от миграций БД и финансового ядра до REST API и интерфейсов Next.js.
Стек: **Go 1.27** (Clean Architecture, generics, pgx v5, squirrel, Gin) + **Next.js 16** (App Router, TypeScript, Tailwind tokens, shadcn/ui, Zustand, Zod).

---

## 1. Архитектурные инварианты Metapus

| Правило | Реализация в Metapus | Нарушение |
|---|---|---|
| **Clean Architecture** | `core` → `domain` → `infrastructure`. Domain не импортирует `gin`, `pgx`, `http`. | Импорт сетевых/БД библиотек в domain |
| **Database-per-Tenant** | Пул БД тенантов. `tx.Manager` извлекается из `context.Context`. | Колонка `tenant_id` в таблицах `cat_*`, `doc_*`, `reg_*` |
| **Единая точка ошибок** | Хэндлер вызывает `c.Error(err) + c.Abort()`. Middleware формирует RFC 7807 ответ. | `c.JSON(status, error)` в хэндлере |
| **Strict Error Types** | Ошибки только через `apperror.AppError` с кодами из `internal/core/apperror/error.go`. | `errors.New()`, сырые ошибки без wrapping `fmt.Errorf("action: %w", err)` |
| **CWE-209 Info Leak** | В клиентские ошибки (`apperror.NewValidation`) запрещено отдавать балансы, лимиты, internal ID, SQL. | Утечка точных балансов или ID в API-ответе |
| **ID Generation** | ID сущностей генерируются в Go через `id.New()` (UUIDv7, `internal/core/id/id.go`). | `DEFAULT gen_random_uuid()` в БД для проводных документов |

---

## 2. Работа с деньгами и типами (Precision & Safety)

В Metapus полностью запрещены типы с плавающей точкой (`float32`, `float64`) для любых денежных расчётов.

### Примитивы ядра (`internal/core/types/`)
1. **Фиат и ERP-деньги**: `types.MinorUnits` (`int64`, минимальные единицы: центы, копейки).
   - Масштаб (`decimalPlaces`) берётся строго из метаданных валюты/токена. Запрещён хардкод `/ 100`.
2. **Криптовалютные суммы**: `types.RawAmount` (`big.Int` внутри, immutable).
   - Сериализация в JSON и БД — каноническая десятичная строка (string), **не** JSON number (JS теряет точность выше $2^{53} - 1$).
   - Для знаковых разниц используется `types.RawDelta`.
3. **Количества**: `types.Quantity` (`int64`, 4 знака после запятой).

### Контракт API (Backend ↔ Frontend)
- Go DTO (`internal/infrastructure/http/v1/dto/`): теги `json:"fieldName,omitempty"`, `camelCase`. Обязательно `Version int `json:"version"`` для optimistic locking.
- Проверка контрактов: `go run ./cmd/http-contracts -check`.
- TypeScript (`frontend/types/`):
  - `MinorUnits` / `RawAmount` → `string`.
  - Арифметика на клиенте — через `bigint` или `decimal.js`.
  - Валидация входных данных — Zod-схемы из `frontend/lib/schemas/`.

---

## 3. Финансовое ядро и Движок проведения (Posting Engine)

Все проводки и регистры координируются через `internal/domain/posting/engine.go`.

### Инварианты регистров и движений
1. **Append-Only Ledger**: движения регистров (`reg_*_movements`) **никогда не обновляются (NO UPDATE) и не удаляются (NO DELETE)**.
2. **Сторно (Reversal)**: отмена проведения документа вызывает `AppendReversalsByRecorder`, добавляя противоположные движения в регистр.
3. **Visitor Pattern**: документ реализует `posting.Postable` и source-интерфейсы. Движения собираются через `RegisterVisitor.CollectMovements(doc)` в `posting.MovementSet`.
4. **Чистые функции**:
   - `Validate(ctx)` — чистая проверка модели в памяти (значения > 0, обязательные поля). Запрещены сетевые и DB-запросы.
   - `GenerateMovements()` — абсолютно детерминированная функция без сайд-эффектов.

### Предотвращение Deadlocks (Resource Ordering)
При блокировке балансов (`SELECT FOR UPDATE` в `posting.PostingValidator`):
- Ресурсы **всегда сортируются по первичному ключу** перед взятием блокировки:
  - `crypto_balance`: сортировка по `(wallet_id, token_id)`
  - `crypto_merchant_balance`: сортировка по `(merchant_id, token_id)`

### Паттерн Debit-First для исходящих операций
- При создании заявки на вывод (`WithdrawalRequest`) списание/резервация с баланса мерчанта происходит **немедленно в момент создания заявки**, до одобрения и отправки в сеть.
- Исполняющий документ (`CryptoWithdrawal`) фиксирует сетевую транзакцию и комиссии, **не создавая повторного дебета мерчанта**.
- Отклонение или подтверждённый отказ заявки компенсирует дебет через отдельный compensation workflow.

---

## 4. Concurrency, Транзакции и Внешние вызовы

### Транзакционная граница (Short DB Transactions)
- **Запрещено выполнять сетевые I/O операции внутри БД-транзакций**: RPC-ноды, HTTP-запросы, вызовы HashiCorp Vault выполняются ДО открытия или ПОСЛЕ коммита транзакции.
- Шаблон для внешних действий:
  ```
  1. DB Tx: Проверить остаток -> Зарезервировать -> Записать намерение (Outbox/Intent) -> Commit
  2. External I/O: Подписать через Vault -> Отправить в сеть / сторонний API
  3. DB Tx: Записать результат / обновить FSM статус -> Commit
  ```

### Безопасность горутин и воркеров (`cmd/worker`)
- Каждая `go func()` обязана принимать `context.Context` и слушать `<-ctx.Done()`.
- Использовать `workerjob.JobRunner` / `tx.RunInTransaction` с таймаутами (statement timeout 30s).
- Воркеры используют distributed leases с fencing tokens для защиты от одновременного исполнения.
- Обязательно вызывать `defer ticker.Stop()` для тикеров.

### Хранение ключей и Vault
- Приватные ключи блокчейна и секреты провайдеров **никогда не хранятся в PostgreSQL** и не логируются.
- Подписание транзакций выполняется через плагины Vault:
  - `internal/infrastructure/crypto/vault_signer` (TRON)
  - `internal/infrastructure/crypto/evm_vault_signer` (EVM)

---

## 5. Webhooks и Идемпотентность

1. **Idempotency-Key**:
   - Любой мутирующий HTTP-запрос (POST/PUT/PATCH/DELETE) должен поддерживать `Idempotency-Key`.
   - На клиенте `frontend/lib/api/core.ts` автоматически генерирует и сохраняет стабильный ключ мутации через `withStableMutationKey`.
   - На бэкенде: проверка уникальности ключа и возврат сохранённого ответа при повторах.
2. **Webhook Dispatcher (`internal/infrastructure/crypto/webhook`)**:
   - Тело подписывается HMAC-SHA256 (`X-Metapus-Signature`).
   - Доставка с exponential backoff retries.
   - Защита от SSRF: адрес назначения валидируется через `urlsafe.ValidatePublicURL` (запрет internal IP, loopback, link-local, принудительный pinning IP).

---

## 6. Фабрики и CRUD (Расширяй, не дублируй)

В Metapus запрещено писать бойлерплейт CRUD с нуля.

### Бэкенд
- **Справочники (Catalogs)**:
  - Репозиторий: встроить `*storage.BaseCatalogRepo[Model]`
  - Сервис: встроить `*domain.CatalogService[Model]`
  - Хэндлер: `handlers.NewCatalogHandler[Model, CreateDTO, UpdateDTO]`
  - Регистрация: реализовать интерфейс `v1.CatalogRegistration` (`internal/infrastructure/http/v1/catalog_factory.go`) и зарегистрировать в `content.RegisterDefaults`.
- **Документы (Documents)**:
  - Репозиторий: встроить `*storage.BaseDocumentRepo[DocModel, LineModel]`
  - Сервис: встроить `*domain.BaseDocumentService[DocModel, LineModel]`
  - Регистрация: реализовать `v1.DocumentRegistration` (`internal/infrastructure/http/v1/document_factory.go`).

### Фронтенд
- **API Клиент (`frontend/lib/api/`)**:
  - Справочник: `export const counterpartyApi = createCatalogApi<ResDTO, CreateDTO, UpdateDTO>("/counterparties", schema, createSchema, updateSchema)`
  - Документ: `export const invoiceApi = createDocumentApi<ResDTO, CreateDTO, UpdateDTO>("/crypto-invoices", schema, createSchema, updateSchema)`
- **UI Страницы**:
  - Списки: использовать `useDocumentListPage` или `CatalogListPage` с `FilterSidebar`.
  - Формы: использовать хуки `useCatalogForm` + `useTabDirty` + `useFormDraft`.
  - Все запросы выполнять через `apiFetch<T>()` или клиент своей HTTP-области из `createScopedClient(scope)`.
  - Стили: компоненты `@/components/ui` (shadcn), токены Tailwind (`cn(...)`), никаких кастомных hex-цветов.

---

## 7. Пошаговый пайплайн реализации фичи

При реализации новой сущности или финансового сценария строго соблюдать последовательность:

```text
1. Миграция      → Редактировать существующую (ранний этап) или добавить cat_/doc_/reg_ миграцию.
                   Обязательно: CDC-колонки (_txid, _deleted_at), version, TIMESTAMPTZ, UUIDv7.
2. Domain Model  → internal/domain/... Модель, методы Validate(ctx), source-интерфейсы движений.
3. Repository    → internal/infrastructure/storage/postgres/... Встроить BaseCatalog/DocumentRepo.
4. Service       → internal/domain/... Бизнес-логика, хуки OnBeforeCreate/Post.
5. DTO & Handler → internal/infrastructure/http/v1/dto/ и handlers/.
6. Registration  → Регистрация фабрики в FactoryRegistry (catalog_factory.go / document_factory.go).
7. Contract Test → go run ./cmd/http-contracts -check.
8. Frontend Types→ frontend/types/... Соответствие Go DTO (версия, camelCase, MinorUnits).
9. Frontend API  → frontend/lib/api/... Вызов createCatalogApi / createDocumentApi.
10. Frontend UI  → frontend/app/(main)/... Страницы списка и формы.
```

---

## 8. Quality Gates (Верификация перед сдачей задачи)

Перед завершением любой работы обязательно выполнить полный цикл проверок:

```bash
# 1. Проверка соответствия API-контрактов Go ↔ TypeScript
go run ./cmd/http-contracts -check

# 2. Сборка и линтинг бэкенда
go build ./...
golangci-lint run ./...
go test ./... -race -count=1

# 3. Проверка типов и линтинг фронтенда
cd frontend
npx tsc --noEmit
npm run lint
```
