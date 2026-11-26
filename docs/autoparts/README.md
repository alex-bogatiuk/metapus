# Конфигурация «Автозапчасти»

> Каркас отраслевой конфигурации магазина автозапчастей. Он проверяет, что платформа несёт вторую конфигурацию рядом с криптопроцессингом и не зависит ни от одной из них. Требования к полной конфигурации — в папке [`configuration-requirements/`](../../internal/configuration/autoparts/configuration-requirements).

## Состав

| Объект | Ключ | Таблица | Код |
|---|---|---|---|
| Справочник «Номенклатура» (иерархический: папки — товарные категории) | `item` | `cat_items` | [`domain/catalogs/item`](../../internal/configuration/autoparts/domain/catalogs/item/model.go) |
| Справочник «Склады» | `warehouse` | `cat_warehouses` | [`domain/catalogs/warehouse`](../../internal/configuration/autoparts/domain/catalogs/warehouse/model.go) |
| Документ «Приходная накладная» с табличной частью «Товары» | `goods_receipt` | `doc_goods_receipts`, `doc_goods_receipts__goods` | [`domain/documents/goods_receipt`](../../internal/configuration/autoparts/domain/documents/goods_receipt/model.go) |
| Документ «Расходная накладная» с табличной частью «Товары» | `goods_issue` | `doc_goods_issues`, `doc_goods_issues__goods` | [`domain/documents/goods_issue`](../../internal/configuration/autoparts/domain/documents/goods_issue/model.go) |
| Регистр накопления «Запасы на складах» (количество по складу и номенклатуре) | `warehouse_stock` | `reg_warehouse_stock_movements`, `reg_warehouse_stock_balances` | [`domain/registers/warehouse_stock`](../../internal/configuration/autoparts/domain/registers/warehouse_stock) |

- Поставщик — платформенный справочник `counterparty`, валюта — `currency`.
- Проведение приходной накладной добавляет каждую строку в запасы склада, расходной — списывает. Перепроведение заменяет движения, отмена проведения их удаляет. Остатки ведут триггеры таблицы движений.
- Запасы не уходят в минус: расходная накладная на большее количество, чем есть на складе, не проводится, как и перепроведение или отмена приходной, из которой уже продали. Сообщение называет склад, номенклатуру, остаток, требуемое и недостающее количество.
- Характеристики, серии, ячейки и суммовой учёт (`РегистрНакопления.Запасы`) в каркас не входят.
- Конфигурация не импортирует крипто, и наоборот: это проверяет `internal/architecture`.
- Код и тесты конфигурации импортируют из платформы только публичный API `pkg/sdk` ([описание](../sdk/README.md)): так её можно вынести в отдельный репозиторий. Это тоже закреплено тестом.

## Запуск

| Что | Команда |
|---|---|
| API-сервер | `go run ./cmd/autoparts-server` |
| Воркер | `go run ./cmd/autoparts-worker` |
| Тенанты (`init-meta`, `create`, `migrate`) | `go run ./cmd/autoparts-tenant …` |
| Сид (с `SEED_DEMO_DATA=true` — три склада и три позиции) | `go run ./cmd/autoparts-seed` |
| Фронтенд | `cd frontend && npm run configure -- autoparts && npm run dev` |
| Проверка всех конфигураций | `make check-configurations` |

Миграции лежат в [`db/configuration/autoparts/`](../../db/configuration/autoparts), их история — `goose_db_version_autoparts`. Свои страницы фронтенду не нужны: справочники и документ показываются универсальными списками и формами платформы. `frontend/configuration/autoparts` регистрирует только контракты и навигацию.

Интеграционные тесты [`stock_integration_test.go`](../../internal/configuration/autoparts/content/stock_integration_test.go) создают тенанта с миграциями конфигурации и проверяют проведение, перепроведение и отмену, контроль остатков при продаже, а также что в базе нет крипто-таблиц.

## Как объявлен регистр

«Запасы на складах» — декларативный регистр: его описание — это структура движения и одна переменная в [`movement.go`](../../internal/configuration/autoparts/domain/registers/warehouse_stock/movement.go).

```go
type Movement struct {
	entity.MovementBase
	WarehouseID id.ID          `db:"warehouse_id" meta:"label:Склад,ref:warehouse" register:"dimension"`
	ItemID      id.ID          `db:"item_id" meta:"label:Номенклатура,ref:item" register:"dimension"`
	Quantity    types.Quantity `db:"quantity" meta:"label:Количество" register:"resource"`
}

var Register = accumulation.New[Movement](accumulation.Definition{
	Key: "warehouse_stock", Label: "Запасы на складах", Table: "reg_warehouse_stock", Kind: accumulation.Balances,
	NonNegative: true,
})
```

Остальное делает платформа ([`internal/domain/registers/accumulation`](../../internal/domain/registers/accumulation)):

| Что | Кто делает |
|---|---|
| Регистрация | `reg.RegisterAccumulation(warehouse_stock.Register)` в [`content/register.go`](../../internal/configuration/autoparts/content/register.go) |
| Движения документа | Метод `RegisterMovements` приходной накладной вызывает `warehouse_stock.Register.Receipt`; регистратор, версию и период платформа берёт из документа |
| Запись, перепроведение, отмена проведения | Общий recorder и хранилище платформы |
| Остатки | Общий триггер `sys_accumulation_balances` (платформенная миграция `00006`) с таблицами и колонками регистра в аргументах |
| Контроль остатков | `NonNegative: true`: блокировки затронутых комбинаций и отказ с `INSUFFICIENT_BALANCE`, если документ увёл остаток в минус ([описание](../systems/posting-engine.md#контроль-отрицательных-остатков-и-резервы)) |
| Запросы | `Register.Balances`, `BalancesAt` (на момент), `Turnovers` (за период) с фильтром по измерениям |
| «Движения документа» | Колонки и подписи из тегов `meta` |
| Таблицы | Миграция [`00002_autoparts_stock.sql`](../../db/configuration/autoparts/00002_autoparts_stock.sql) по образцу `migration.AccumulationDDL`; миграция тенанта сверяет таблицы, типы, первичный ключ и триггеры с объявлением и останавливается при расхождении |

Ручной код регистра (сервис, visitor, recorder, репозиторий, две plpgsql-функции, модуль проведения) удалён. Новый регистр — это структура движения, переменная, строка регистрации и миграция таблиц.

Резервы под заказы покупателей платформа ведёт как измерение «заказ» в регистре остатков (пустое — свободный остаток). В конфигурации их пока нет: для них нужен документ «Заказ покупателя» и регистр «Запасы» по требованиям. Что ещё не входит: регистры сведений, отчёт по остаткам. Регистр, который проводит воркер, а не сервер, пока подключается к движку воркера вручную (`accumulation.Install`).
