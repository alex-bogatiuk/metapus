# Базовая конфигурация «Торговля и сервис»

> Базовая конфигурация второго уровня ([слои приложения](../systems/configuration-layers.md)): торговый учёт, которого компании хватает и без отраслевой надстройки. Склад, закупки, продажи, заказы с резервами, цены, себестоимость и закрытие месяца. На ней строится надстройка [«Автобизнес»](../automotive/README.md). Код — [`configurations/trade`](../../configurations/trade), вне `internal/`: надстройка из другого репозитория должна иметь возможность её импортировать.

## Состав

| Объект | Ключ | Таблица | Код |
|---|---|---|---|
| Справочник «Номенклатура» (иерархический: папки — товарные категории) | `item` | `cat_items` | [`domain/catalogs/item`](../../configurations/trade/domain/catalogs/item/model.go) |
| Справочник «Склады» | `warehouse` | `cat_warehouses` | [`domain/catalogs/warehouse`](../../configurations/trade/domain/catalogs/warehouse/model.go) |
| Справочник «Состояния заказов покупателей» (название магазина и статус: открыт, в работе, выполнен, отменён) | `customer_order_state` | `cat_customer_order_states` | [`domain/catalogs/order_state`](../../configurations/trade/domain/catalogs/order_state/model.go) |
| Документ «Приходная накладная» с табличной частью «Товары» | `goods_receipt` | `doc_goods_receipts`, `doc_goods_receipts__goods` | [`domain/documents/goods_receipt`](../../configurations/trade/domain/documents/goods_receipt/model.go) |
| Документ «Заказ покупателя» с табличной частью «Товары» (количество и резерв) | `customer_order` | `doc_customer_orders`, `doc_customer_orders__goods` | [`domain/documents/customer_order`](../../configurations/trade/domain/documents/customer_order/model.go) |
| Документ «Расходная накладная» с табличной частью «Товары» | `goods_issue` | `doc_goods_issues`, `doc_goods_issues__goods` | [`domain/documents/goods_issue`](../../configurations/trade/domain/documents/goods_issue/model.go) |
| Регистр накопления «Запасы на складах» (количество по складу и номенклатуре) | `warehouse_stock` | `reg_warehouse_stock_movements`, `reg_warehouse_stock_balances` | [`domain/registers/warehouse_stock`](../../configurations/trade/domain/registers/warehouse_stock) |
| Регистр накопления «Запасы» (по складу, номенклатуре и заказу: свободный остаток и резервы) | `stock` | `reg_stock_movements`, `reg_stock_balances` | [`domain/registers/stock`](../../configurations/trade/domain/registers/stock) |
| Документ «Установка цен номенклатуры» с табличной частью «Цены» | `price_setting` | `doc_price_settings`, `doc_price_settings__prices` | [`domain/documents/price_setting`](../../configurations/trade/domain/documents/price_setting/model.go) |
| Регистр сведений «Цены номенклатуры» (цена с дня, пишет установка цен) | `item_prices` | `reg_item_prices` | [`domain/registers/item_prices`](../../configurations/trade/domain/registers/item_prices/record.go) |
| Регистр сведений «История состояний заказов» (с какого момента заказ в каком состоянии, кто поставил) | `order_state_history` | `reg_order_state_history` | [`domain/registers/order_state_history`](../../configurations/trade/domain/registers/order_state_history/record.go) |
| Регистр накопления «Заказы покупателей» (что заказ ещё ждёт к отгрузке) | `customer_orders` | `reg_customer_orders_movements`, `reg_customer_orders_balances` | [`domain/registers/customer_orders`](../../configurations/trade/domain/registers/customer_orders) |
| Регистр накопления «Закупки» (обороты: количество и сумма по складу, номенклатуре, поставщику, валюте) | `purchases` | `reg_purchases_movements` | [`domain/registers/purchases`](../../configurations/trade/domain/registers/purchases) |
| Регистр накопления «Продажи» (обороты: количество, выручка и себестоимость по складу, номенклатуре, покупателю, валюте) | `sales` | `reg_sales_movements` | [`domain/registers/sales`](../../configurations/trade/domain/registers/sales) |
| Регистр накопления «Себестоимость запасов» (количество и стоимость остатка в валюте учёта, пишет только закрытие месяца) | `inventory_cost` | `reg_inventory_cost_movements`, `reg_inventory_cost_balances` | [`domain/registers/inventory_cost`](../../configurations/trade/domain/registers/inventory_cost) |
| Документ «Закрытие месяца» (себестоимость продаж месяца по средней) | `month_closing` | `doc_month_closings` | [`domain/documents/month_closing`](../../configurations/trade/domain/documents/month_closing/model.go) |

