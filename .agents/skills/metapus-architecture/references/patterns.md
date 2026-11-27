# Рекомендуемые паттерны Metapus

У каждого паттерна: **когда** применять, **как** он устроен здесь, **где** посмотреть живой пример и **ловушка**, в которую обычно попадают. Пути даны от корня репозитория. Если пример расходится с этим текстом, верь коду и поправь текст.

Содержание:
1. Декларативная сущность
2. Явная регистрация
3. Порты и адаптеры
4. Точка расширения (инверсия зависимости)
5. Visitor / Recorder регистра
6. Хуки жизненного цикла и transactional outbox
7. Unit of Work через context
8. Контроль конкурентности
9. Фасад и сужение (`pkg/sdk`)
10. Composition Root и значение `Configuration`
11. Разбор на границе, тонкий адаптер
12. Состояние документа
13. Идемпотентность
14. Functional options

---

## 1. Декларативная сущность

**Когда.** Любой новый справочник или документ, который укладывается в стандартный CRUD (включая табличные части и расчётные поля).

**Как.** Модель встраивает `entity.Catalog` или `entity.Document`, описывает поля тегами `db`, `json`, `meta`, `binding` и возвращает `entity.Definition` — один источник для таблицы, маршрута, иерархии, правил кода/номера и табличных частей. Регистрация — одна строка в `content/`. DTO, репозиторий, сервис, маршруты, HTTP-контракты, TypeScript-типы и форма выводятся из модели (`autoShape`).

```go
// configurations/trade/domain/catalogs/warehouse/model.go
var Definition = entity.Definition{
	Key: "warehouse", Name: "Warehouse", Route: "warehouses", Table: "cat_warehouses",
	Kind: entity.CatalogKind,
	Code: entity.CodeRule{Prefix: "WH"},
}

type Warehouse struct {
	entity.Catalog
	Address string `db:"address" json:"address" meta:"label:Адрес"`
}

func (Warehouse) EntityDefinition() entity.Definition { return Definition }
func (e *Warehouse) Validate(ctx context.Context) error { return e.Catalog.Validate(ctx) }

// configurations/trade/content/register.go
reg.RegisterCatalog(registry.Catalog[warehouse.Warehouse](registry.Presentation{
	Singular: "Склад", Plural: "Склады", NewLabel: "Новый склад", Genitive: "склад",
}))
```

Таблица — в миграции своей истории. После добавления — `go run ./cmd/http-contracts`.

**Где.** `configurations/trade/domain/{catalogs,documents}`, `docs/howto/new-entity.md`, `docs/howto/new-document.md`, `go run ./cmd/scaffold --name X --type catalog|document`.

**Ловушка.** Писать DTO, репозиторий и handler руками «для контроля». Это копия того, что платформа уже делает, и она разойдётся с контрактами. Пиши руками только то, что не выводится (следующий паттерн).

## 2. Явная регистрация

**Когда.** Сущность выходит за автоправило: секреты только для записи, вычисляемые поля ответа, коллекции без своей таблицы, поле, задаваемое только при изменении, собственные маршруты.

**Как.** `v1.CatalogRegistrationFor[Model, Create, Update, Read]` с `Build` — типизированные DTO и свой сервис, но тот же реестр, права и контракты. Промежуточный вариант — обернуть автоматическую регистрацию и добавить интерфейс: `goodsReceiptRegistration` в `automotive/content/register.go` встраивает `*registry.AutoDocument` и реализует `MovementSourceProvider`.

**Где.** `internal/configuration/crypto/content/catalog_registrations.go`, `integration_registrations.go`.

**Ловушка.** Переходить на явную регистрацию из-за одного поля, которое решается тегом (`meta:"readOnly:true"`, `meta:"calc:…"`, `binding`). Сначала проверь теги в `docs/systems/extension-api.md`, раздел 2.

## 3. Порты и адаптеры

**Когда.** Доменному правилу или сценарию нужна база, сеть, подпись, время, очередь.

**Как.** Порт — интерфейс рядом с потребителем (`domain/<контекст>/service.go`: `type Repository interface { … }`; сценарии — в `application/`). Адаптер — в `infrastructure/` (`storage/postgres/<…>_repo`, HTTP-клиенты провайдеров). Склейка — в `content/` или `internal/bootstrap`. Домен формулирует, что ему нужно, в своих терминах; адаптер переводит в SQL или HTTP.

