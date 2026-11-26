# Система настроек

> **TL;DR:** Tenant-настройки хранятся в `sys_settings`, параметры организации — в `cat_organizations`, пользовательские предпочтения — в `sys_user_preferences`. UI секций описан frontend-реестром.
> **Тип:** Concept
> **Аудитория:** Developer

## Хранение и API

`sys_settings` содержит JSONB-секции `general`, `numbering`, `performance`, `exchange_rates`. Primary key `singleton` с CHECK допускает не более одной строки; исходная миграция создаёт эту строку.

`GET /api/v1/settings` возвращает секции и общую `version`. `PATCH /api/v1/settings/:section` принимает `{data, version}`. Обновление секции увеличивает **общую версию строки**: параллельная запись другой секции также может вызвать 409. Доступ требует роли `admin`; изменение дополнительно защищено `system.settings.update` step-up.

`exchangeRates` в JSON содержит только `sourceId`. Настройки источников и интеграционных ключей находятся в отдельных справочниках.

## Дата запрета изменения

Секция `periodLock` (`{"date": "YYYY-MM-DD"}`, пустая строка — запрета нет) закрывает период. Дата хранится не в `sys_settings`, а строкой без организации в `sys_period_locks` (платформенная миграция `00007`; таблица готова к датам по организациям); запись секции увеличивает общую версию настроек, как и остальные. Правило: документ с датой не позже этого дня в часовом поясе клиента (`general.timezone`) нельзя создать, изменить, провести, отменить проведение или пометить на удаление. Нельзя и перенести документ в закрытый период или вынести из него: проверяются новая и сохранённая даты.

- Проверка — `settings.PeriodLockSettings.CheckDocumentDates` ([`period_lock.go`](../../internal/domain/settings/period_lock.go)). Её вызывает общий репозиторий документов (`document_repo.BaseDocumentRepo`) в `Create`, `Update`, `Delete`. Через него проходит любая запись документа, в том числе запись после проведения и отмены, поэтому одна проверка закрывает все пути, и проводимые регистры откатываются вместе с отказом.
- Ошибка — `PERIOD_CLOSED` (422): «Период до 31.10.2026 включительно закрыт. Изменение данных запрещено.» с деталями `documentDate` и `lockDate`.
- Исключение — право `closed_period:edit` («Изменение документов в закрытом периоде»): роль, которой его дали в редакторе ролей, работает в закрытом периоде. Администраторы имеют все права. Запрос без пользователя (системные процессы) не проверяется.
- Пока нет: даты по организациям и по ролям (одна дата на клиента), записи документов прямым SQL в обход репозитория (воркеры крипто) не проверяются.

## UI и добавление настройки

`frontend/lib/settings-registry.ts` описывает секции, поля, виджеты и подписи. `SettingsSectionRenderer` строит форму по этому локальному реестру. Endpoint `/meta/settings/sections` отсутствует.

1. Измените Go-модель в `internal/domain/settings/model.go`, DTO/валидацию и repository при необходимости.
2. Добавьте поле в соответствующую секцию `frontend/lib/settings-registry.ts` и TS-контракт.
3. Обновите defaults исходной миграции для новой БД; для заполненного окружения подготовьте явный upgrade по правилам проекта.
4. Если поле меняет HTTP DTO, обновите generated contracts через `go run ./cmd/http-contracts` и проверьте `-check`.

Preferences имеют собственный API и модель; раскладка dashboard хранится в `sys_user_preferences.dashboard_layout`. Wallet pool — отдельная pseudo-section настроек со специализированным API.

## Файловая карта

```path
internal/domain/settings/model.go
internal/infrastructure/http/v1/handlers/settings.go
internal/infrastructure/storage/postgres/settings_repo.go
frontend/lib/settings-registry.ts
frontend/components/settings/settings-section-renderer.tsx
internal/domain/settings/period_lock.go
db/migrations/00005_platform.sql
db/migrations/00007_period_lock.sql
```
