---
trigger: always_on
---

# Metapus — Глобальное правило

Ты работаешь над **Metapus** — мультитенантной ERP-платформой.
**Стек:** Go 1.24 (Clean Architecture, pgx, squirrel, Gin) + Next.js 16 (App Router, TypeScript, shadcn/ui, Zustand, Zod).

---

## §1. Перед тем как писать код — ОСТАНОВИСЬ

1. **Прочитай `docs/ROUTER.md`** → найди релевантные документы архитектуры.
2. **Найди ближайший аналог** в кодовой базе — в обоих слоях (backend + frontend).
3. **Можно ли решить задачу расширением существующей абстракции?** Если да — расширяй, не создавай новое.
4. **Если нет аналога** — спроектируй решение как generic и reusable, не под одну страницу/сущность.

> Самый частый источник плохого кода — начать писать, не изучив, что уже есть.

---

## §2. Архитектурные инварианты (нарушение = критическая ошибка)

| # | Инвариант | Почему |
|---|-----------|--------|
| 1 | **`core` → `domain` → `infrastructure`**. Domain НЕ импортирует infrastructure. | Clean Architecture |
| 2 | **Нет `tenant_id`** в бизнес-таблицах. `TxManager` из `context.Context`. | Database-per-Tenant |
| 3 | **Нет `any`/`unknown`** в TypeScript API-типах. Нет `interface{}`/`any` в Go, где можно generic. | Type Safety |
| 4 | **Нет хардкода валютного scale** (100, 1000). Только `decimalPlaces` из метаданных. | Финансовая точность |
| 5 | **Handler — тонкий адаптер.** `c.Error(err) + c.Abort()`, никогда `c.JSON(status, error)`. | Единая точка ошибок |
| 6 | **Все ошибки** через `apperror.AppError`. Wrapping: `fmt.Errorf("action: %w", err)`. | Трассируемость |
| 7 | **`Validate(ctx)`** — чистая функция, без обращений к БД. | Тестируемость |
| 8 | **`GenerateMovements()`** — детерминированная, без side effects. | Воспроизводимость |
| 9 | **TypeScript types === Go DTO** (поля, имена, nullability). | Contract Integrity |
| 10 | **Enum `iota + 1`** для бизнес-значений. Нулевое значение = «не задано». `iota` без `+1` — только для `ctxKey` и sentinel. | Zero-value Safety |
| 11 | **Ошибку обработай один раз.** Либо `log` + swallow, либо `wrap` + return. Никогда `log` + return одновременно. | Трассируемость |
| 12 | **Слайсы на границах — defensive copy.** Принимаешь `[]T` в domain — `copy()`. Возвращаешь внутренний `[]T` — `copy()`. | Иммутабельность |

---

## §3. Go: качество и производительность

- **Capacity hints**: `make([]T, 0, n)` если размер известен или оценим. Единая точка: generic `List()` в `BaseCatalogRepo[T]`.
- **`strconv` > `fmt`** для числовых конвертаций в hot paths (`strconv.Itoa`, `strconv.FormatInt`). `fmt.Sprintf` допустим для шаблонов с текстом.
- **Functional Options** (`func NewX(required, opts ...Option)`) — для новых конструкторов с ≥3 необязательными параметрами. Не переписывать существующее ретроактивно.
- **`_` префикс** для неэкспортируемых package-level `const`/`var` (`_defaultBatchSize`, `_maxRetries`).
- **Table-driven тесты**: `tests := []struct{...}`, переменная `tt`, поля `give`/`want`. Единая конвенция для всех тестов.
- **Импорты**: 3 группы — `stdlib` → `external` → `internal`.

---

## §4. Паттерны: расширяй, не дублируй

### Backend
- **CRUD**: embed `BaseCatalogRepo[T]`, `CatalogService[T]`, `BaseDocumentService[T, L]`. Запрещено писать Create/Update/GetByID/List с нуля.
- **Миграции** (ранний этап): редактируй оригинальную, не создавай новую. Префиксы `cat_`/`doc_`/`reg_`/`sys_`/`auth_`. CDC-колонки + триггеры + `TIMESTAMPTZ`.
- **Транзакции**: Optimistic Locking (version) по умолчанию. `FOR UPDATE` + resource ordering — только для остатков. Короткие tx, statement_timeout 30s.
- **Goroutines**: каждая `go func()` → owner + `ctx.Done()` + stop-signal. `defer ticker.Stop()`.

### Frontend
- **API**: `createCatalogApi<>()` / `createDocumentApi<>()` — одна строка на сущность.
- **Списки**: `CatalogListPage` / `useDocumentListPage` + `FilterSidebar` (metadata-driven).
- **Формы**: `useCatalogForm` + `useTabDirty` + `useFormDraft`. Все запросы через `apiFetch<T>()`.
- **Стиль**: только shadcn/ui, только Tailwind tokens (`cn()`), никаких hex-цветов и custom primitives.
- **State**: URL-driven (списки), Zustand (auth/tabs/metadata), useState (UI).

---

## §5. Формат вывода

1. **Начни с решения.** Какие паттерны применяешь, какие файлы затронуты.
2. **Покажи только уникальное.** НЕ генерируй портянки стандартного boilerplate — покажи модель, валидацию, хуки, строки регистрации.
3. **Код** — первая строка блока = полный путь к файлу.
4. **Trade-offs** — если есть выбор, обоснуй.
5. **Verify** — в конце напомни:
   ```
   go build ./... && golangci-lint run ./...
   cd frontend && npx tsc --noEmit && npm run lint
   ```

---

## §6. Запрещено (Anti-patterns)

- ❌ Копипаста CRUD/UI между сущностями — используй generics и фабрики
- ❌ Бизнес-логика в handler — только в domain service
- ❌ Сырой state в React-компоненте — выноси в хук
- ❌ `go func()` без контекста — goroutine leak
- ❌ `SELECT FOR UPDATE` без сортировки — deadlock
- ❌ Новая миграция для правки существующей таблицы (ранний этап) — редактируй оригинал
- ❌ Hex-цвета в Tailwind — только design tokens
- ❌ «Быстрый хак» вместо системного решения — мы строим на десятилетия
- ❌ `log.Error(err)` + `return err` — двойная обработка ошибки. Выбери одно
- ❌ `var items []T` + `append` в цикле при известном размере — используй `make([]T, 0, n)`
- ❌ `fmt.Sprintf("%d", n)` в hot path — используй `strconv`
- ❌ `iota` (без `+1`) для бизнес-enum, где 0 не осмыслен — zero-value ловушка
- ❌ Сохранение чужого `[]T` без `copy()` в domain-модели — caller может мутировать