**Где.** `internal/configuration/crypto/domain/catalogs/merchant/service.go` (порты `Repository`, `APIKeyRepository`), `crypto/application/outboundapp` + `crypto/infrastructure/storage/postgres/outbound_repo` (сценарии исполнения и их SQL-реализации), `crypto/infrastructure/crypto_worker` (воркер без SQL — закреплено тестом).

**Ловушка.** Порт, повторяющий таблицу один к одному (`GetByID`, `Update`, `List`) — это не порт, а DAO, и он тащит модель хранения в домен. Порт описывает операцию предметной области: «зарезервировать адрес», «найти активный ключ».

## 4. Точка расширения (инверсия зависимости)

**Когда.** Платформе нужно вызвать поведение конфигурации, а импортировать конфигурацию нельзя.

**Как.** Платформа объявляет интерфейс или реестр и принимает реализации через значение `Configuration` (`Register`, `Server`, `Worker`, `PoolConsumers`, `IntegrationCatalog`, `Seed`). Готовые точки:

| Точка | Для чего |
|---|---|
| `posting.Module` (`configuration.Server.PostingModules`) | регистры, visitors/recorders, проверки проведения |
| `v1.RouteModule` (`RouteContext.Root/API/Protected/System`, `ProtectedGroup`, `Authenticated`) | собственные маршруты |
| `auth.WithAccessScopeProvider` | объектные права сессии |
| `middleware.WithCommandScope` | владелец ключей идемпотентных команд |
| `integrations.Catalog`, `integrations.PoolConsumer` | провайдеры интеграций и справочники, читающие пулы |
| `platform.MovementSourceProvider` | поля документа за колонками движений (FLS) |
| `v1.PrintFormsProvider` | печатные формы документа |
| фронтенд: `registerEntityOverlays`, `registerNavigation`, `registerWidget`, `registerSettingsPage`… | UI конфигурации |

Если подходящей нет — добавь новую тем же способом (интерфейс в платформе, поле в `configuration.Server`/`Worker` или метод `Configuration`, алиас в `pkg/sdk`, реализация в конфигурации).

**Где.** `docs/systems/extension-api.md`, раздел 4; `internal/domain/integrations/pool_consumer.go`; `internal/domain/posting/module.go`.

**Ловушка.** «Маленький» `if cfg.Name() == "crypto"` или строковое имя таблицы конфигурации в платформе. Оба ломают границу так же, как импорт, и второе ловит тест.

## 5. Регистр накопления: декларативный или Visitor / Recorder

**Когда.** Документ при проведении двигает регистр (остатки, взаиморасчёты, баланс).

**Как — по умолчанию декларативно.** Регистр с измерениями, ресурсами и остатками объявляется структурой движения и одной переменной; visitor, recorder, репозиторий и plpgsql пишет платформа (`internal/domain/registers/accumulation`, фасад `pkg/sdk/registers/accumulation`).

```go
// configurations/trade/domain/registers/warehouse_stock/movement.go
type Movement struct {
	entity.MovementBase
	WarehouseID id.ID          `db:"warehouse_id" meta:"label:Склад,ref:warehouse" register:"dimension"`
	ItemID      id.ID          `db:"item_id" meta:"label:Номенклатура,ref:item" register:"dimension"`
	Quantity    types.Quantity `db:"quantity" meta:"label:Количество" register:"resource"`
}

var Register = accumulation.New[Movement](accumulation.Definition{
	Key: "warehouse_stock", Label: "Запасы на складах", Table: "reg_warehouse_stock", Kind: accumulation.Balances,
	NonNegative: true, // остаток не уходит в минус: проведение с нехваткой отклоняется
})

// документ — accumulation.Source, чистая функция от своих полей
func (e *GoodsReceipt) RegisterMovements(_ context.Context, out *accumulation.Movements) error {
	for _, line := range e.Goods {
		warehouse_stock.Register.Receipt(out, warehouse_stock.Movement{WarehouseID: e.WarehouseID, ItemID: line.ItemID, Quantity: line.Quantity})
	}
	return nil
}
```

Дальше — `reg.RegisterAccumulation(warehouse_stock.Register)` в `content/register.go` и миграция таблиц по образцу `migration.AccumulationDDL(Register.Schema())`: таблица движений, таблица остатков, триггеры на общих функциях `sys_accumulation_balances` и `sys_accumulation_movements_immutable`. Миграция тенанта сверяет таблицы с объявлением и остановится при расхождении. Запросы — `Register.Balances`, `BalancesAt`, `Turnovers` с `accumulation.Filter` по колонкам измерений. Для FLS регистрация документа по-прежнему объявляет `MovementSources()`; ключи колонок движений — имена колонок (`warehouse_id`, `quantity`).

