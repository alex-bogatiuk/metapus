---
trigger: always_on
---

# Metapus — Глобальное правило

Ты работаешь над **Metapus** — мультитенантной ERP-платформой.
**Стек:** Go 1.27 (Clean Architecture, pgx, squirrel, Gin) + Next.js 16 (App Router, TypeScript, shadcn/ui, Zustand, Zod).

**Архитектура бэкенда** — слои, граница «платформа / конфигурация / расширение», `pkg/sdk`, тенанты, транзакции, проведение, хуки, миграции, ошибки, деньги и паттерны проектирования — в скилле `metapus-architecture` (`.agents/skills/metapus-architecture/`). Загрузи его перед любой работой с Go-кодом в `internal/`, `pkg/`, `extensions/`, `cmd/`. Ниже — только то, чего в скилле нет.

---

## §1. Инварианты (нарушение = ошибка)

| # | Инвариант | Почему |
|---|-----------|--------|
| 1 | **Нет `any`/`unknown`** в TypeScript API-типах. | Type Safety |
| 2 | **TypeScript types === Go DTO** (поля, имена, nullability). Типы генерируются `go run ./cmd/http-contracts`, руками не правятся. | Contract Integrity |
| 3 | **Enum `iota + 1`** для бизнес-значений. Нулевое значение = «не задано». `iota` без `+1` — только для `ctxKey` и sentinel. | Zero-value Safety |
| 4 | **Слайсы на границах — defensive copy.** Принимаешь `[]T` в domain — `copy()`. Возвращаешь внутренний `[]T` — `copy()`. | Иммутабельность |

---

## §2. Go: качество и производительность

- **Capacity hints**: `make([]T, 0, n)` если размер известен или оценим. Единая точка: generic `List()` в `BaseCatalogRepo[T]`.
- **`strconv` > `fmt`** для числовых конвертаций в hot paths (`strconv.Itoa`, `strconv.FormatInt`). `fmt.Sprintf` допустим для шаблонов с текстом.
- **`_` префикс** для неэкспортируемых package-level `const`/`var` (`_defaultBatchSize`, `_maxRetries`).
- **Table-driven тесты**: `tests := []struct{...}`, переменная `tt`, поля `give`/`want`. Единая конвенция для всех тестов.
- **Импорты**: 3 группы — `stdlib` → `external` → `internal`.

---

## §3. Frontend: расширяй, не дублируй

- **API**: `createCatalogApi<>()` / `createDocumentApi<>()` — одна строка на сущность.
- **Списки**: `CatalogListPage` / `useDocumentListPage` + `FilterSidebar` (metadata-driven).
- **Формы**: `useCatalogForm` + `useTabDirty` + `useFormDraft`. Все запросы через `apiFetch<T>()`.
- **Стиль**: только shadcn/ui, только Tailwind tokens (`cn()`), никаких hex-цветов и custom primitives.
- **State**: URL-driven (списки), Zustand (auth/tabs/metadata), useState (UI).

---

## §4. Формат вывода

1. **Начни с решения.** Какие паттерны применяешь, какие файлы затронуты.
2. **Покажи только уникальное.** НЕ генерируй портянки стандартного boilerplate — покажи модель, валидацию, хуки, строки регистрации.
3. **Код** — первая строка блока = полный путь к файлу.
4. **Trade-offs** — если есть выбор, обоснуй.
5. **Verify** — в конце напомни проверки: для Go — из скилла `metapus-architecture` (раздел 6), для фронтенда:
   ```
   cd frontend && npx tsc --noEmit && npm run lint
   ```

---

## §5. Запрещено (Anti-patterns)

- ❌ Копипаста UI между сущностями — используй generics и фабрики
- ❌ Сырой state в React-компоненте — выноси в хук
- ❌ Hex-цвета в Tailwind — только design tokens
- ❌ «Быстрый хак» вместо системного решения — мы строим на десятилетия
- ❌ `var items []T` + `append` в цикле при известном размере — используй `make([]T, 0, n)`
- ❌ `fmt.Sprintf("%d", n)` в hot path — используй `strconv`
- ❌ `iota` (без `+1`) для бизнес-enum, где 0 не осмыслен — zero-value ловушка
- ❌ Сохранение чужого `[]T` без `copy()` в domain-модели — caller может мутировать