- Поставщик — платформенный справочник `counterparty`, валюта — `currency`.
- Проведение приходной накладной добавляет каждую строку в запасы склада, расходной — списывает. Перепроведение заменяет движения, отмена проведения их удаляет. Остатки ведут триггеры таблицы движений.
- Работа под заказ, как в требованиях: заказ покупателя переносит строку «Резерв» со свободного остатка на заказ и ставит всё количество в «Заказы покупателей». Недостающее приходит приходной накладной со строкой под заказ и сразу встаёт в резерв. Расходная накладная по заказу берёт «Из резерва» из резерва заказа, остальное — со свободного остатка, и закрывает заказ.

  | Документ | «Запасы на складах» | «Запасы» | «Заказы покупателей» |
  |---|---|---|---|
  | Заказ покупателя | — | − свободный, + заказ (резерв) | + количество |
  | Приходная накладная | + | + свободный или + заказ строки | — |
  | Расходная накладная | − | − заказ (из резерва), − свободный (остальное) | − количество, если по заказу |

- Состояние заказа решает, что он делает ([`customer_order/state.go`](../../configurations/trade/domain/documents/customer_order/state.go)). Справочник начинается с шести состояний: «Новый» и «Ожидает предоплаты» (открыт), «В работе» и «Готов к выдаче» (в работе), «Выполнен», «Отменён»; магазин добавляет свои с одним из четырёх статусов.

  | Статус | Движения заказа |
  |---|---|
  | Открыт (заказ без состояния — «Новый») | никаких: не резервирует, не ждёт отгрузки |
  | В работе | резерв строк и ожидание отгрузки всего количества |
  | Выполнен, Отменён (закрыт) | ожидание только отгруженного; неотгруженное — «Отменено», остаток резерва — «Снято с резерва», возвращается в свободный остаток |

  Смены статуса ограничены [статусной моделью](../systems/document-statuses.md) заказа: открытый заказ уходит в работу и обратно, открытый или рабочий выполняется или отменяется, а возобновить закрытый может только владелец права «Заказы покупателей: возобновление закрытого заказа» (`document:customer_order:reopen`). Каждая смена статуса попадает в историю статусов документа.

  Перед каждой записью заказа обработчик `ApplyState` (статус состояния читает порт `order_state.Statuses`, его реализация — [`order_state_repo`](../../configurations/trade/infrastructure/storage/postgres/order_state_repo/statuses.go)) берёт статус состояния и для закрытого заказа считает «Отменено» и «Снято с резерва» по регистрам, так чтобы после проведения заказ ничего не ждал и ничего не держал в резерве. Проведение остаётся функцией документа. Повторное открытие обнуляет оба поля и возвращает резерв, если свободного остатка хватает. Отгрузить по закрытому заказу или отменить проведение отгруженного нельзя: это не даст контроль «Заказов покупателей».
- Перед каждой записью заказа (`customer_order.BeforeSave`) строки без цены берут цену номенклатуры на дату заказа в его валюте из «Цен номенклатуры» и сумму по ней, а смена состояния записывается в «Историю состояний заказов» с автором. Повторная запись того же состояния истории не добавляет.
- Ввод на основании заказа ([`goods_issue/basis.go`](../../configurations/trade/domain/documents/goods_issue/basis.go), [`goods_receipt/basis.go`](../../configurations/trade/domain/documents/goods_receipt/basis.go)):
  - **Расходная накладная** — покупатель, склад и валюта заказа. В строках то, что заказ ещё ждёт к отгрузке («Заказы покупателей»), «Из резерва» — сколько из этого зарезервировано («Запасы»), сумма — пропорционально количеству. Полностью отгруженный или не проведённый заказ накладную не даёт.
  - **Приходная накладная под заказ** — склад и валюта заказа. В строках то, что заказ ждёт сверх резерва, каждая строка под заказом, поэтому приход сразу встаёт в резерв. Поставщик и закупочные цены заполняет пользователь.