Регистр сведений (цены, история состояний, настройки по измерениям) объявляется так же: структура записи с `information.RecordBase`, `information.New[R](Definition{Key, Label, Table, Periodicity, Mode})`, `reg.RegisterInformation`. Независимый пишет код (`Set`), подчинённый — документ в том же `RegisterMovements` (`Register.Record`); читают срезом последних `SliceLast`.

Контроль отрицательных остатков — `NonNegative: true`, без своего валидатора: платформа блокирует затронутые комбинации в общем отсортированном порядке и отказывает с `INSUFFICIENT_BALANCE`, если документ увёл ресурс ниже нуля. Резерв — измерение «заказ» в том же регистре (`id.Nil` — свободный остаток): резервирование — расход со свободного и приход на заказ.

**Как — вручную, когда декларативного не хватает** (append-only финансовые движения со сторно, проверки остатков между несколькими регистрами, nullable-измерения, регистр крипто). Три части:
- **источник** — интерфейс документа: `Generate<Регистр>Movements(ctx) ([]Movement, error)`; чистая функция от полей документа;
- **Visitor** — проверяет, реализует ли документ источник, и кладёт движения в `MovementSet` под именем регистра (`set.SetExtension`);
- **Recorder** — забирает свой набор (`set.GetExtension`) и пишет через сервис регистра; `ReverseMovements` сторнирует при перепроведении.

Всё вместе отдаётся `posting.Module`, который также возвращает `MovementProvider` для отчёта «Движения документа». Ресурсы, которые надо заблокировать до проверки остатков, — через `PostingResourceLocker` (ключи отсортированы). Проверка остатков — `PostingValidator.ValidateBeforePost` или `PostingController` (проверка после записи всех регистров).

**Где.** Декларативный: `internal/domain/registers/accumulation`, `configurations/trade/domain/registers/warehouse_stock`, `db/migrations/00006_accumulation_registers.sql`. Ручной: `crypto/infrastructure/crypto/cryptoregisters`, `crypto/domain/registers/*`. Обзор — `docs/systems/posting-engine.md`.

**Ловушка.** Генерировать движения с обращением к базе (курс, остаток) — тогда перепроведение даёт другой результат. Нужные данные загружаются в документ до проведения или проверяются валидатором; генератор остаётся чистым. Вторая ловушка — UPDATE/DELETE финансовых движений: финансовые регистры append-only, отмена — `AppendReversalsByRecorder`. Декларативный регистр при перепроведении удаляет движения прежних версий, поэтому для финансового учёта со сторно он не подходит. Третья — миграция регистра, разошедшаяся с объявлением: исправляй миграцию (до релиза) или объявление, а не проверку.

## 6. Хуки жизненного цикла и transactional outbox

**Когда.** У записи есть последствие: проверка уникальности, нумерация, аудит, событие во внешнюю систему, уведомление.

**Как.** `CatalogService`, `BaseHeaderDocumentService`, `BaseDocumentService` дают три фазы:

| Фаза | Где выполняется | Ошибка | Для чего |
|---|---|---|---|
| `Before*` | в транзакции, до записи | откатывает | проверки с БД (уникальность), подготовка |
| `Transaction*` | в транзакции, после записи | откатывает | **обязательные** последствия: аудит, outbox, связанная запись |
| `After*` | `tx.OnCommit`, после внешнего коммита | логируется | необязательное: кэш, уведомление «хорошо бы» |

Внешнюю доставку (webhook, письмо, RPC) делает не хук, а relay outbox воркера: хук `Transaction*` ставит событие в outbox платформы (`OutboxPublisher.Publish` или `postgres.EnqueueOutbox`) атомарно с записью, воркер доставляет с повторами. Маршрут outbox конфигурация отдаёт в `configuration.Worker`.

**Где.** `internal/domain/repository.go` (`HookRegistry`, события `Transaction*`), `internal/domain/service.go`, `docs/systems/crud-pipeline.md`, `docs/systems/automation-engine.md`.

**Ловушка.** Сетевой вызов в `Before*`/`Transaction*` (держит транзакцию и не откатывается) или обязательный аудит в `After*` (теряется при падении процесса между коммитом и хуком).

## 7. Unit of Work через context

**Когда.** Несколько записей должны пройти или откатиться вместе.

