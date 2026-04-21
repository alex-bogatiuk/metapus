Проанализировав весь фронтенд Metapus, вот мои рекомендации по UI-элементам и фичам, которые стоит рассмотреть. Группирую по приоритету.

---

## 1. Command Palette / Глобальный поиск (Ctrl+K)

```
🎨 UX Insight: Command Palette / Global Search
┌─────────────────────────────────────────────────────
│ 1С:      "Перейти по ссылке" + поиск по меню (Ctrl+Alt+P).
│          Поиск по всем объектам через "Поиск данных".
│ Fiori:   Shell Bar Search — unified search across
│          business objects, apps, recent items.
│          Fiori Launchpad: type-ahead по всем приложениям.
│ ERPNext: Awesome Bar (Ctrl+K) — поиск по DocTypes,
│          записям, действиям, навигации. Killer feature.
│ Odoo:    Command Palette (Ctrl+K в 17+) — поиск по
│          меню, записям, действиям.
│ ─────────────────────────────────────────────────────
│ Лучшее:  ERPNext Awesome Bar — самый зрелый, ищет
│          по всему: навигация, записи, действия.
│ Metapus: Есть OpenUrlPopover (Ctrl+L) — но это только
│          ввод URL. Нужен полноценный Command Palette:
│          - Ctrl+K → поиск по навигации (из metadata)
│          - Поиск по записям (контрагенты, документы)
│          - Быстрые действия ("Создать поступление")
│          - Недавние страницы (из useTabsStore)
│          Компонент: cmdk (уже есть Command из shadcn/ui)
└─────────────────────────────────────────────────────
```

В Metapus уже есть `Command` из shadcn/ui и `OpenUrlPopover` с Ctrl+L, но это лишь ввод URL. open-url-popover.tsx:27-33

Metadata store (`useMetadataStore`) уже содержит все сущности с `presentation` и `routePrefix` — идеальная основа для Command Palette. app-sidebar.tsx:189-213

---

## 2. Централизованная система горячих клавиш + Help Dialog

```
🎨 UX Insight: Keyboard Shortcuts Registry
┌─────────────────────────────────────────────────────
│ 1С:      F9 = Провести, Ctrl+S = Записать,
│          Ctrl+Shift+P = Провести и закрыть.
│          Справка по клавишам через F1.
│ Fiori:   Keyboard Shortcuts Dialog (Ctrl+Shift+?),
│          стандартизированные по Fiori Design Guidelines.
│ ERPNext: Ctrl+S, Ctrl+Enter (submit), Alt+S (save),
│          Ctrl+Shift+? для справки.
│ Odoo:    Alt+S (save), Alt+J (discard), Alt+N (new).
│          Нет единого справочника.
│ ─────────────────────────────────────────────────────
│ Лучшее:  SAP Fiori — стандартизированный диалог
│          со всеми shortcuts, контекстно-зависимый.
│ Metapus: Горячие клавиши разбросаны по компонентам
│          (ProductPickerDialog, DataToolbar F9=Copy).
│          Нужен:
│          1) useShortcutRegistry — централизованный хук
│          2) ShortcutHelpDialog (Ctrl+?) — список всех
│             активных shortcuts на текущей странице
│          3) Стандарт: Ctrl+S=Записать, Ctrl+Enter=
│             Провести, F9=Копировать, Ctrl+K=Поиск
└─────────────────────────────────────────────────────
```

Сейчас keyboard hints есть только в `ProductPickerDialog` (Enter/+/- для количества), а F9=Копировать упоминается в tooltip `DataToolbar`: product-picker-dialog.tsx:529-541 data-toolbar.tsx:61-77

---

## 3. Режим "Inline Editing" в списках (Editable List View)

```
🎨 UX Insight: Inline Editing in List View
┌─────────────────────────────────────────────────────
│ 1С:      Нет inline editing в динамическом списке
│          (только просмотр, редактирование через форму).
│ Fiori:   Responsive Table — inline edit mode,
│          Draft Handling для каждой строки.
│ ERPNext: Editable Grid в Child Tables (не в List View).
│ Odoo:    Editable List View — клик на ячейку →
│          inline edit → автосохранение. Killer feature
│          для массового ввода данных.
│ ─────────────────────────────────────────────────────
│ Лучшее:  Odoo — inline editing в списке справочников
│          (быстрое редактирование цен, названий).
│ Metapus: DataTable сейчас read-only. Для справочников
│          типа "Единицы измерения", "Ставки НДС" —
│          inline editing сэкономит массу кликов.
│          Не для документов (слишком сложная форма),
│          а для простых справочников.
│          Реализация: Column<T>.editable?: boolean,
│          onCellEdit callback в DataTable.
└─────────────────────────────────────────────────────
```

