# Metapus Database Migrations

> Все SQL-миграции для tenant-баз данных. Управляются [goose](https://github.com/pressly/goose).

---

## Конвенция нумерации

| Диапазон | Назначение | Пример |
|----------|------------|--------|
| `00001–09999` | **Core** — миграции ядра Metapus | `00001_initial.sql`, `00020_doc_basis_fields.sql` |
| `10000+` | **Extensions** — миграции расширений клиента | `10001_cat_vehicles.sql` |

### Почему это важно

- Goose использует **единую таблицу** `goose_db_version` для всех миграций
- Core и extension миграции применяются отдельными `goose.Provider`, но в **общую таблицу**
- Непересекающиеся диапазоны гарантируют отсутствие конфликтов версий

---

## Правила написания миграций

### 1. Формат файла

```sql
-- +goose Up
CREATE TABLE cat_new_entity (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- ... columns ...
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ
);

-- +goose Down
DROP TABLE IF EXISTS cat_new_entity;
```

### 2. CDC-колонки (обязательно)

Каждая бизнес-таблица **ОБЯЗАНА** содержать:
- `_txid BIGINT NOT NULL DEFAULT txid_current()` — ID транзакции для Change Data Capture
- `_deleted_at TIMESTAMPTZ` — soft delete (NULL = активна)

### 3. Именование таблиц

| Тип | Префикс | Пример |
|-----|---------|--------|
| Справочник | `cat_` | `cat_contractors`, `cat_items` |
| Документ | `doc_` | `doc_goods_receipts` |
| Регистр | `reg_` | `reg_stock` |
| Системная | `sys_` | `sys_sequences` |

### 4. `NO TRANSACTION` для индексов CONCURRENTLY

```sql
-- +goose Up
-- +goose NO TRANSACTION
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cat_items_code ON cat_items (code);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS idx_cat_items_code;
```

> ⚠️ `CREATE INDEX CONCURRENTLY` нельзя выполнять внутри транзакции. Goose поддерживает `-- +goose NO TRANSACTION` для таких миграций.

### 5. Не редактировать опубликованные миграции

- Если миграция уже применена в production, **создавайте новую миграцию** для изменений
- В dev-окружении можно откатить и поправить, но в production — только forward-only

### 6. Идемпотентность

Используйте `IF NOT EXISTS` / `IF EXISTS` для безопасного повторного применения:
```sql
CREATE TABLE IF NOT EXISTS cat_example (...);
ALTER TABLE cat_example ADD COLUMN IF NOT EXISTS new_column TEXT;
```

---

## Как применить миграции

### Из CLI (dev/ops)

```bash
# Все тенанты
go run cmd/tenant/main.go migrate

# Конкретный тенант
go run cmd/tenant/main.go migrate --id <tenant-uuid>
```

### Из UI (admin)

Настройки → Тенанты → кнопка «Обновить БД»

### Программно (goose-as-library)

```go
import "metapus/internal/infrastructure/storage/postgres/migration"

output, err := migration.RunAll(dsn)
```

---

## Связанные документы

- [15-naming-conventions.md](../docs/15-naming-conventions.md) — полные правила именования
- [16-development-rules.md](../docs/16-development-rules.md) — правила разработки
- [17-migration-status.md](../docs/17-migration-status.md) — статус миграций и env variables
