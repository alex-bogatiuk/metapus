# Курсы Metapus

Структурированные обучающие материалы по архитектуре и внутреннему устройству Metapus.

---

## Курс 1: Архитектура Go-бэкенда

Фундамент: от запуска программы до паттернов проектирования и обработки ошибок.

| # | Модуль | Файл |
|---|--------|------|
| 1 | [Точки входа и Жизненный цикл](go-architecture/01-entry-points.md) | DI, Middleware, Gin |
| 2 | [Clean Architecture](go-architecture/02-clean-architecture.md) | core → domain → infrastructure |
| 3 | [Generics в Go 1.24](go-architecture/03-generics.md) | BaseCatalogRepo[T], Constraints |
| 4 | [Базы Данных и Multi-Tenancy](go-architecture/04-databases-multitenancy.md) | Database-per-Tenant, TxManager |
| 5 | [Дизайн-паттерны](go-architecture/05-design-patterns.md) | Visitor, Factory, Functional Options |
| 6 | [Обработка Ошибок и Валидация](go-architecture/06-error-handling.md) | AppError, wrapping, чистая валидация |

---

## Курс 2: Криптопроцессинг — от Инвойса до Движений

Полный путь крипто-платежа — от HTTP-запроса мерчанта до финальных движений в регистрах.

| # | Модуль | Файл |
|---|--------|------|
| 1 | [Архитектура Worker'а](crypto-processing/01-worker-architecture.md) | Фоновые задания, MultiTenantWorker |
| 2 | [Go-конкурентность](crypto-processing/02-go-concurrency.md) | Goroutines, Channels, Select |
| 3 | [Merchant API](crypto-processing/03-merchant-api.md) | Создание инвойса, Wallet Pool |
| 4 | [ChainWatcher](crypto-processing/04-chain-watcher.md) | Наблюдатель блокчейна TRON |
| 5 | [EventProcessor](crypto-processing/05-event-processor.md) | Мозг криптопроцессинга |
| 6 | [FSM](crypto-processing/06-payment-fsm.md) | Конечный автомат платежей |
| 7 | [Posting Engine](crypto-processing/07-posting-engine.md) | Движения в регистрах |
| 8 | [Sweep](crypto-processing/08-sweep.md) | Сбор средств с пул-кошельков |
| 9 | [CryptoAmount](crypto-processing/09-crypto-amount.md) | Типобезопасные деньги (int64) |
| 10 | [Тестирование](crypto-processing/10-testing.md) | Table-driven тесты, mock'и, E2E |

### Схема полного цикла крипто-платежа

```
POST /invoices → Lease Wallet → TRON Watcher polls blockchain
  → BlockchainEvent → Channel → EventProcessor
    → Guard → Idempotency → Match Wallet → Create Payment (Fee Snapshot)
      → FSM: Detected → Confirming → Confirmed
        → PostingEngine: 3 регистра (Balance, Fee, MerchantBalance)
          → Sweep Evaluation → MarkSweepPending → On-chain TX
```
