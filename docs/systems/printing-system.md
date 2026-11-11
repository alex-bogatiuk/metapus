# Система печатных форм (Printing System)

> **TL;DR:** Code is Metadata. Печатные формы описаны в статическом реестре и `.gohtml`-шаблонах. Документ получает печать, когда объявляет capability `printForms` и его handler реализует `Print`/`ListPrintForms`. Сейчас печать есть у `crypto_invoice`.

> **Тип:** Concept
> **Аудитория:** Developer

---

## 1. Архитектура: Code is Metadata

Печатные формы не хранятся в базе данных. Они описываются в статическом реестре (`internal/domain/printing/registry.go`). Это даёт строгую типизацию, версионирование через Git и защищает от поломок при миграциях БД.

### Определение формы
Каждая форма (`PrintFormDef`) привязывается к ключу документа и имеет:
- идентификатор (`"standard"` для `crypto_invoice`; пустой `format` выбирает первую форму);
- имя файла шаблона (`crypto_invoice.gohtml`);
- размер бумаги (`A4`, `A5`);
- категорию (`standard` — системная, `custom` — добавленная интегратором).

## 2. Подключение к документу

| Шаг | Где |
|---|---|
| Capability `PrintForms: true` | `DocumentCapabilities()` регистрации, например `internal/content/entity_contracts.go`. Флаг включает маршруты `GET /print-forms`, `GET /:id/print`, их HTTP-контракты и `capabilities.printForms` в метаданных |
| Handler | Поле `*DocumentPrintHandler[T]` и методы `Print`, `ListPrintForms`, делегирующие ему (`handlers/crypto_invoice.go`). Зависимости — `DocumentDeps.PrintRegistry/PrintRenderer/PDFRenderer` |
| Данные формы | `BuildPrintData(ctx, doc, policy)` возвращает `PrintData.Table` (`handlers/crypto_invoice_print.go`) |
| Шаблон | `templates/<doc>.gohtml`, обычно только `{{template "table_form" .}}` |

Оба маршрута требуют `<permission>:read`. Если флаг объявлен без методов handler, тест `builtin_entities_test.go` найдёт расхождение маршрутов и контрактов.

## 3. Безопасность данных

- **RLS/CEL:** документ читается через `DocumentService.GetByID`, как в `GET /:id`.
- **FLS документа:** построитель получает `security.ProjectForRead(doc, policy)` и саму политику. Скрытое поле в проекции равно нулю, поэтому построитель проверяет `policy.IsFieldAllowed(<db column>)` и выводит «скрыто», а не `0`.
- **Связанные сущности** (мерчант, токен, сеть, кошелёк) читаются только через `readaccess.Service`: проверяются право чтения, RLS, CEL-правила и FLS целевой сущности. Запрещённая ссылка показывается как «скрыто» и не прерывает форму. Без `readaccess` в контексте печать возвращает `403`.
- **Суммы crypto:** `printing.FormatCryptoUnits` переводит `CryptoAmount` по `decimalPlaces` токена точно, без float. Если масштаб токена недоступен, печатаются сырые единицы с пометкой «мин. ед.» — масштаб не угадывается.
- **Шаблоны** — `html/template`: значения экранируются; активного содержимого (`onclick`, скриптов) в формах нет.

## 4. Форматы вывода

1. **HTML** (`output=html`, по умолчанию): самодостаточная страница. Фронтенд вставляет её в sandbox-`iframe` без `allow-scripts` и вызывает печать.
2. **PDF:** HTML передаётся через domain-порт `PDFRenderer` внутреннему сервису с Chromium (`PDF_RENDERER_URL`). Основной server остаётся distroless. Sandbox, ограничения и smoke-test описаны в [руководстве renderer](../../deployments/pdf-renderer/README.md). Недоступный сервис даёт `503 PDF_RENDERER_UNAVAILABLE`; HTML и DOCX работают независимо.
3. **DOCX:** `godocx` строит документ из той же `PrintData.Table` (`internal/infrastructure/printing/docx_renderer.go`), поэтому содержимое совпадает с HTML.

## 5. Форма `crypto_invoice`

- **Заголовок:** «Инвойс», «№ <номер> от <дата>».
- **Реквизиты:** мерчант, статус (подпись из `metadata.EnumLabel`), сеть, токен, адрес для оплаты, срок действия, требуемые подтверждения. Если заданы — номер заказа, внешний ID, описание, моменты оплаты, подтверждения, завершения и отзыва (UTC).
- **Таблица:** получено, подтверждено, зачислено, переплата (если она положительна или скрыта).
- **Итог:** «Сумма к оплате» = ожидаемая сумма.

## 6. Frontend

`AutoForm` показывает `PrintMenuButton` в слоте `printMenu` у `FormToolbar`, только если `capabilities.printForms === true` и документ сохранён. Кнопка загружает формы через `GET /api/v1/document/{routePrefix}/print-forms` (`usePrintForms`, кэш на сессию) и предлагает 🖨 HTML, 📄 PDF, 📃 DOCX. Тип `PrintFormSummary` — алиас сгенерированного контракта.

## 7. Файловая карта

```path
internal/domain/printing/registry.go — реестр форм
internal/domain/printing/data.go, table.go — PrintData и PrintTable
internal/infrastructure/printing/renderer.go — HTML-рендерер, FormatCryptoUnits
internal/infrastructure/printing/templates/ — base.gohtml (styles, table_form), crypto_invoice.gohtml
internal/infrastructure/printing/docx_renderer.go — DOCX
internal/infrastructure/http/v1/handlers/print.go — DocumentPrintHandler
internal/infrastructure/http/v1/handlers/crypto_invoice_print.go — данные формы инвойса
frontend/components/shared/print-menu-button.tsx, frontend/hooks/usePrintForms.ts — UI
```

## Связанные документы
- [auth-security.md](auth-security.md) — RBAC, RLS и FLS, которые применяет печать.
- [../../deployments/pdf-renderer/README.md](../../deployments/pdf-renderer/README.md) — сервис PDF.
