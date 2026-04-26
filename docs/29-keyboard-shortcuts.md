# 29 — Keyboard Shortcuts System

> Централизованная система горячих клавиш: реестр, единый обработчик, Help Dialog.

---

## Архитектура

Система горячих клавиш полностью находится на **фронтенде**. Вместо множества разрозненных `addEventListener("keydown")` используется один глобальный listener и Zustand-store для регистрации/отображения shortcuts.

```
frontend/
├── lib/keyboard-utils.ts                     # 1. Утилиты: парсинг комбо, кросс-платформа
├── stores/useShortcutStore.ts                # 2. Zustand-реестр активных shortcuts
├── hooks/useShortcut.ts                      # 3. Декларативный хук регистрации
├── components/layout/shortcut-manager.tsx    # 4. Единый keydown listener (в AppShell)
└── components/layout/shortcut-help-dialog.tsx # 5. Help Dialog (Ctrl+/)
```

### Как работает

```
┌─────────────────────────────────────────────────────┐
│                    ShortcutManager                   │
│   (один window.addEventListener("keydown"))          │
│                                                      │
│   KeyboardEvent → parseCombo → matchEvent            │
│                 → приоритетный Entry → action()       │
└────────────────────┬────────────────────────────────┘
                     │ читает
              ┌──────▼──────┐
              │  Zustand    │
              │  Store      │
              │  Map<id,    │
              │  Entry>     │
              └──────▲──────┘
                     │ register / unregister
           ┌─────────┼─────────┐
           │         │         │
      useShortcut  useShortcut  store.register
      (SiteHeader) (DocList)   (Sidebar)
```

- **ShortcutManager** — рендерится в `AppShell`, слушает `keydown` на `window`.
- **useShortcutStore** — Zustand store (паттерн проекта: `useTabsStore`, `useMetadataStore`).
- **useShortcut** — хук: mount → `register()`, unmount → `unregister()`.
- **ShortcutHelpDialog** — модальное окно по `Ctrl+/`, читает текущие записи из store.

---

## Шаги добавления нового shortcut

> Минимальный набор: **1 строка кода** — вызов `useShortcut` в компоненте.

---

### Шаг 1 — Вызови `useShortcut` в компоненте

```typescript
import { useShortcut } from "@/hooks/useShortcut"

function MyPage() {
    const handleSave = useCallback(() => { /* ... */ }, [])

    useShortcut("editing.save", "mod+s", "Записать", "editing", handleSave)
    // Shortcut автоматически:
    //   ✓ Зарегистрирован при монтировании
    //   ✓ Удалён при размонтировании
    //   ✓ Появляется в Help Dialog (Ctrl+/)
    //   ✓ Обработан единым global listener
}
```

**Всё.** Ничего больше менять не нужно.

---

### Параметры `useShortcut`

```typescript
function useShortcut(
    id: string,           // Уникальный ключ: "nav.close-tab", "editing.save"
    keys: string,         // Комбинация: "mod+s", "f9", "alt+arrowleft", "delete"
    label: string,        // Метка для Help Dialog: "Записать", "Копировать"
    group: ShortcutGroup, // Группа: "navigation" | "editing" | "list" | "general"
    action: () => void,   // Callback
    options?: {
        enabled?: boolean    // Default: true. false = unregister
        priority?: number    // Default: 0. Выше = приоритетнее при конфликте
        passive?: boolean    // Default: false. true = только в Help Dialog, не обрабатывается
    }
): void
```

---

### Формат `keys` (комбинации)

| Пример | Описание |
|--------|----------|
| `"mod+s"` | Ctrl+S (Win/Linux), ⌘S (Mac) |
| `"mod+enter"` | Ctrl+Enter / ⌘Enter |
| `"mod+shift+s"` | Ctrl+Shift+S / ⌘⇧S |
| `"alt+w"` | Alt+W на всех платформах |
| `"f9"` | Функциональная клавиша F9 |
| `"delete"` | Delete / Del |
| `"alt+arrowleft"` | Alt+← |

**Спецификатор `mod`** — кросс-платформенный alias:
- **Windows/Linux** → `Ctrl`
- **macOS** → `⌘ (Cmd)`

Все комбо парсятся через `parseCombo()` из `lib/keyboard-utils.ts`.

---

### Группы (`ShortcutGroup`)

| Ключ | Метка | Назначение |
|------|-------|------------|
| `navigation` | Навигация | Табы, sidebar, навигация |
| `editing` | Редактирование | Save, Post, Undo |
| `list` | Списки | Copy, Delete, Select |
| `general` | Общие | Help Dialog, поиск |

