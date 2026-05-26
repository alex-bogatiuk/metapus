# Задание 3. Проектирование и начальная разработка интерфейса

Раздел содержит описание структуры интерфейса, верстки экранов и клиентской логики для ERP-системы **Metapus**.

---

## 1. Структура веб-приложения

Интерфейс построен по принципу одностраничного приложения (SPA) на базе фреймворка **Next.js App Router**. Структура включает следующие разделы:

1. **Экран авторизации (`/login`):** форма аутентификации пользователей с разделением по идентификаторам тенантов.
2. **Панель мониторинга (Dashboard, `/`):** главная страница со сводными показателями работы (KPI), графиками продаж и статусом активного тенанта.
3. **Реестры справочников (`/catalogs`):** интерфейс отображения списков (Контрагенты, Номенклатура) с поддержкой поиска и фильтрации.
4. **Форма документа (`/documents/[id]`):** многовкладочный интерфейс для ввода данных, редактирования табличных частей и проведения документов.

---

## 2. Дизайн-макеты интерфейса

Интерфейс спроектирован в темной цветовой гамме с применением эффекта матового стекла (glassmorphism) для визуального разделения блоков и фиолетово-синей палитры для выделения активных элементов управления.

### 2.1. Главный экран (Dashboard)
Содержит сводные отчеты, быстрые ссылки на часто используемые функции.
![Dashboard Mockup](./dashboard_mockup.png)

### 2.2. Страница документов (Document List)
Отображает таблицу документов с боковой панелью фильтрации параметров, формируемой на основе метаданных.
![Document List Mockup](./document_list_mockup.png)

### 2.3. Форма создания и проведения документа (Document Form)
Интерактивная форма ввода товарных позиций в табличную часть с кнопками сохранения и проведения документа.
![Document Form Mockup](./document_form_mockup.png)

---

## 3. HTML-верстка и семантическая разметка

Интерфейс сверстан с использованием семантических тегов стандарта **HTML5** и утилитарных классов **Tailwind CSS**.

### Семантическая структура:
- `<aside>` — боковая панель навигации по разделам ERP.
- `<header>` — верхняя панель (поиск, индикатор тенанта, профиль пользователя).
- `<main>` — контейнер основного содержимого активной страницы.
- `<section>` — логические блоки страниц (виджеты дашборда, группы полей формы).
- `<nav>` — элементы навигации и переключатели вкладок.

### Адаптивность:
Макет адаптирован под различные разрешения экранов с помощью встроенных брейкпоинтов Tailwind CSS (`sm`, `md`, `lg`):
- **Сетка (Grid Layout):** количество колонок на дашборде динамически меняется от 1 (на мобильных устройствах, `grid-cols-1`) до 4 (на десктопах, `lg:grid-cols-4`).
- **Скрытие элементов:** боковая панель навигации скрывается на малых экранах (`hidden lg:flex`) и вызывается через мобильное меню.
- **Таблицы:** для табличных данных активирована горизонтальная прокрутка (`overflow-x-auto`) при просмотре на узких экранах для предотвращения искажения верстки.

---

## 4. Клиентская логика (JavaScript / TypeScript)

Прикладная логика на стороне клиента обеспечивает проверку вводимых данных, управление глобальным состоянием интерфейса и защиту от потери изменений.

### 4.1. Валидация форм (React Hook Form + Zod)
Для проверки корректности данных перед отправкой на сервер используется декларативная валидация. Схема описывается средствами библиотеки **Zod**, управление состоянием полей возложено на **React Hook Form**.

