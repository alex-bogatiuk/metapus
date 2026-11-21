# Добавление RPC-провайдера

> **TL;DR:** RPC-провайдер — часть конфигурации: описание в каталоге `cryptointegrations`, профиль привязок в `rpcconfig`, HTTP-профиль в `cryptoprofiles` и строка в списке провайдеров миграции `00006`. Выбор аккаунтов, квоты, retry, EVM/TRON pools, HTTP handlers и формы остаются общими.
> **Тип:** How-To
> **Аудитория:** Developer, AI-Agent
> **Связанные:** [Аккаунты интеграций](../operations/integration-accounts.md), [RPC ingestion](../operations/rpc-ingestion.md)

## 1. Границы ответственности

| Часть | Ответственность |
|---|---|
| `domain/integrations.ProviderDefinition`, `Catalog` | Контракт платформы: назначения, формат и состав credentials, политика квоты, минимальный интервал аккаунта; неизменяемый каталог приложения, который передаётся сервисам, выбору аккаунтов и HTTP явно |
| `domain/crypto/cryptointegrations` | Провайдеры и назначения конфигурации (`rpc`, `aml_kyt`, `blockchain_resources`) и `Catalog()` — каталог приложения |
| `domain/crypto/rpcconfig` (`profiles.go`) | Допустимые сети, роли и адреса RPC-операторов, managed-only, имя ключа и группа доверия |
| `infrastructure/integrations/profiles` | Реестр профилей платформы (`Registry`, CoinGecko) и общие помощники: `Classify`, `RetryAt`, `HeaderKey`, `ClearAuthorization` |
| `infrastructure/crypto/cryptoprofiles` | HTTP-профили RPC-операторов, `Registry()` приложения и `ImportRPC` |
| `infrastructure/integrations.Transport` | Выбор аккаунта, ограничение попыток, pacing, отчёт о квоте/credentials, сохранение тела запроса |
| `application/integrationapp` | Сценарий проверки подключения пула (`ConnectionTestService`) |
| `application/cryptoapp.RPCConfigurationService` | Транзакционный сценарий изменения RPC-конфигурации сети |
| `domain/crypto/rpcconfig.RPCConfiguration.Plan` | Построение итогового набора изменений, конфликты, порядок и покрытие без SQL |
| `domain/crypto/rpcconfig.RPCTopology` | Общая конфигурация и каноническая ревизия без transport и значений секретов |
| `crypto_integration_repo` | Хранение RPC-подключений и настроек сетей; `RPCPoolConsumer` держит имена ключей пула согласованными с подключениями; `RPCProbe` и `RPCTransport` проверяют подключение |
| EVM/TRON adapters | Протокол сети, routing, quorum, operation circuits и поколения клиентов |

Тип провайдера (`Provider`, например `alchemy`), UUID подключения, имя подключения (`ProviderID`), аккаунт и группа доверия — разные идентичности. Несколько ключей, аккаунтов или подключений одного оператора не создают независимые голоса.

## 2. Объявить поддерживаемый контракт

Добавить стабильный `Provider` и описание в `internal/configuration/crypto/domain/crypto/cryptointegrations/catalog.go` (`Providers()`, место в `providerOrder`) и тот же идентификатор в оба `ck_integration_*_provider` миграции `00006_crypto_core.sql`; тест каталога сверяет их. В описании:

- `Kinds: []integrations.Kind{KindRPC}`, `CanCreate`, подписи и политику квоты;
- `AccountMinRequestIntervalMS`, если все привязки аккаунта должны использовать общий pacer;
- `Credentials.KeyFormat`: существующий `hex32`, `url_safe`, `bearer` либо обычный непрозрачный ключ;
- фиксированные credential fields, `RequireFields`, `SingleKey`, `KeyOnly` при необходимости;
- `Credentials.URLImports`, если UI может извлекать ключ из строго проверенной ссылки. URL не посещается и не сохраняется.

В `internal/configuration/crypto/domain/crypto/rpcconfig/profiles.go` добавить:

- `rpcProfiles`: family, network identifier, роли, точный host либо проверенный suffix, базовый путь без секрета и trust group; провайдер — в `rpcProviderOrder`;
- `managedRPCOnly`, если доступ должен использовать пул аккаунтов.

Пустой `NetworkIdentifier` разрешает любую сеть указанного family; конкретный identifier ограничивает профиль одной сетью. `SlotByRole` связывает имя ключа с RPC-ролью, например `fullnode`/`solidity`. `KeyName` фиксирует имя ключа профиля, например `sepolia`; пустое значение оставляет выбор оператору среди общих имён ключей аккаунтов пула. Итоговое имя возвращает `rpcconfig.RPCKeyName`; оно совпадает с полем credentials, которое заполняет форма аккаунта.

`rpcconfig.ValidateRPCBinding` используется каталогом; `rpcconfig.RPCProfileForURL` повторно проверяет адрес при загрузке managed binding и при авторизации запроса. Проверки на разных границах используют одну политику. Нельзя заменять их проверкой только в UI.

Группа доверия известного managed RPC выводится из профиля оператора. Пользовательское имя подключения или trust group не должно создавать дополнительный голос. Правила legacy public/file-only операторов сохранены в `RPCTrustIdentity`.

## 3. Добавить HTTP-профиль

Создать файл в `internal/configuration/crypto/infrastructure/crypto/cryptoprofiles/` по аналогии с `infura.go`, `chainstack.go`, `getblock.go` или `tron_api.go`. Для существующего способа авторизации использовать `pathSuffix`, `pathPrefix` либо платформенный `profiles.HeaderKey`.

