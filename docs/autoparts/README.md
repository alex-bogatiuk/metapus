# Конфигурация «Автозапчасти»

> Каркас отраслевой конфигурации магазина автозапчастей. Он проверяет, что платформа несёт вторую конфигурацию рядом с криптопроцессингом и не зависит ни от одной из них. Требования к полной конфигурации — в папке [`configuration-requirements/`](../../internal/configuration/autoparts/configuration-requirements).

## Состав

| Объект | Ключ | Таблица | Код |
|---|---|---|---|
| Справочник «Номенклатура» (иерархический: папки — товарные категории) | `item` | `cat_items` | [`domain/catalogs/item`](../../internal/configuration/autoparts/domain/catalogs/item/model.go) |
| Справочник «Склады» | `warehouse` | `cat_warehouses` | [`domain/catalogs/warehouse`](../../internal/configuration/autoparts/domain/catalogs/warehouse/model.go) |
| Документ «Приходная накладная» с табличной частью «Товары» | `goods_receipt` | `doc_goods_receipts`, `doc_goods_receipts__goods` | [`domain/documents/goods_receipt`](../../internal/configuration/autoparts/domain/documents/goods_receipt/model.go) |
| Регистр накопления «Запасы на складах» (количество по складу и номенклатуре) | `warehouse_stock` | `reg_warehouse_stock_movements`, `reg_warehouse_stock_balances` | [`domain/registers/warehouse_stock`](../../internal/configuration/autoparts/domain/registers/warehouse_stock) |

- Поставщик — платформенный справочник `counterparty`, валюта — `currency`.
- Проведение приходной накладной добавляет каждую строку в запасы склада. Перепроведение заменяет движения, отмена проведения их удаляет. Остатки ведут триггеры таблицы движений.
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

Интеграционные тесты [`stock_integration_test.go`](../../internal/configuration/autoparts/content/stock_integration_test.go) создают тенанта с миграциями конфигурации и проверяют проведение, перепроведение и отмену, а также что в базе нет крипто-таблиц.

## Сколько ручной работы требует регистр

Справочники и документ выводятся из моделей: `v1.Catalog` и `v1.Document` дают хранение, маршруты, контракты, права, проведение и движения. Регистр «Запасы на складах» пришлось писать руками целиком. Это список для будущих декларативных регистров:

1. **Тип движения** с `entity.MovementBase`, измерениями и ресурсами — и тип остатка (`movement.go`).
2. **Интерфейс репозитория и сервис**: запись, удаление при перепроведении, остаток, а также `entity.MovementProvider` с колонками движений, их подписями, типами и `RefEntity` (`service.go`).
3. **Интерфейс-источник для документов, visitor и recorder** проведения с ключом в `MovementSet.Extensions` и приведением типа (`posting.go`).
4. **PostgreSQL-репозиторий** поверх `register_repo.BaseAccumulationRepo`: список колонок и маппер строки, выборка движений документа, остаток (`stock_repo.go`).
5. **Модуль проведения** `posting.Module`, подключённый через `Configuration.Server` (`content/registers.go`).
6. **Миграция**: таблица движений и таблица остатков, две функции и два statement-триггера пересчёта остатков, индексы и ограничения (`00002_autoparts_stock.sql`).
7. **Источники движений документа** для FLS (`platform.MovementSourceProvider`) — обёртка над `v1.Document` в `content/register.go`.
8. **Метод документа** `GenerateWarehouseStockMovements` с версией движений `PostedVersion+1`.

Всё, кроме пункта 8 и правил выбора ресурсов, выводится из описания регистра: ключ, таблица, измерения со ссылками и ресурсы. Это примерно 300 строк Go и 100 строк SQL на каждый регистр. Отчёт по остаткам и контроль отрицательных остатков сюда ещё не входят.
