# Руководство по обновлению

В этом документе описывается, как обновлять клиентские расширения при изменении платформы Metapus.

---

## Как обновляться

### Шаг 1: Обновление зависимости

```bash
# При использовании модулей Go (внешние расширения)
cd client-ext/
go get metapus@v1.x.x

# При использовании workspace (внутренние расширения)
# Просто стяните последнюю ветку main
```

### Шаг 2: Сборка

```bash
go build ./...
```

Если компилируется — вы закончили. Система типов Go гарантирует совместимость интерфейсов.

### Шаг 3: Исправление ошибок компиляции (при мажорных версиях)

Компилятор точно покажет, какие интерфейсы изменились. Распространенные сценарии:

#### Новый обязательный метод в CatalogRegistration

```
./registration.go:10:15: cannot use &VehicleRegistration{} as CatalogRegistration
    missing method: NewRequiredMethod
```

**Решение:** Реализуйте новый метод в вашей структуре регистрации.

#### Изменение сигнатуры хуков

```
./hooks.go:5:42: cannot use hookFunc as Hook[Vehicle]
    have func(context.Context, Vehicle) error
    want func(context.Context, Vehicle, HookContext) error
```

**Решение:** Обновите сигнатуру вашей функции хука.

#### Изменение типов полей в DTO

```
./dto.go:12:5: cannot use field (variable of type string) as int
```

**Решение:** Обновите код маппинга DTO.

---

## История версий

### v1.0.0 (Начальный релиз)

**Установлен контракт Extension API:**

- Интерфейс `CatalogRegistration` (обязательные: RoutePrefix, Permission, EntityName, Build)
- Интерфейс `DocumentRegistration` (обязательные: RoutePrefix, Permission, EntityName, Build)
- Опциональные интерфейсы: `Presentable`, `Inspectable`, `Labeled`, `ReferenceProvider`
- `HookRegistry[T]` с поддержкой приоритетов
- `posting.RegisterVisitor` и `posting.RegisterRecorder`
- `entity.Attributes` (пользовательские поля JSONB)
- `SchemaCache` с механизмами LISTEN/NOTIFY
- `FactoryRegistry` для регистрации в runtime
- Frontend `UIRegistry` + `registerWidget()`

---

## FAQ

**В: Сломается ли мое расширение при минорных обновлениях?**
О: Нет. Минорные обновления добавляют только новые опциональные интерфейсы (проверяемые через type assertion). Ваш код не сломается.

**В: Что делать, если нужно изменить поведение стандартной сущности?**
О: Используйте `HookRegistry` (до/после событий) или декораторы middleware для `DocumentService` (`Chain[T]()`). Никогда не изменяйте файлы платформы напрямую.

**В: Можно ли добавлять поля к существующим сущностям без миграции БД?**
О: Да, используйте колонку JSONB `attributes` через таблицу `sys_custom_field_schemas`. Endpoint метаданных автоматически объединит их для фронтенда.

**В: Как протестировать расширение на новой версии платформы?**
О: Запустите `make check-extensions` — скрипт собирает все расширения в папке `extensions/` с текущим ядром.