`Profile` содержит только необходимые особенности:

- `Authorize` проверяет целевой адрес и формат ключа, затем изменяет переданную копию запроса; исходный запрос остаётся без секрета;
- `Classify` переопределяет общую классификацию только для подтверждённых особенностей ответа; конверт ошибок TRON (`{"Error": …}`) распознаёт `cryptoprofiles.classify`, а не платформа;
- `RetryAt` обрабатывает особый срок повторной попытки, если стандартного `Retry-After` недостаточно;
- `CursorPin` объявляет курсор пагинации (параметр и пути, например TronGrid `fingerprint` на `/v1/`): транспорт привязывает курсор к выдавшему его аккаунту;
- `ResponseError` возвращает безопасную ошибку операции, например отсутствие архивного доступа, без отключения исправного аккаунта;
- `ImportCredentials` нужен только для особого legacy-формата. Обычный импорт использует общий профиль и secret file;
- `Probe` нужен для отдельного протокола интеграции, например CoinGecko ping. EVM/TRON проверяются протокольной проверкой конфигурации `crypto_integration_repo.RPCProbe`, которую `ConnectionProber` получает как `EndpointProbe`.

Профиль не выбирает следующий аккаунт, не выполняет собственные HTTP retries, не меняет quota state и не создаёт отдельный RPC pool. В ошибки не включаются ключи, полные credential URL и тела ответов.

Добавить профиль в `Profiles()` пакета `cryptoprofiles`; интервал pacing берётся из описания провайдера. Реестр неизменяемый после создания; `init()` и глобальная регистрация не используются. Транспорт и `ConnectionProber` получают `cryptoprofiles.Registry()` явно через `Profiles`; без него (nil) доступны только профили платформы.

## 4. UI, импорт и проверка подключения

Форма читает credential fields, формат ключа, обязательность набора и правила извлечения URL из `/system/integrations/providers`. Новый провайдер с уже поддерживаемым форматом не требует веток по его имени на frontend. После изменения DTO использовать генератор контрактов; generated-файлы вручную не редактировать.

CLI import вызывает `cryptoprofiles.ImportRPC`; он определяет провайдера и возвращает только базовый адрес без секрета. Неизвестный адрес допускается только без managed credentials. Для отдельного аккаунта/пула по-прежнему требуется явный mapping.

Проверка RPC-пула требует существующую активную привязку к сети. `ConnectionTarget` платформы передаёт её как `EndpointID`, `URL` и непрозрачные `Attributes` (family, network, role), которые читает только `crypto_integration_repo.RPCProbe`. Она использует UUID этой привязки, family, роль и фактический network identifier. Для EVM проверяется точный chain ID, для TRON — ответ соответствующей FullNode/Solidity роли. Проверка аккаунта без привязки не обращается к произвольно выбранной тестовой сети. Недоступность upstream возвращает безопасный результат проверки.

Новый формат credentials, протокол либо жизненный цикл квоты — отдельное изменение общей возможности платформы. Для ручного срока ключа уже есть общая политика `manual`: её использует dRPC. Она требует ключ и будущую `credentialsExpiresAt` при создании, новый ключ и новую дату при продлении, серверное поколение периода, проверку срока в runtime и credential deadline в transport. Такая политика не имеет автоматического quota reset. Добавление профиля не создаёт новую политику или поддержку chain family автоматически. Правила и пользовательский сценарий: [ручное продление dRPC](../operations/integration-accounts.md#drpc-sepolia-ручное-продление).

## 5. Изменение конфигурации

`RPCConfigurationService` открывает транзакцию, получает общий configuration lock, читает согласованный снимок и вызывает `RPCConfiguration.Plan`. Доменная функция возвращает только изменённые активность и позиции. Репозиторий сохраняет план с проверкой ревизии и штатных security policies; cross-binding constraints проверяются на конечном состоянии транзакции. Аудит записывается до commit. Каталожные изменения используют тот же lock и общие правила покрытия.

`chainconfig.Network.Configuration()` преобразует runtime-модель в чистую `RPCTopology`. Ревизия сохраняет прежние JSON tags и порядок полей, включая decoder suffix TRON; клиенты, resolved secrets и process-local admission scopes исключены. Изменение алгоритма ревизий требует отдельного решения по совместимости.

## 6. Проверки

- Тест полноты реестра требует HTTP-профиль для каждого RPC-провайдера каталога; тест каталога сверяет список провайдеров с миграцией.
- Негативные тесты проверяют чужой host, несовместимую сеть/роль, credential URL, encoded path, неверный ключ и подмену trust group.
- Тест transport проверяет сохранение тела, отсутствие секрета в исходном запросе и штатное переключение аккаунтов.
- Ошибки авторизации, временный throttle, исчерпание квоты и ограничения метода/архива проверяются отдельно.
- Для роли с отдельным ключом проверяются slot и общая квота аккаунта.
- Frontend-тест добавляет описание вымышленного провайдера и проверяет работу существующей формы/нормализатора без новой ветки по имени.
- PostgreSQL-тесты проверяют конкуренцию, атомарность, аудит и сохранение attributes на отдельном временном кластере.

Запустить `go test -race` для изменённых пакетов, интеграционные тесты `integration_repo` и `crypto_integration_repo` (RPC-импорт и транспорт; полный `go run ./cmd/integration-test` включает их, отдельно — с `METAPUS_TEST_INTEGRATIONS_DSN`), `go run ./cmd/http-contracts -check`, frontend typecheck/lint и целевые браузерные сценарии. Общие repository checks выполняются перед завершением изменения.