*Пример кода валидации карточки товара:*
```typescript
// frontend/components/catalogs/product-form.tsx
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import * as z from "zod"

// Определение схемы валидации
export const ProductSchema = z.object({
  code: z.string()
    .min(3, "Код должен содержать не менее 3 символов")
    .max(20, "Максимальная длина кода — 20 символов"),
  name: z.string()
    .min(1, "Наименование обязательно для заполнения"),
  price: z.number()
    .positive("Цена должна быть больше нуля")
    .or(z.string().transform((val) => Number(val))),
  category: z.string().uuid("Некорректный формат UUID категории"),
})

export type ProductFormValues = z.infer<typeof ProductSchema>

export function ProductForm({ onSubmit }) {
  const { register, handleSubmit, formState: { errors } } = useForm<ProductFormValues>({
    resolver: zodResolver(ProductSchema),
    defaultValues: { code: "", name: "", price: 0, category: "" }
  })

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
      <div>
        <label className="text-sm font-medium">Код товара</label>
        <input {...register("code")} className="w-full p-2 bg-slate-900 border rounded" />
        {errors.code && <p className="text-red-500 text-xs mt-1">{errors.code.message}</p>}
      </div>

      <div>
        <label className="text-sm font-medium">Наименование</label>
        <input {...register("name")} className="w-full p-2 bg-slate-900 border rounded" />
        {errors.name && <p className="text-red-500 text-xs mt-1">{errors.name.message}</p>}
      </div>

      <button type="submit" className="px-4 py-2 bg-purple-600 rounded text-white hover:bg-purple-700">
        Сохранить
      </button>
    </form>
  )
}
```

### 4.2. Состояние вкладок и предотвращение потери данных (Zustand)
Для сохранения данных при переключении между вкладками разработан глобальный стейт-менеджер на базе **Zustand**, который отслеживает статус изменения данных в открытых формах (`dirty-state`).

*Код хука отслеживания изменений формы (`useTabDirty.ts`):*
```typescript
// frontend/hooks/useTabDirty.ts
import { useCallback } from "react"
import { usePathname } from "next/navigation"
import { useTabsStore } from "@/stores/useTabsStore"

export function useTabDirty() {
    const pathname = usePathname()
    const { setTabDirty, tabs } = useTabsStore()

    // Поиск активной вкладки по текущему пути URL
    const currentTab = tabs.find((t) => t.id === pathname)
    const tabId = currentTab?.id

    const markDirty = useCallback(() => {
        if (tabId) {
            setTabDirty(tabId, true)
        }
    }, [tabId, setTabDirty])

    const markClean = useCallback(() => {
        if (tabId) {
            setTabDirty(tabId, false)
        }
    }, [tabId, setTabDirty])

    return { markDirty, markClean, isDirty: currentTab?.isDirty ?? false }
}
```

Если свойство `isDirty` имеет значение `true`, система блокирует закрытие вкладки и выводит предупреждающее окно `AlertDialog` для подтверждения действия пользователем.

---

## 5. Репозиторий и развертывание проекта

### Структура каталогов проекта:
```text
metapus/
├── cmd/                  # Точки входа (server, worker, tenant)
├── internal/             # Бизнес-логика бэкенда (Clean Architecture)
├── db/                   # Скрипты миграций СУБД (PostgreSQL)
└── frontend/             # Next.js 16 веб-приложение
    ├── app/              # Маршрутизация страниц (App Router)
    ├── components/       # Набор UI-компонентов
    ├── hooks/            # Реализация React-хуков
    ├── stores/           # Хранилища состояния Zustand
    └── types/            # Схемы типов TypeScript
```

### Руководство по развертыванию:

Для запуска ERP-системы Metapus в локальном окружении выполните следующие команды:

1. **Запуск контейнера базы данных (PostgreSQL):**
   ```bash
   docker compose up postgres -d
   ```

2. **Инициализация БД тенанта и запуск миграций бэкенда:**
   ```bash
   export META_DATABASE_URL="postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable"
   export TENANT_DB_USER="metapus"
   export TENANT_DB_PASSWORD="metapus"
   
   go run cmd/tenant/main.go create --slug=default --name="Dev"
   go run cmd/tenant/main.go migrate
   ```

3. **Запуск API-сервера (порт :8080):**
   ```bash
   go run ./cmd/server
   ```

4. **Запуск веб-интерфейса Next.js (порт :3000):**
   ```bash
   cd frontend
   npm install
   npm run dev
   ```

Вход в систему: `admin@metapus.io` / `Admin123!`.