Текущий `DataTable` поддерживает только просмотр — `onRowClick` и `onRowDoubleClick` для навигации: [1-cite-5](#1-cite-5) 

---

## 4. Toast с действием "Отменить" (Undo Action)

```
🎨 UX Insight: Undo Action in Notifications
┌─────────────────────────────────────────────────────
│ 1С:      Нет undo в уведомлениях. Только "Отменить
│          проведение" как отдельное действие.
│ Fiori:   MessageToast с action button — "Undo"
│          после удаления/перемещения.
│ ERPNext: Нет undo в toast.
│ Odoo:    Нет undo в toast (есть "Отменить" в формах).
│ ─────────────────────────────────────────────────────
│ Лучшее:  SAP Fiori + Gmail pattern — toast с кнопкой
│          "Отменить" после деструктивных действий.
│ Metapus: sonner (toast) уже поддерживает action buttons.
│          После "Помечено на удаление: 5" → кнопка
│          "Отменить" в toast, которая вызывает
│          clearDeletionMark для тех же ID.
│          Аналогично для "Отменено проведение".
└─────────────────────────────────────────────────────
```

`useDocumentBatchActions` уже показывает toast после операций, но без undo: useDocumentBatchActions.ts:145-155

---

## 5. Activity Log / Timeline на форме документа

```
🎨 UX Insight: Document Activity Timeline
┌─────────────────────────────────────────────────────
│ 1С:      ЖурналРегистрации — отдельный журнал,
│          не встроен в форму документа.
│ Fiori:   Object Page — Feed section с timeline
│          изменений, комментариев, статусов.
│ ERPNext: Timeline — встроен в каждую форму:
│          комментарии, email, изменения статуса,
│          версии, assignments. Killer feature.
│ Odoo:    Chatter — комментарии, логи изменений,
│          email, followers. Встроен в каждую форму.
│ ─────────────────────────────────────────────────────
│ Лучшее:  ERPNext Timeline / Odoo Chatter — оба
│          отличные, но ERPNext чище визуально.
│ Metapus: FormSidebar показывает только "Изменено"
│          и "Создано" (дата + пользователь). Нет
│          полной истории изменений, комментариев.
│          Рекомендация: ActivityTimeline компонент
│          в FormSidebar — лог проведений/отмен,
│          изменений статуса, комментарии пользователей.
│          Backend: domain.WithLogging уже есть.
└─────────────────────────────────────────────────────
```

`FormSidebar` сейчас показывает только метаданные создания/изменения и файлы: form-sidebar.tsx:70-88

---

## 6. Drag-and-Drop reorder строк в табличной части

```
🎨 UX Insight: Line Reordering in Table Parts
┌─────────────────────────────────────────────────────
│ 1С:      Кнопки "Переместить вверх/вниз" в командной
│          панели табличной части. Нет drag-and-drop.
│ Fiori:   Responsive Table — drag-and-drop reorder
│          с accessibility (keyboard).
│ ERPNext: Child Table — drag handle для reorder строк.
│ Odoo:    sequence field + drag handle в One2many.
│ ─────────────────────────────────────────────────────
│ Лучшее:  ERPNext/Odoo — drag handle интуитивен.
│ Metapus: DocumentLineRow не поддерживает reorder.
│          @dnd-kit уже в проекте (ColumnChooserPopover).
│          Добавить drag handle в DocumentLineRow,
│          используя тот же @dnd-kit/sortable.
└─────────────────────────────────────────────────────
```

`@dnd-kit` уже используется в `ColumnChooserPopover` — инфраструктура готова: column-chooser-popover.tsx:7-25 

`DocumentLineRow` сейчас не имеет drag handle: document-line-row.tsx:112-114

---

## 7. Skeleton Loading вместо спиннеров

```
🎨 UX Insight: Loading States
┌─────────────────────────────────────────────────────
│ 1С:      Простой индикатор "Загрузка..." или progress bar.
│ Fiori:   Busy Indicator + Ghost Loading (skeleton
│          placeholders для таблиц и форм).
│ ERPNext: Простой спиннер.
│ Odoo:    Skeleton loading в Owl components (17+).
│ ─────────────────────────────────────────────────────
│ Лучшее:  SAP Fiori Ghost Loading — skeleton повторяет
│          layout будущего контента, уменьшает CLS.
│ Metapus: Есть skeleton.tsx в UI kit, но формы и
│          списки используют Loader2 spinner.
│          Рекомендация: DataTableSkeleton (строки-
│          заглушки), FormSkeleton (поля-заглушки).
│          Уменьшает perceived loading time.
└─────────────────────────────────────────────────────
```

`skeleton.tsx` есть в UI kit, но не используется в основных компонентах — везде `Loader2`: auto-form.tsx:208-214 

---

## 8. Notification Center

```
🎨 UX Insight: Notification Center
┌─────────────────────────────────────────────────────
│ 1С:      Центр уведомлений (с 8.3.17) — задачи,
│          напоминания, системные сообщения.
│ Fiori:   Notification Center — bell icon, grouped
│          by priority, mark as read, actions.
│ ERPNext: Notification Log — bell icon, real-time
│          via WebSocket, seen/unseen.
│ Odoo:    Discuss — messaging + notifications,
│          @mentions, channels.
│ ─────────────────────────────────────────────────────
│ Лучшее:  SAP Fiori — structured notifications с
│          priority, grouping, inline actions.
│ Metapus: Только заглушка NOTIFICATION_COUNT = 3
│          в AppSidebar. Нет реального UI.
│          Рекомендация: NotificationPanel (Sheet)
│          с группировкой по типу, mark as read,
│          click-to-navigate. Backend: через
│          Transactional Outbox (уже есть).
└─────────────────────────────────────────────────────
```

Сейчас уведомления — это только хардкоженная заглушка: app-sidebar.tsx:181-182 

---

## 9. Conditional Row Styling / Status Indicators в списках

```
🎨 UX Insight: Conditional Formatting in Lists
┌─────────────────────────────────────────────────────
│ 1С:      Условное оформление в динамическом списке —
│          цвет строки по условию (просроченные, помеченные).
│ Fiori:   Criticality annotations в CDS Views →
│          автоматическая подсветка (Error/Warning/Success).
│          Object Status indicators (semantic colors).
│ ERPNext: Indicator colors на List View (green dot =
│          submitted, red = cancelled, etc.).
│ Odoo:    decoration-danger, decoration-warning в
│          tree view — условная подсветка строк.
│ ─────────────────────────────────────────────────────
│ Лучшее:  SAP Fiori Criticality — metadata-driven,
│          автоматическое. Odoo — декларативное.
│ Metapus: DataTable имеет rowClassName callback —
│          механизм есть, но нет стандартных паттернов.
│          Рекомендация: стандартизировать semantic
│          row states: "overdue" (красный), "warning"
│          (жёлтый), "success" (зелёный), "muted"
│          (серый для помеченных на удаление).
│          Metadata-driven: поле criticality в meta.
└─────────────────────────────────────────────────────
```

`DataTable` уже поддерживает `rowClassName` — механизм готов, нужна стандартизация: data-table.tsx:76-78 

---

## 10. Excel-like Paste в табличные части

```
🎨 UX Insight: Clipboard Paste into Table Parts
┌─────────────────────────────────────────────────────
│ 1С:      Нет нативной вставки из Excel в табличную
│          часть (только через обработки).
│ Fiori:   Smart Table — paste from clipboard (limited).
│ ERPNext: Нет.
│ Odoo:    Нет нативного, но есть модули.
│ ─────────────────────────────────────────────────────
│ Лучшее:  Ни одна ERP не делает это хорошо.
│          Это возможность ПРЕВЗОЙТИ все системы.
│ Metapus: Ctrl+V в табличной части → парсинг TSV
│          (tab-separated) из Excel → создание строк
│          с автоматическим маппингом колонок.
│          Огромная экономия времени при массовом вводе.
│          Реализация: onPaste handler на table container,
│          парсинг clipboard.getData("text/plain").
└─────────────────────────────────────────────────────
```

---

## 11. Responsive / Mobile Layout

```
🎨 UX Insight: Mobile Responsiveness
┌─────────────────────────────────────────────────────
│ 1С:      Мобильный клиент — отдельное приложение,
│          адаптированные формы.
│ Fiori:   Responsive by design — все Fiori Elements
│          адаптируются от desktop до mobile.
│          Breakpoints: S/M/L/XL.
│ ERPNext: Mobile-responsive (Bootstrap), но UX
│          деградирует на маленьких экранах.
│ Odoo:    Responsive с 16+, но таблицы проблемные.
│ ─────────────────────────────────────────────────────
│ Лучшее:  SAP Fiori — единственная ERP с настоящим
│          mobile-first design.
│ Metapus: Есть use-mobile.tsx hook, compact mode,
│          hideOnDesktop в FormToolbar. Но DataTable,
│          DocumentLineRow, FilterSidebar не адаптивны.
│          Рекомендация: для таблиц на mobile —
│          card layout вместо table (Fiori pattern).
│          FilterSidebar → bottom sheet на mobile.
└─────────────────────────────────────────────────────
```

`FormToolbar` уже имеет `hideOnDesktop` для адаптивности действий: form-toolbar.tsx:35-39 

---

## 12. Dashboard: Configurable Widget Grid

```
🎨 UX Insight: Configurable Dashboard
┌─────────────────────────────────────────────────────
│ 1С:      Начальная страница — настраиваемые формы,
│          но ограниченные виджеты.
│ Fiori:   Fiori Launchpad — tiles, drag-and-drop,
│          groups, KPI tiles с real-time данными.
│ ERPNext: Workspace — shortcuts, charts, number cards,
│          custom layout.
│ Odoo:    Dashboard — configurable, drag-and-drop
│          виджеты, KPI, графики.
│ ─────────────────────────────────────────────────────
│ Лучшее:  Odoo Dashboard + ERPNext Workspace —
│          пользователь сам собирает свой dashboard.
│ Metapus: Есть widget-grid, widget-gallery-dialog,
│          widget-config-panel, chart-renderer и др.
│          Инфраструктура ЕСТЬ. Нужно убедиться, что:
│          1) Виджеты подключены к реальным данным
│          2) Пользователь может добавлять/удалять
│          3) Layout сохраняется в user preferences
└─────────────────────────────────────────────────────
```

Инфраструктура виджетов уже богатая:


Но `RecentActivity` и `QuickActions` пока используют хардкоженные данные: recent-activity.tsx:5-42 quick-actions.tsx:6-27 

---

## 13. Bulk Field Update (Групповое изменение реквизитов)

```
🎨 UX Insight: Bulk Field Update
┌─────────────────────────────────────────────────────
│ 1С:      "Групповое изменение реквизитов" — выбрать
│          N записей → изменить поле X на значение Y.
│ Fiori:   Mass Edit — выбрать строки → Edit → изменить
│          общие поля для всех выбранных.
│ ERPNext: Bulk Update — выбрать записи → Actions →
│          Update field.
│ Odoo:    Server Action "Update Record" на выбранных.
│ ─────────────────────────────────────────────────────
│ Лучшее:  1С — самый мощный (любое поле, условия).
│ Metapus: Batch actions есть (post/unpost/deletionMark),
│          но нет generic "изменить поле X для выбранных".
│          Рекомендация: BulkUpdateDialog — выбрать поле
│          из metadata → ввести значение → применить.
│          Работает через selectAllByFilter (уже есть).
└─────────────────────────────────────────────────────
```

Batch-инфраструктура уже мощная (ID-based + filter-based + SSE progress): useDocumentBatchActions.ts:85-95 

---

## Сводная таблица приоритетов

| # | Фича | Сложность | Ценность | Есть основа? |
|---|-------|-----------|----------|-------------|
| 1 | Command Palette (Ctrl+K) | Средняя | Очень высокая | `Command` + metadata store |
| 2 | Keyboard Shortcuts Registry + Help | Средняя | Высокая | Разрозненные shortcuts |
| 3 | Inline Editing в списках | Высокая | Средняя | DataTable.rowClassName |
| 4 | Toast с Undo | Низкая | Высокая | sonner + batch actions |
| 5 | Activity Timeline | Высокая | Высокая | FormSidebar + WithLogging |
| 6 | Drag-and-Drop строк | Низкая | Средняя | @dnd-kit уже в проекте |
| 7 | Skeleton Loading | Низкая | Средняя | skeleton.tsx есть |
| 8 | Notification Center | Высокая | Высокая | Заглушка в sidebar |
| 9 | Conditional Row Styling | Низкая | Средняя | rowClassName callback |
| 10 | Excel Paste в таблицы | Средняя | Высокая | Нет |
| 11 | Mobile Layout | Высокая | Средняя | use-mobile hook |
| 12 | Dashboard с реальными данными | Средняя | Высокая | Widget infrastructure |
| 13 | Bulk Field Update | Средняя | Сред