**Как.** Менеджер транзакций тенанта лежит в `context.Context`. Домен: `tenant.GetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error { … })`. Репозитории берут querier из менеджера в контексте — он сам вернёт открытую транзакцию или пул, поэтому методы репозитория одинаково работают внутри и вне транзакции. Вложенный `RunInTransaction` переиспользует внешнюю транзакцию; `UseSavepoint` — когда вложенная ошибка не должна откатывать внешнюю (редко, дорого). Более строгие изоляция и `AccessMode` вложенного вызова не ослабляются молча — вызов падает.

**Где.** `internal/core/tx`, `internal/core/tenant/context.go`, `internal/infrastructure/storage/postgres/tx_manager.go`, `docs/systems/transactions.md`.

**Ловушка.** Передавать `pgx.Tx` или пул параметрами по цепочке; открывать транзакцию в handler; держать транзакцию на время RPC. Вторая ловушка — горутина, унаследовавшая контекст с транзакцией: она должна получить свой контекст с привязкой тенанта (`middleware.BindTenant`), иначе переживёт запрос и пул.

## 8. Контроль конкурентности

**Когда.** Два пользователя или воркера могут менять одно и то же.

**Как.**
- **По умолчанию — оптимистическая блокировка.** `version` в модели, `UPDATE … WHERE id = $1 AND version = $2`, ноль строк → `apperror.NewConcurrentModification` → 409. Базовые репозитории делают это сами.
- **Пессимистическая — только для остатков и переходов состояния.** `FOR UPDATE` или advisory lock, **ключи отсортированы** (иначе AB-BA deadlock), транзакция короткая. Документ при проведении блокирует движок (`posting.NewLockingEngine`).
- **Append-only для денег.** Движения не меняются; ошибка исправляется сторно и новым движением.
- **Один писатель общего состояния.** Если двум компонентам не нужно одно изменяемое — дай каждому своё; если нужно — один владелец, остальные через канал или очередь. В HTTP-потоке (SSE) пишет одна горутина.

**Где.** `crypto_register_repo` (`AppendReversalsByRecorder`, сортировка ключей), `internal/domain/posting/recorder.go` (`PostingResourceLocker`), `docs/systems/posting-engine.md`.

**Ловушка.** Лечить гонку ретраем или `sleep`. Сначала определи, кто писатели и что им общее, — обычно гонка исчезает, когда у состояния один владелец.

## 9. Фасад и сужение (`pkg/sdk`)

**Когда.** Конфигурации нужен механизм платформы.

**Как.** `pkg/sdk` — фасад: `type X = internal.X` и тонкие функции-обёртки, пакеты названы как внутренние. Фасад не только расширяют, но и **сужают**: если конфигурация собирает механизм из платформенных деталей (взять пул, держать ссылку, построить менеджер транзакций, положить тенанта в контекст), платформа делает это одной функцией, а детали уходят из фасада. Так появились:
- `middleware.BindTenant(ctx, manager, tenantID) (ctx, release, err)` — привязка тенанта для маршрутов, которые находят его сами;
- `RouteContext.ProtectedGroup(path)` / `Authenticated(path)` — группы под своим префиксом с цепочкой платформы;
- `ReferenceResolver.Resolve(ctx)` — без лишнего параметра-пула;
- `sdktest` — фикстуры только для тестов.

Каждое изменение — в `docs/sdk/CHANGELOG.md` и версии `sdk.APIVersion` (semver; удалить — только в мажорной).

**Где.** `pkg/sdk`, `docs/sdk/README.md`.

**Ловушка.** Выставить внутренний пакет целиком «на всякий случай» — каждый алиас становится обещанием совместимости. Выставляй то, что используется, и предпочитай одну функцию платформы набору деталей.

## 10. Composition Root и значение `Configuration`

**Когда.** Сборка сервисов, сервера, воркера, сида.

**Как.** Зависимости собираются руками, без DI-фреймворка, в одном месте: у платформы — `internal/bootstrap/{server,worker,tenant,seed}`, у конфигурации — её `content/`. Конфигурация целиком — одно значение `configuration.Configuration`: `Name`, `APIVersion`, `Register`, `RouteContracts`, `Migrations`, `IntegrationCatalog`, `PoolConsumers`, `Server(ctx, env)`, `Worker(ctx, env)`, `Seed(ctx, env)`. Свои настройки окружения конфигурация читает в `Server`/`Worker` и возвращает ошибку, если они неверны. `cmd/<имя>-server` — однострочный `main`: `launch.Server(launch.Build{…}, content.Trade(), launch.WithExtensions(extensions.Register))` из `pkg/sdk/launch`. Тест конфигурации — `sdktest.Check(t, cfg)`.

