# Frontend Guidelines

> **TL;DR:** Адаптация Clean Architecture под React/Next.js. Strict TypeScript, URL-driven state, UI только на базе shadcn/ui.

> **Тип:** Reference
> **Аудитория:** Developer
> **Связанные:** [03-project-structure.md](../guide/03-project-structure.md)

---

## 1. Фундаментальные принципы

### 1.1 Code is Metadata
Пользовательский интерфейс строится на строгих контрактах с бекендом.
- **Type Safety**: Запрещено использовать `any` или `unknown`. 
- **Contract Integrity**: Если бекенд возвращает DTO `InvoiceResponse`, фронтенд обязан иметь идентичный TypeScript `interface` с теми же полями и правилами nullability.

### 1.2 URL-driven State
Избегайте скрытого состояния для навигации.
- Сортировки, пагинация, значения фильтров в списках, открытые табы — всё это должно быть отражено в URL-параметрах. 
- Пользователь должен иметь возможность скопировать URL и отправить коллеге, который увидит ровно ту же выборку.

### 1.3 UI Kit Restriction
> [!IMPORTANT]
> Весь UI строится исключительно на базе Tailwind CSS + **shadcn/ui**. 

- Запрещено писать базовые примитивы (Button, Input, Select, Dialog) с нуля.
- Запрещено использовать хардкодные HEX-цвета. Все цвета берутся из Tailwind токенов (например, `bg-background`, `text-muted-foreground`).

## 2. Архитектура файлов (Feature-Sliced/Vertical)

Код разделяется по функциональным "срезам", а не по типу файлов.

```path
frontend/app/
├── catalogs/          # Срез: Справочники
│   └── [type]/page.tsx
├── documents/         # Срез: Документы
```

1. **Page Components (`page.tsx`)**: Серверные компоненты. Делают начальный fetch, прокидывают данные вниз, метаданные для SEO/Табов.
2. **Feature Components**: Клиентские формы, таблицы. Лежат рядом со срезом или в `components/shared/`.
3. **UI Components**: Только в `components/ui/`. Ничего не знают о бизнесе (DUMB components).

## 3. Обработка данных

- Все HTTP-запросы должны проходить через централизованную утилиту (`apiFetch`), которая добавляет `X-Tenant-ID` и обрабатывает 401/403.
- Для форм используется единый хук `useFormDraft` (для автосохранения) и интеграция с React Hook Form + Zod.
- Если загрузка долгая — используйте Skeleton-паттерн (`DataTableSkeleton`), а не просто крутящийся спиннер.