Группы определяют порядок отображения в Help Dialog.

---

## Кросс-платформенность (Win/Mac)

### Обработка клавиш

Все shortcuts сопоставляются по `event.code` (физическая позиция клавиши), что делает их **полностью layout-independent**:

- **Буквенные клавиши** (`a-z`, `0-9`) — `Ctrl+S` корректно работает в **русской раскладке** (физ. клавиша `KeyS`, `e.key = "ы"`).
- **Пунктуация** (`/`, `.`, `,`, `-`, `=`, `` ` ``) — `Ctrl+/` работает в **русской раскладке** (физ. клавиша `Slash`, `e.key = "."`).
- **Специальные клавиши** (`Enter`, `F9`, `Delete`, `ArrowLeft`) — единственная категория, сопоставляемая по `event.key`, т.к. для них нет layout-зависимости.

### Отображение в UI

На Mac: `⌘⇧S`, `⌥←`, `⌃B`
На Windows: `Ctrl+Shift+S`, `Alt+←`, `Ctrl+B`

Функция `formatCombo()` из `lib/keyboard-utils.ts` автоматически выбирает формат.

---

## Фильтрация ввода (Input Suppression)

`ShortcutManager` автоматически подавляет shortcuts, когда фокус находится в текстовом поле:

| Тип shortcut | В `<input>` / `<textarea>` | Вне input |
|-------------|--------------------------|-----------|
| `mod+s` (с модификатором) | ✅ Срабатывает | ✅ Срабатывает |
| `f9` (без модификатора) | ❌ Подавлен | ✅ Срабатывает |
| `delete` (без модификатора) | ❌ Подавлен | ✅ Срабатывает |

**Логика:** если `e.target` — это `INPUT`, `TEXTAREA`, `SELECT` или `[contenteditable]`, и shortcut не содержит модификаторов (Ctrl/Meta/Alt/Shift), то shortcut подавляется.

---

## Приоритеты и конфликты

Если несколько shortcuts зарегистрированы на одну комбинацию, выигрывает entry с **большим `priority`** (по умолчанию 0).

```typescript
// Глобальный Ctrl+S (priority: 0)
useShortcut("global.save", "mod+s", "Записать", "editing", globalSave)

// Специфичный Ctrl+S для формы (priority: 10 — побеждает)
useShortcut("form.save", "mod+s", "Записать форму", "editing", formSave, { priority: 10 })
```

При размонтировании компонента формы `form.save` удаляется, и `global.save` снова начинает обрабатываться.

---

## Help Dialog (Ctrl+/)

Модальный диалог со списком всех **активных** shortcuts. Автоматически обновляется при навигации между страницами.

- **На дашборде:** показывает только Navigation + General shortcuts.
- **На странице списка документов:** добавляются List shortcuts (F9, Delete).
- **На странице формы документа:** добавляются Editing shortcuts (Ctrl+S, Ctrl+Enter).

Диалог использует компоненты `Dialog` из shadcn/ui и `<kbd>` для отображения клавиш.

---

## Регистрация без хука (для UI-примитивов)

Для компонентов, где использование хука затруднено (например, `forwardRef` обёртки), можно использовать прямой доступ к store:

```typescript
import { useShortcutStore } from "@/stores/useShortcutStore"

// Внутри useEffect:
React.useEffect(() => {
    useShortcutStore.getState().register({
        id: "nav.toggle-sidebar",
        keys: "mod+b",
        label: "Боковая панель",
        group: "navigation",
        action: toggleSidebar,
    })
    return () => useShortcutStore.getState().unregister("nav.toggle-sidebar")
}, [toggleSidebar])
```

---

## Текущий реестр shortcuts

### Глобальные (всегда активны)

| Shortcut | ID | Label | Источник |
|----------|-----|-------|----------|
| `Ctrl+/` | `general.help` | Горячие клавиши | `app-shell.tsx` |
| `Ctrl+B` | `nav.toggle-sidebar` | Боковая панель | `sidebar.tsx` |
| `Alt+W` | `nav.close-tab` | Закрыть вкладку | `site-header.tsx` |
| `Ctrl+W` | `nav.close-tab-ctrl` | Закрыть вкладку | `site-header.tsx` |
| `Alt+←` | `nav.prev-tab` | Предыдущая вкладка | `site-header.tsx` |
| `Alt+→` | `nav.next-tab` | Следующая вкладка | `site-header.tsx` |
| `Ctrl+L` | `nav.open-url` | Открыть по ссылке | `site-header.tsx` |

### Контекстные: Списки документов

| Shortcut | ID | Label | Источник |
|----------|-----|-------|----------|
| `F9` | `list.copy` | Копировать | `goods-receipts/page.tsx`, `goods-issues/page.tsx` |
| `Delete` | `list.delete` | Пометить на удаление | `goods-receipts/page.tsx`, `goods-issues/page.tsx` |

### Локальные (не в реестре, scoped)

| Shortcut | Компонент | Описание |
|----------|-----------|----------|
| `Ctrl+C` | `report-page.tsx` | Копирование ячеек таблицы |
| `Enter`, `+`, `-`, `↑`, `↓` | `product-picker-dialog.tsx` | Управление количеством и навигация |
| `Escape` | `section-panel.tsx` | Закрытие панели |

---

## Anti-patterns (чего избегать)

| ❌ Не делай | ✅ Делай |
|-------------|----------|
| `document.addEventListener("keydown", ...)` в компоненте | `useShortcut("my.id", "mod+s", ...)` |
| Хардкод `e.key === "s"` для русской раскладки | `parseCombo` + `matchEvent` (использует `e.code`) |
| Дублирование F9 handler в каждом document list page | Один `useShortcut("list.copy", ...)` — вынеси в shared hook |
| `if (navigator.platform.includes("Mac"))` | `isMac()` из `lib/keyboard-utils.ts` |

---

## Полный пример: добавление Ctrl+S в форму документа

```typescript
// frontend/hooks/useCatalogForm.ts (или конкретный page component)

import { useShortcut } from "@/hooks/useShortcut"

function MyDocumentForm() {
    const handleSave = useCallback(async () => {
        if (!isDirty) return
        await api.goodsReceipts.update(id, mapToUpdate(form))
        toast.success("Сохранено")
    }, [isDirty, id, form])

    const handlePost = useCallback(async () => {
        await api.goodsReceipts.post(id)
        toast.success("Проведено")
    }, [id])

    // Shortcuts — автоматически в Help Dialog
    useShortcut("editing.save", "mod+s", "Записать", "editing", handleSave)
    useShortcut("editing.post", "mod+enter", "Провести", "editing", handlePost)

    return (/* ... */)
}
```

---

## Checklist

```
[ ] 1. useShortcut вызван с уникальным id
       — Формат: "группа.действие" (e.g. "editing.save")
       — id не конфликтует с существующими (см. таблицу выше)

[ ] 2. keys использует mod вместо ctrl/meta
       — "mod+s", не "ctrl+s" (для кросс-платформенности)

[ ] 3. action обёрнут в useCallback
       — Иначе бесконечные re-registration

[ ] 4. group выбран правильно
       — navigation / editing / list / general

[ ] 5. Проверка
       — npx tsc --noEmit
       — Ctrl+/ показывает новый shortcut в Help Dialog
       — Shortcut работает (в т.ч. в русской раскладке для буквенных клавиш)
       — Shortcut не срабатывает в input-полях (для одиночных клавиш)
```

---

## FAQ

**Q: Как зарегистрировать shortcut, который виден в Help Dialog, но обрабатывается локально?**

Используй `passive: true`. Shortcut появится в Help Dialog, но `ShortcutManager` не будет его обрабатывать — ваш локальный `onKeyDown` handler остаётся ответственным.

```typescript
useShortcut("picker.enter", "enter", "Добавить товар", "editing", noop, { passive: true })
```

---

**Q: Почему `mod` вместо `ctrl`?**

`mod` — платформо-адаптивный alias. На Windows `mod+s` → `Ctrl+S`, на macOS → `⌘S`. Это стандартный подход (VS Code, GitHub, Figma). Если нужен именно `Ctrl` на всех платформах — используй `"ctrl+s"`.

---

**Q: Shortcut конфликтует с браузерным (Ctrl+W)?**

Браузер перехватывает `Ctrl+W` до JavaScript — это нельзя предотвратить. Поэтому для закрытия вкладки используем **два** shortcut: `Alt+W` (основной, надёжный) и `Ctrl+W` (best-effort).

---

**Q: Как посмотреть все зарегистрированные shortcuts в DevTools?**

В консоли браузера:

```javascript
// Все записи
window.__ZUSTAND_STORE = require("@/stores/useShortcutStore")
useShortcutStore.getState().getAll()

// Или проще — нажми Ctrl+/ в приложении
```

---

## Связанные документы

- [19-dashboard-widgets.md](19-dashboard-widgets.md) — аналогичный паттерн Zustand-реестра
- [03-project-structure.md](03-project-structure.md) — расположение файлов
- [16-development-rules.md](16-development-rules.md) — правила разработки
