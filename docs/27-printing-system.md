# Система печатных форм (Printing System)

> Руководство по архитектуре печатных форм, их регистрации и добавлению новых макетов.

---

## Философия: CODE IS METADATA

В Metapus печатные формы следуют принципу **CODE IS METADATA**:
1.  **Макеты (.gohtml)** — это файлы в репозитории, а не строки в базе данных.
2.  **Реестр (Registry)** — конфигурация форм описывается в коде (статический реестр), что обеспечивает типизацию, версионирование и легкость развертывания.
3.  **Категории** — формы разделяются на `standard` (регламентированные платформой) и `custom` (добавленные пользователем/партнером).

---

## Архитектура Backend

Система печати состоит из трех уровней:
1.  **Registry (`internal/domain/printing/registry.go`)**: Хранит определения форм (`PrintFormDef`) для каждого типа документа.
2.  **Renderer (`internal/domain/printing/renderer.go`)**: Движок рендеринга (поддерживает HTML, PDF через Chrome/Edge, DOCX через XML-шаблоны).
3.  **Handlers**: HTTP-хендлеры, реализующие интерфейсы для печати и получения списка форм.

### Определение формы (PrintFormDef)

```go
type PrintFormDef struct {
    Name      string            // Идентификатор (slug), например "standard"
    Label     string            // Название в UI, например "Товарная накладная"
    Template  string            // Имя файла шаблона в internal/domain/printing/templates/
    PaperSize string            // Размер бумаги (A4, A5)
    Category  PrintFormCategory // standard или custom
    SortOrder int               // Порядок сортировки в меню
}
```

---

## Как добавить новую печатную форму

### Шаг 1: Создание шаблона (.gohtml)

Создайте файл шаблона в директории `internal/domain/printing/templates/`. Используйте стандартный синтаксис `html/template`.

**Пример:** `my_custom_form.gohtml`
```html
<!DOCTYPE html>
<html>
<head>
    <style>
        @page { size: {{.PaperSize}}; margin: 10mm; }
        body { font-family: "DejaVu Sans", sans-serif; }
    </style>
</head>
<body>
    <h1>{{.FormLabel}} № {{.Doc.Number}}</h1>
    <!-- Данные доступны через объект .Doc (DTO документа) -->
</body>
</html>
```

### Шаг 2: Регистрация в Registry

Добавьте регистрацию формы в `NewPrintFormRegistry()` в файле `internal/domain/printing/registry.go` (или через метод `Register` при инициализации модуля).

```go
r.Register("goods_issue", PrintFormDef{
    Name:      "proforma_invoice",
    Label:     "Счет на оплату",
    Template:  "proforma_invoice.gohtml",
    PaperSize: "A4",
    Category:  CategoryStandard,
    SortOrder: 10,
})
```

### Шаг 3: Реализация интерфейсов в Handler

Чтобы документ поддерживал печать, его Handler должен реализовывать два интерфейса (в `internal/infrastructure/http/v1/route_helpers.go`):

1.  `DocumentPrintHandlerInterface` — для самого процесса печати.
2.  `DocumentPrintFormsListHandler` — для отображения списка доступных форм в меню.

**Пример реализации (делегирование):**

```go
func (h *MyHandler) Print(c *gin.Context) {
    h.printHandler.Print(c)
}

func (h *MyHandler) ListPrintForms(c *gin.Context) {
    h.printHandler.ListPrintForms(c)
}
```

`RegisterDocumentRoutes` автоматически обнаружит эти интерфейсы и добавит роуты:
- `GET /api/v1/document/{type}/:id/print`
- `GET /api/v1/document/{type}/print-forms`

---

## Архитектура Frontend

На фронтенде используется динамический компонент, который получает метаданные с бэкенда.

### Хук usePrintForms

Хук `usePrintForms(documentType)` инкапсулирует логику загрузки и кэширования:
- Использует `useSyncExternalStore` для эффективного обновления.
- Кэширует список форм в памяти (так как они меняются только при перезагрузке сервера).
- Группирует формы по категориям (`standard`, `custom`).

### Компонент PrintMenuButton

Универсальная кнопка для тулбара документа. Принимает `documentType` и `documentId`.

```tsx
<PrintMenuButton 
    documentType="goods-issue" 
    documentId={id} 
/>
```

**Особенности UI:**
- **Split-кнопка**: При нажатии открывается Dropdown.
- **Группировка**: Формы разделены на «Регламентированные» и «Кастомные».
- **Inline Форматы**: Для каждой формы доступны иконки для мгновенного выбора формата:
    - 🖨 — HTML (открывается в новом окне для печати)
    - 📄 — PDF (скачивание)
    - 📃 — DOCX (скачивание)

---

## Полезные советы

1.  **Данные для печати**: В конфиге `DocumentPrintHandler` (в фабрике документа) вы описываете функцию `BuildPrintData`. Именно она готовит данные (DTO, таблицы, итоги), которые будут доступны в шаблоне.
2.  **PDF Рендеринг**: Metapus использует headless браузер для генерации PDF из HTML. Убедитесь, что стили в шаблоне оптимизированы для `@media print`.
3.  **Локализация**: Для вывода сумм прописью или дат используйте вспомогательные функции из пакета `printing` (доступны в шаблонах).
