# Alchemy TRON: обновление стенда 05.11.2026

> **TL;DR:** Поддержка Alchemy расширена на TRON Mainnet/Nile. На тестовом стенде подключён новый аккаунт Nile; обе роли прошли сверку блоков и receipts с тремя независимыми операторами.
>
> **Тип:** Reference
>
> **Аудитория:** Developer, QA, Admin
>
> **Связанные:** [integration-accounts.md](integration-accounts.md), [public-nile-deployment.md](public-nile-deployment.md)

## Реализация

- В существующий провайдер `alchemy` добавлены точные TRON-профили: `tron-mainnet.g.alchemy.com` → `tron/mainnet`, `tron-testnet.g.alchemy.com` → `tron/nile`. Роли: `fullnode`, `solidity`; группа доверия: `alchemy`.
- Точный host имеет приоритет перед общим EVM-суффиксом независимо от порядка профилей. TRON-host нельзя привязать к EVM или другой TRON-сети.
- Авторизация вставляет ключ между `/v2/` и `/wallet/...` либо `/walletsolidity/...`, сохраняя тело запроса и исходный URL без секрета. EVM execution RPC продолжает использовать `/v2/{key}`.
- TRON-клиент нормализует завершающий `/` базового URL. Чтение transaction info использует POST с JSON-полем `value`, что совместимо с managed transport без query parameters.
- Подтверждённый ответ Alchemy об отключённой сети возвращает `access_denied`, не инвалидируя ключ общего аккаунта.
- Новые enum, SQL-миграции, отдельный RPC pool или специальные ветки форм не потребовались. Метаданные провайдера и эксплуатационная инструкция обновлены.

## Развёртывание

Публичный стенд: [https://176.215.182.82](https://176.215.182.82).
Compose project: `metapus-public-test`; tenant: `nile-e2e`,
`01a0cfbe-5e01-72f5-8484-2046b6a672a5`.

Окончательный release: `alchemy-tron-20261104T205322Z-r2`.
API, worker, frontend, ingestion monitor и AML pool controller пересобраны
из текущего checkout. Checkout не содержит `.git`; вместо commit сохранены
manifest хешей Go-исходников, build logs и фактические Docker image IDs.
Схема осталась на версии 76. PostgreSQL, Vault, кошельки и public edge сохранены.

Перед каждым переключением остановлены application writers и сохранены
согласованные tenant/meta dumps и исходный runtime manifest. Все 214 таблиц
сохранили свои отпечатки при переключении. Финальные dumps восстановлены
в отдельном PostgreSQL; отпечатки всех 214 tenant tables совпали, meta database
восстановлена. Восстановление выполнялось только в изолированном контейнере.

Финальные backup и доказательства:

```text
/Users/alexandrbogatiuk/.metapus/public-test-20260923/backups/alchemy-tron-20261104T205322Z-r2
```

Каталог имеет режим `0700`, файлы — `0600`; `SHA256SUMS` проверен. Backup
перед первым переключением также сохранён под release без суффикса `-r2`.

## Новый аккаунт и подключения

Использован второй переданный владельцем ключ. Первый ключ получил от Alchemy
отказ из-за отключённой сети и в аккаунт стенда не записывался. Значения ключей
в исходниках, документации и отчёте отсутствуют.

| Объект | Значение |
|---|---|
| Аккаунт | `ALCHEMY-NILE`, «Alchemy — TRON Nile» |
| UUID аккаунта | `01a1a85b-2e4c-7889-9c38-759cfb8ba5d9` |
| Пул | `ALCHEMY-NILE`, `01a1a85b-2e6c-77c2-b38b-881e3db407b6` |
| Именованный ключ | `nile` |
| Сеть | `TRON-NILE` |
| База без секрета | `https://tron-testnet.g.alchemy.com/v2/` |
| Provider ID / trust group | `alchemy-nile` / `alchemy` |
| Роли | `fullnode`, `solidity`, обе активны |
| Приоритет | `20` в обеих ролях |
| Минимальный интервал | `200` мс в обеих ролях |
| Квота | Месячная; начальные настройки сброса: день 1, UTC |

Аккаунт, пул и подключения созданы через штатный HTTPS API с существующим MFA
и подтверждением `system.integrations.write`. Ключ сохранён штатным механизмом
шифрования credentials. GET-ответы содержат только признак наличия credentials;
доступность аккаунта после проверок — `available`. Порядок прежних провайдеров
сохранён; Alchemy добавлен перед GetBlock.

## Проверки

| Проверка | Результат |
|---|---|
| Полная Go-сборка | Успешно |
| `go test -race -count=1 ./...` | Успешно, 117 пакетов с тестами |
| PostgreSQL `integration_repo`, `-tags integration -race` | Успешно в отдельном кластере; проверен настоящий TRON-клиент и обновление ключей |
| `golangci-lint` | 0 issues |
| HTTP contracts | 436 контрактов и схем проверены |
| Frontend typecheck / lint / production build | Успешно |
| Unit-сценарии настроек аккаунтов | 17 passed |
| Браузерные сценарии интеграций и RPC | 34 passed |
| API readiness, публичные HTTPS health/login | Успешно, HTTP 200 |
| MFA, refresh дважды, `/auth/me` | Успешно; один активный refresh token проверочной сессии |
| Alchemy connection test | `ready=true` |
| Alchemy Nile FullNode / Solidity | В каждой роли head, block и block receipts: 5/5 успешных выборок |
| Канонические TRON digests | 10/10 сравнений совпали; Alchemy присутствует во всех сравнениях |
| Положительный internal TRX sample | Подтверждён Alchemy и тремя независимыми операторами в обеих ролях |
| Покрытие TRON | 4 здоровых независимых оператора на роль при требовании 2 |
| Native HTTP POST | Успешны transaction info и account в обеих ролях, account resources в FullNode |
| Sepolia RPC | `ready=true`; Alchemy прошёл свежую и историческую выборки |
| AML configuration / ingestion Nile и Sepolia | `ready=true` |
| Сверка балансов | 64/64 пары; 0 read errors, 0 новых/изменённых incidents |
| Открытые reconciliation incidents | 0 |
| Restore backup и checksums | Успешно |

TRON-высоты: `72429556`, `72429555`, `71963230`, `71963229`, `71994066`.
Положительный sample:
`dc0586ee1dfa586a9ebe8264926a9b905db1f269c517cea5d1274c59138a9675`.

Расширенный preflight обнаружил лишний `/` в реальном TRON-клиенте после
первого переключения. Ошибка исправлена и покрыта PostgreSQL-тестом;
все итоговые проверки выполнены на окончательном release `-r2`.
После второго переключения ingestion сначала догонял сеть, затем обе сети
вернулись в `ready`. Проверочные auth-сессии отозваны отдельно.

## Границы проверки

Живые проверки нового подключения выполнены на Nile. Mainnet-профиль покрыт
локальными тестами; Mainnet-аккаунт на стенде не создавался. TronGrid V1 indexer
через Alchemy не объявлен. Операторские проверки не отправляли новые
блокчейн-транзакции и не выполняли платные AML-проверки.

Существующее ограничение GetBlock Nile (`pool_exhausted`) сохранилось; его
подключения не отключались и quorum не снижался. В Sepolia ограничения
отдельных резервных endpoint не препятствовали успешной проверке Alchemy
и независимого покрытия.