Надстройка — тоже одно значение: `configuration.AddOn` с `Base()` (значение базы) и `BaseVersion()`; база с надстройками объявляет `Version()` (`configuration.Versioned`). `cmd/<надстройка>-server` передаёт в `launch.Server` надстройку, платформа сама собирает её с базой (`configuration.Assemble`): база регистрируется, обслуживается и засевается первой.

**Где.** `configurations/trade/content/configuration.go` (база), `configurations/automotive/content/configuration.go` (надстройка), `internal/configuration/crypto/content` (полная), `internal/configuration/none`, `internal/configuration/stack.go` (сборка стека).

**Ловушка.** Глобальные синглтоны и `init()` с регистрацией; чтение переменных окружения в глубине доменного пакета; сборка зависимостей в `cmd/`.

## 11. Разбор на границе, тонкий адаптер

**Когда.** HTTP-обработчик, вход воркера из внешнего API, чтение файла, вебхук.

**Как.** Данные проверяются и превращаются в типы домена там, где входят, — дальше им доверяют. Запрос читается строго: неизвестное или незаписываемое поле — 400. Обработчик только переводит: DTO → вызов сервиса → DTO; ошибка уходит `c.Error(err)` + `c.Abort()`, ответ об ошибке формирует `ErrorHandler`. Бизнес-условий в обработчике нет. Сообщения клиенту не раскрывают сумм, балансов, внутренних ID и SQL (CWE-209) — детали в `logger.Warn`.

**Где.** `internal/infrastructure/http/v1/handlers`, `crypto/infrastructure/http/cryptohttp`, `docs/reference/development-rules.md` (разделы 3 и 7).

**Ловушка.** Повторная валидация одного и того же на каждом слое «на всякий случай» и `map[string]any` в DTO. Типизируй один раз на входе.

## 12. Состояние документа

**Когда.** Правило «можно ли изменить, провести, отменить проведение, удалить».

**Как.** Не `if doc.Posted && …`, а `doc.State()` — объект состояния (`DocumentState`) с `CanModify`, `CanPost`, `CanUnpost`, `CanDelete`. Правило живёт в одном месте, и новое состояние не требует правки всех условий. Свои предусловия проведения документ добавляет в `CanPost(ctx)`.

**Где.** `internal/core/entity/document_state.go`, `crypto/domain/documents/crypto_custody_receipt/model.go`, `configurations/trade/domain/documents/goods_receipt/model.go`.

**Ловушка.** Дублировать проверку состояния в handler и сервисе. Делегируй состоянию.

## 13. Идемпотентность

**Когда.** Повтор запроса клиентом, повтор задачи воркером, повторный запуск сида или миграции, рестарт после падения на середине.

**Как.** Повторный запуск и запуск после сбоя посередине приводят к тому же состоянию.
- Команды API, меняющие деньги или состояние, требуют `Idempotency-Key`; владелец ключей задаётся `middleware.WithCommandScope` (например, мерчант по API-ключу).
- Сид конфигурации (`Configuration.Seed`) можно запускать повторно — он ничего не ломает.
- Провижининг тенанта — сага со статусами и advisory lock по slug.
- Воркер: задача фиксирует результат в той же транзакции, что и отметку о выполнении; доставка из outbox — at-least-once, получатель дедуплицирует по ID события.

**Где.** `internal/infrastructure/http/v1/middleware/idempotency.go`, `internal/application/command`, `docs/operations/worker-reliability.md`.

**Ловушка.** «Проверить, что ещё не сделано, потом сделать» в двух транзакциях — между ними вклинится второй исполнитель. Проверка и действие — в одной транзакции под блокировкой или уникальным ключом.

## 14. Functional options

**Когда.** Новый конструктор с тремя и более необязательными параметрами. Существующие конструкторы не переписываются ради этого.

**Как.** `func NewX(required A, opts ...Option) *X`, где `type Option func(*config)`; опции называются `WithY`. Обязательное — позиционными параметрами, не опциями.

**Где.** `internal/infrastructure/storage/postgres/migration` (`WithConfiguration`, `WithPermissions`, `WithCatalogCodes`), `pkg/sdk/migration`.

**Ловушка.** Опция для обязательной зависимости — тогда забытая опция превращается в nil-панику во время работы, а не в ошибку компиляции.
