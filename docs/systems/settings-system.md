# Система настроек

> **TL;DR:** Tenant-настройки хранятся в `sys_settings`, параметры организации — в `cat_organizations`, пользовательские предпочтения — в `sys_user_preferences`. UI секций описан frontend-реестром.
> **Тип:** Concept
> **Аудитория:** Developer

## Хранение и API

`sys_settings` содержит JSONB-секции `general`, `numbering`, `performance`, `exchange_rates`. Primary key `singleton` с CHECK допускает не более одной строки; исходная миграция создаёт эту строку.

`GET /api/v1/settings` возвращает секции и общую `version`. `PATCH /api/v1/settings/:section` принимает `{data, version}`. Обновление секции увеличивает **общую версию строки**: параллельная запись другой секции также может вызвать 409. Доступ требует роли `admin`; изменение дополнительно защищено `system.settings.update` step-up.

`exchangeRates` в JSON содержит только `sourceId`. Настройки источников и интеграционных ключей находятся в отдельных справочниках.

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
db/migrations/00011_sys_settings.sql
```