- Свободный остаток = физический остаток минус резервы: он хранится строкой с пустым заказом в «Запасах», поэтому другой покупатель не может взять чужой резерв, а заказ берёт свой.
- Запасы не уходят в минус: расходная накладная на большее количество, чем есть на складе, не проводится, как и перепроведение или отмена приходной, из которой уже продали. Сообщение называет склад, номенклатуру, остаток, требуемое и недостающее количество.
- Себестоимость продаж считает документ «Закрытие месяца» по средней за месяц в валюте учёта: стоимость остатка на начало месяца плюс закупки месяца, делённые на их количество ([закрытие месяца](../systems/document-sequences.md#закрытие-месяца-в-автозапчастях)). Приходные и расходные накладные входят в последовательность себестоимости: изменение накладной в закрытом месяце требует перепровести закрытия с этого месяца по порядку, форма закрытия об этом предупреждает.
- Характеристики, серии и ячейки в каркас не входят.
- Номенклатура, склады и состояния заказов загружаются из XLSX или CSV пунктом «Загрузить из файла…» в меню списка ([загрузка данных](../systems/data-import.md)); строки накладных и установки цен вставляются из Excel в табличную часть.
- Конфигурация не импортирует ни крипто, ни свою надстройку: это проверяет `internal/architecture`.
- Код и тесты конфигурации импортируют из платформы только публичный API `pkg/sdk` ([описание](../sdk/README.md)): так её можно вынести в отдельный репозиторий. Это тоже закреплено тестом.

## Запуск

| Что | Команда |
|---|---|
| API-сервер | `go run ./cmd/trade-server` |
| Воркер | `go run ./cmd/trade-worker` |
| Тенанты (`init-meta`, `create`, `migrate`) | `go run ./cmd/trade-tenant …` |
| Сид (с `SEED_DEMO_DATA=true` — три склада и три позиции) | `go run ./cmd/trade-seed` |
| Фронтенд | `cd frontend && npm run configure -- trade && npm run dev` |
| Проверка всех конфигураций | `make check-configurations` |

Миграции лежат в [`db/configuration/trade/`](../../db/configuration/trade), их история — `goose_db_version_trade`. Свои страницы фронтенду не нужны: справочники и документы показываются универсальными списками и формами платформы. `frontend/configuration/trade` регистрирует только контракты и навигацию.

Версия конфигурации для надстроек — `content.Version` (сейчас 1.0.0). Пакеты `configurations/trade/domain` — её публичный API для надстроек: добавления в них поднимают минорную версию, несовместимые изменения — мажорную.

Интеграционные тесты [`stock_integration_test.go`](../../configurations/trade/content/stock_integration_test.go) создают тенанта с миграциями конфигурации и проверяют проведение, перепроведение и отмену, контроль остатков при продаже, сценарий заказа с резервом и приходом под заказ, заполнение документов на основании заказа, а также что в базе нет крипто-таблиц.

## Как объявлен регистр

«Запасы на складах» — декларативный регистр: его описание — это структура движения и одна переменная в [`movement.go`](../../configurations/trade/domain/registers/warehouse_stock/movement.go).

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
| Регистрация | `reg.RegisterAccumulation(warehouse_stock.Register)` в [`content/register.go`](../../configurations/trade/content/register.go) |
| Движения документа | Метод `RegisterMovements` приходной накладной вызывает `warehouse_stock.Register.Receipt`; регистратор, версию и период платформа берёт из документа |
| Запись, перепроведение, отмена проведения | Общий recorder и хранилище платформы |
| Остатки | Общий триггер `sys_accumulation_balances` (платформенная миграция `00006`) с таблицами и колонками регистра в аргументах |
| Контроль остатков | `NonNegative: true`: блокировки затронутых комбинаций и отказ с `INSUFFICIENT_BALANCE`, если документ увёл остаток в минус ([описание](../systems/posting-engine.md#контроль-отрицательных-остатков-и-резервы)) |
| Запросы | `Register.Balances`, `BalancesAt` (на момент), `Turnovers` (за период) с фильтром по измерениям |
| «Движения документа» | Колонки и подписи из тегов `meta` |
| Таблицы | Миграция [`00002_trade_stock.sql`](../../db/configuration/trade/00002_trade_stock.sql) по образцу `migration.AccumulationDDL`; миграция тенанта сверяет таблицы, типы, первичный ключ и триггеры с объявлением и останавливается при расхождении |

Ручной код регистра (сервис, visitor, recorder, репозиторий, две plpgsql-функции, модуль проведения) удалён. Новый регистр — это структура движения, переменная, строка регистрации и миграция таблиц.

Что ещё не входит: виды цен (сейчас одна цена продажи), экран истории состояний на форме заказа, запрет переходов между состояниями (любое состояние можно выбрать из любого), «Потребность в запасах» и заказы поставщикам, суммовой учёт и себестоимость в «Запасах», регистры сведений, отчёт по остаткам. Регистр, который проводит воркер, а не сервер, пока подключается к движку воркера вручную (`accumulation.Install`).
