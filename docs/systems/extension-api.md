# API Расширений (Extension API)

> **TL;DR:** Трехуровневая архитектура позволяет интеграторам добавлять новые сущности (справочники, документы), регистры и бизнес-логику (через Хуки) в папку `extensions/` без изменения исходного кода ядра платформы.

> **Тип:** Concept
> **Аудитория:** Developer, Integrator
> **Связанные:** [crud-pipeline.md](crud-pipeline.md)

---

## 1. Трехуровневая модель

Вместо того чтобы изменять ядро платформы (Hard Fork), Metapus использует модульную систему.

| Слой | Назначение | Кто разрабатывает | Папка |
|------|------------|-------------------|-------|
| **Core** | Движок, транзакции, права доступа | Команда Metapus | `internal/core/` |
| **Content** | Встроенные (стандартные) справочники и документы | Команда Metapus | `internal/content/` |
| **Extensions**| Кастомные клиентские сущности | Интегратор | `extensions/` |

## 2. Регистрация новых сущностей

Чтобы добавить новый Справочник или Документ, интегратор не пишет CRUD-хэндлеры с нуля. Он создаёт структуру, реализующую `CatalogRegistration` или `DocumentRegistration`.

Затем модуль подключается в общем Composition Root `extensions/register.go`; сервер и генератор контрактов используют этот список:
```go
myExtension.Register(factoryReg) // registration uses v1.EntityRegistration[Model]
```

**Frontend Auto-Discovery:**
Платформа генерирует UI для расширений автоматически! Фронтенд получает `/api/v1/meta/entities` и использует generic routes `[routePrefix]` для списка и AutoForm. Запросы/ответы должны быть зарегистрированы в HTTP contracts registry: metadata не разрешает незарегистрированный URL или дополнительное writable поле. Новые сущности автоматически появляются в разделе расширений меню; custom forms и особое размещение подключаются через frontend registry/navigation. Подробности — [новый справочник](../howto/new-entity.md).

## 3. Точки расширения

Кроме создания новых сущностей, Extension API позволяет "вклиниваться" в стандартные:

1. **Хуки (Hooks):** Инъекция кастомной логики в системные документы.
   ```go
   invoiceService.Hooks().OnBeforeCreate(func(ctx, doc) error { ... })
   ```
2. **Новые Регистры:** Интегратор может написать свой `RegisterVisitor` и `RegisterRecorder`, чтобы документы, реализующие нужные source-интерфейсы начали двигать новые кастомные регистры. Регистры конфигурации подключаются модулем `posting.Module` через `RouterConfig.PostingModules`: модуль добавляет visitors, recorders и проверки в движок и возвращает `MovementProvider` для отчёта о движениях документа. Пример — `cryptoregisters.Registers`.
3. **Атрибуты (JSONB):** У каждой сущности есть поле `attributes` для произвольных данных интегратора. Схемы, валидации, фильтрации и колонок для них нет; типизированное поле добавляется в Go-модель сущности.
4. **CEL Политики:** Написание кастомных правил блокировок (например, запрет проведения документов с суммой > 1 млн) в виде конфигурации через CEL (Common Expression Language), без перекомпиляции Go.

## 4. Scaffold CLI

Для ускорения разработки расширений используется встроенный кодогенератор:
```bash
go run cmd/scaffold/main.go --name waybill --type document
```
Генератор создаёт модель выбранного вида, service/repository, явные writable DTO, update-version, регистрацию, типизированный HTTP bundle и миграцию с актуальными CDC triggers и permissions. Документ создаётся без искусственной табличной части. `--hierarchical` включает папки и проверки иерархии справочника; без флага справочник плоский.

Общее `entity.Definition` связывает identity, таблицу, маршрут и hierarchy. `contracts/spec.Entity` выводит стандартные HTTP-контракты из Definition и конкретных DTO. После добавления модуля в `extensions/register.go` выполните `go run ./cmd/http-contracts`; отдельные правки registry/routes для расширения не нужны. Права других ролей, бизнес-поля, локализация, движения и новый worker workflow требуют явной настройки.

Модули `extensions/` компилируются внутри Metapus. Публичный внешний SDK в `pkg/extension` пока ограничен версией API; не используйте его как готовый фасад регистрации.

## 5. Единый контракт встроенных сущностей и расширений

Все встроенные справочники и документы предоставляют domain `Definition`. Модель реализует `EntityDefinition()`; generic `EntityRegistration[Model]` выводит identity и metadata, `CatalogRegistrationFor[Model,Create,Update,Read]` также выводит стандартные transport contracts. Нулевое значение регистрации работоспособно: передавать Definition вручную в struct literal не требуется.

Factory contracts требуют Definition и HTTP bundle. Стандартные DTO и маршруты codegen получает из того же registry, что и сервер. Разрешённые document capabilities ограничивают набор сгенерированных маршрутов; custom команды и специальные detail projections объявляются у владельца документа.

Catalog service и repository читают hierarchy из Definition, включая type, folder restrictions и max depth. Глобальный реестр hierarchy удалён; прямые repository writes используют ту же политику, что и service. Для отдельного тестового ограничения передайте копию Definition с нужной Hierarchy.

## Extension API 2.0

Добавлены обязательные Definition и HTTPContracts у factory registrations. CatalogServiceConfig принимает Definition вместо строкового EntityName; глобальные GetCatalogMeta/RegisterCatalogMeta удалены. Внешний модуль на прежнем API должен перейти на EntityDefinition, typed registration и Definition-based repository/service constructors. Новые scaffold-модули сразу используют API 2.0.
