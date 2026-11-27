# Надстройка «Автозапчасти»

> Надстройка третьего уровня ([слои приложения](../systems/configuration-layers.md)) над базовой конфигурацией [«Торговля и сервис»](../trade/README.md): специфика магазина автозапчастей. Код — [`configurations/autoparts`](../../configurations/autoparts), требования — [`configuration-requirements/`](../../configurations/autoparts/configuration-requirements).

Пока надстройка ничего не добавляет: тенант «Автозапчастей» — это тенант «Торговли и сервиса» с записанной надстройкой. Объекты и реквизиты автозапчастей появятся с механизмами надстройки (этап B): реквизиты к объектам базы, движения к её документам, объявленные точки замены поведения.

- База — `trade` версии 1.0 (`BaseVersion`). Платформа не запустит надстройку на базе другой мажорной версии или младшей минорной.
- Надстройка импортирует только `pkg/sdk` и пакеты своей базы; база о надстройке не знает. Это проверяет `internal/architecture`.
- Миграции надстройки — свои, со своей историей `goose_db_version_autoparts`; сейчас их нет.

## Запуск

| Что | Команда |
|---|---|
| API-сервер | `go run ./cmd/autoparts-server` |
| Воркер | `go run ./cmd/autoparts-worker` |
| Тенанты (`init-meta`, `create`, `migrate`) | `go run ./cmd/autoparts-tenant …` |
| Сид (с `SEED_DEMO_DATA=true` — демо базы) | `go run ./cmd/autoparts-seed` |
| Фронтенд | `cd frontend && npm run configure -- autoparts && npm run dev` |

Сервер надстройки обслуживает тенантов `trade+autoparts`. Тенанта одной базы `autoparts-tenant migrate` переводит на надстройку; снять надстройку с тенанта или заменить её нельзя.
