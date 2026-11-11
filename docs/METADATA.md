# Структура метаданных Metapus

> **TL;DR:** Объекты текущей конфигурации в терминах 1С: справочники, документы, табличные части, регистры, перечисления, настройки и отчёты.
> **Тип:** Reference
> **Аудитория:** Developer, Analyst, AI-Agent
> **Состояние:** 11 ноября 2026 года; исходные миграции до `00077`, `ExpectedSchemaVersion = 77`. Прежние миграции 77–104 свёрнуты в [00077](../db/migrations/00077_crypto_custody_accounting.sql); «схема 78–104» ниже — исторические номера её разделов. Она добавляет accounting evidence index, receipt/suspense, общий registry для funding, JIT native funding, sweep, treasury, merchant withdrawal и custody loss, отдельное признание network fee, expected-transfer routing, source-bound manual corrections, immutable basis повторного financial reorg сторно и общий SQL hold lifecycle guard. Двойной модели учёта нет: исходящие документы и резервы комиссий признают principal и fee только по финальному независимому доказательству.
> **Связанные:** [ROUTER.md](ROUTER.md), [Базовые сущности](systems/core-layer.md), [Проведение](systems/posting-engine.md).

## Содержание

- [Дерево конфигурации](#дерево-конфигурации)
- [Общие реквизиты и типы](#общие-реквизиты-и-типы)
- [Справочники](#справочники)
- [Подчинённые справочники](#подчинённые-справочники)
- [Документы](#документы)
- [Документы отдельных сценариев](#документы-отдельных-сценариев)
- [Регистры накопления](#регистры-накопления)
- [Регистры сведений](#регистры-сведений)
- [Индекс учётных событий](#индекс-учётных-событий)
- [AML и merchant custody](#aml-и-merchant-custody)
- [Перечисления](#перечисления)
- [Настройки — аналог констант](#настройки--аналог-констант)
- [Отчёты и варианты](#отчёты-и-варианты)
- [Служебные объекты платформы](#служебные-объекты-платформы)
- [Источники и актуализация](#источники-и-актуализация)

Группировка соответствует понятиям конфигуратора 1С. Источник структуры — модели Go, регистрации объектов и SQL-миграции. Это описание текущей реализации, а не проект будущей конфигурации. Состав бизнес-справочников и документов общего реестра приведён полностью; технические регистры и подсистемы платформы перечислены обзорно.

Поле `Attributes` хранит произвольный JSON-объект без схемы в метаданных; пользовательские реквизиты как отдельные поля метаданных не поддерживаются. Формы, команды и права доступа имеют собственные контракты и здесь не разворачиваются в полный каталог.

## Дерево конфигурации

```text
Metapus
├── Справочники (13 объектов общего реестра)
│   ├── Контрагенты
│   ├── Организации
│   ├── Валюты
│   ├── Договоры
│   ├── Блокчейн-сети
│   ├── Токены
│   ├── Мерчанты
│   ├── Кошельки
│   ├── Аккаунты провайдеров ресурсов
│   ├── Источники курсов
│   ├── Аккаунты интеграций
│   ├── Пулы аккаунтов интеграций
│   └── Подключения RPC
├── Подчинённые справочники (отдельные API)
│   ├── API-ключи мерчантов
│   └── Webhook-подписки мерчантов
├── Документы (9 объектов общего реестра)
│   ├── Крипто-инвойс
│   ├── Крипто-платёж
│   ├── Заявка на вывод
│   ├── Крипто-вывод
│   ├── Крипто-свип
│   │   ├── Табличная часть: Кошельки
│   │   └── Коллекция модели: Расходы платформы
│   ├── Казначейский вывод
│   │   └── Коллекция модели: Источники комиссии
│   ├── Сетевая криптооперация
│   ├── Аренда TRON-ресурсов
│   └── Операция баланса провайдера
├── Документы отдельных сценариев
│   ├── Учётная криптокорректировка
│   ├── Корректировка хранения
│   ├── Пополнение сетевой комиссии
│   └── Платёжная ссылка
├── Регистры накопления
│   ├── Взаиморасчёты с контрагентами
│   ├── Криптоостатки кошельков
│   ├── Крипторасчёты с мерчантами
│   ├── Криптокомиссии
│   ├── Расходы платформы
│   ├── Балансы провайдеров
│   └── Излишки и недостачи кошельков
├── Регистры сведений
│   ├── Курсы валют
│   ├── Соответствия валют источникам
│   ├── Настройки свипа по мерчанту и токену
│   ├── Тарифы комиссий
│   ├── Разрешённые адреса выводов
│   ├── Состав пулов интеграций
│   ├── Политики сетей и пулов кошельков
│   └── Технические регистры криптопроцессинга
├── AML и merchant custody
│   ├── Subjects, jobs, reports, decisions и risk projections
│   ├── Principal/fee inventory и operating resources
│   ├── Batches, merchant provisioning и pool policies
│   ├── Baselines, dispatch permits и withdrawal source lines
│   └── Testnet emulator, opening inventory и exact renewal
├── Перечисления
├── Настройки (аналог констант)
├── Отчёты
│   ├── Журнал документов
│   └── Сохранённые варианты
└── Служебные объекты платформы
    ├── Пользователи, роли, профили безопасности
    ├── Автоматизация
    ├── Нумераторы и дополнительные реквизиты
    └── Уведомления, фоновые задания, аудит
```

## Общие реквизиты и типы

Имена в столбце «Поле модели / JSON» соответствуют Go-модели и её тегу `json`. `json:"-"` обозначает внутреннее поле, скрытое при сериализации модели. Фактический HTTP-контракт определяется DTO: присутствие поля в модели само по себе не разрешает его изменение через API. Указатель Go отмечен как nullable в модели; в HTTP поле с omission может отсутствовать вместо null. Точное присутствие и nullability определяют DTO, JSON facade и generated contract, а обязательность ввода — валидация.

| Тип | Значение |
|---|---|
| Ссылка / UUID | `id.ID` или `uuid.UUID`; идентификатор объекта. Основные идентификаторы создаются как UUIDv7 |
| Дата и время | `time.Time`; формат и часовой пояс вывода зависят от контракта |
| Криптосумма | `types.CryptoAmount`; целое число минимальных единиц токена произвольной точности, в финансовых таблицах `NUMERIC(78,0)` |
| Денежная сумма | `types.MinorUnits`; целое число минимальных единиц валюты |
| Десятичное число | `decimal.Decimal`; например, курс `NUMERIC(38,18)` |
| Перечисление | Именованный строковый тип Go; варианты приведены в разделе перечислений |
| Структура / JSON | Составной реквизит или расширяемые данные |

### Реквизиты всех справочников и стандартных документов

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Ссылка | `ID` / `id` | UUID | — |
| Пометка удаления | `DeletionMark` / `deletionMark` | Булево | — |
| Версия | `Version` / `version` | Целое число | — |
| Дополнительные реквизиты | `Attributes` / `attributes` | Дополнительные реквизиты (JSON) | — |

Служебные поля CDC встроены через `CDCFields`:

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Дата удаления CDC | `DeletedAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |
| Идентификатор транзакции CDC | `TxID` | Целое число | Внутреннее поле; JSON скрыт |

### Стандартные реквизиты справочников

Дополняют общие реквизиты. Иерархия «группы и элементы» включена у контрагентов; остальные справочники общего реестра плоские. Наличие `ParentID` и `IsFolder` в базовой модели не включает иерархию автоматически.

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Код | `Code` / `code` | Строка | — |
| Наименование | `Name` / `name` | Строка | — |
| Родитель | `ParentID` / `parentId` | UUID; допускает null в модели | Иерархия включена только у контрагентов |
| Группа | `IsFolder` / `isFolder` | Булево | — |

### Стандартные реквизиты документов

Дополняют общие реквизиты. Организация не входит в базовый тип документа.

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Номер | `Number` / `number` | Строка | — |
| Дата | `Date` / `date` | Дата и время | — |
| Проведен | `Posted` / `posted` | Булево | — |
| Версия проведения | `PostedVersion` / `postedVersion` | Целое число | — |
| Тип основания | `BasisType` / `basisType` | Строка | — |
| Основание | `BasisID` / `basisId` | UUID; допускает null в модели | — |
| Комментарий | `Description` / `description` | Строка | — |

Аудит документа (`BaseDocument`):

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Дата создания | `CreatedAt` / `createdAt` | Дата и время | — |
| Дата изменения | `UpdatedAt` / `updatedAt` | Дата и время | — |
| Автор | `CreatedBy` / `createdBy` | Ссылка → Пользователи | — |
| Изменил | `UpdatedBy` / `updatedBy` | Ссылка → Пользователи | — |

## Справочники

### Контрагенты — `counterparty`

Модель: `Counterparty`. Таблица: `cat_counterparties`. Источник: [model.go](../internal/domain/catalogs/counterparty/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Тип | `Type` / `type` | Перечисление `counterparty.CounterpartyType` | — |
| Юр. форма | `LegalForm` / `legalForm` | Перечисление `counterparty.LegalForm` | — |
| Полное наименование | `FullName` / `fullName` | Строка; допускает null в модели | — |
| ИНН | `INN` / `inn` | Строка; допускает null в модели | — |
| КПП | `KPP` / `kpp` | Строка; допускает null в модели | — |
| ОГРН | `OGRN` / `ogrn` | Строка; допускает null в модели | — |
| Юр. адрес | `LegalAddress` / `legalAddress` | Строка; допускает null в модели | — |
| Факт. адрес | `ActualAddress` / `actualAddress` | Строка; допускает null в модели | — |
| Телефон | `Phone` / `phone` | Строка; допускает null в модели | — |
| Email | `Email` / `email` | Строка; допускает null в модели | — |
| Контактное лицо | `ContactPerson` / `contactPerson` | Строка; допускает null в модели | — |
| Комментарий | `Comment` / `comment` | Строка; допускает null в модели | — |

### Организации — `organization`

Модель: `Organization`. Таблица: `cat_organizations`. Источник: [model.go](../internal/domain/catalogs/organization/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Полное наименование | `FullName` / `fullName` | Строка; допускает null в модели | — |
| ИНН | `INN` / `inn` | Строка; допускает null в модели | — |
| КПП | `KPP` / `kpp` | Строка; допускает null в модели | — |
| ОГРН | `OGRN` / `ogrn` | Строка; допускает null в модели | — |
| Юридический адрес | `LegalAddress` / `legalAddress` | Строка; допускает null в модели | — |
| Фактический адрес | `ActualAddress` / `actualAddress` | Строка; допускает null в модели | — |
| Телефон | `Phone` / `phone` | Строка; допускает null в модели | — |
| Email | `Email` / `email` | Строка; допускает null в модели | — |
| Вебсайт | `Website` / `website` | Строка; допускает null в модели | — |
| Валюта учёта | `BaseCurrencyID` / `baseCurrencyId` | Ссылка → Валюты | — |
| Основная | `IsDefault` / `isDefault` | Булево | — |
| Руководитель | `Director` / `director` | Строка; допускает null в модели | — |
| Главный бухгалтер | `Accountant` / `accountant` | Строка; допускает null в модели | — |
| Логотип | `LogoURL` / `logoUrl` | Строка; допускает null в модели | — |
| Система налогообложения | `TaxSystem` / `taxSystem` | Перечисление `organization.TaxSystem` | — |
| Плательщик НДС | `VatPayer` / `vatPayer` | Булево | — |
| Начало фискального года | `FiscalYearStart` / `fiscalYearStart` | Строка | — |

### Валюты — `currency`

Модель: `Currency`. Таблица: `cat_currencies`. Источник: [model.go](../internal/domain/catalogs/currency/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| ISO-код / тикер | `ISOCode` / `isoCode` | Строка; допускает null в модели | — |
| Цифровой код ISO (3 цифры) | `ISONumericCode` / `isoNumericCode` | Строка; допускает null в модели | — |
| Символ | `Symbol` / `symbol` | Строка; допускает null в модели | — |
| Знаков после запятой | `DecimalPlaces` / `decimalPlaces` | Целое число | — |
| Множитель минимальных единиц | `MinorMultiplier` | Целое число | Внутреннее поле; JSON скрыт |
| Базовая | `IsBase` / `isBase` | Булево | — |
| Страна | `Country` / `country` | Строка; допускает null в модели | — |

### Договоры — `contract`

Модель: `Contract`. Таблица: `cat_contracts`. Источник: [model.go](../internal/domain/catalogs/contract/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Контрагент | `CounterpartyID` / `counterpartyId` | Ссылка → Контрагенты | — |
| Тип | `Type` / `type` | Перечисление `contract.ContractType` | — |
| Валюта | `CurrencyID` / `currencyId` | Ссылка → Валюты; допускает null в модели | — |
| Дата начала | `ValidFrom` / `validFrom` | Дата и время; допускает null в модели | — |
| Дата окончания | `ValidTo` / `validTo` | Дата и время; допускает null в модели | — |
| Срок оплаты (дн.) | `PaymentTermDays` / `paymentTermDays` | Целое число | — |
| Описание | `Description` / `description` | Строка; допускает null в модели | — |

### Блокчейн-сети — `blockchain_network`

Модель: `BlockchainNetwork`. Таблица: `cat_blockchain_networks`. Источник: [model.go](../internal/domain/catalogs/blockchain_network/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Источник RPC (file/database) | `RPCConfigSource` / `rpcConfigSource` | Строка | — |
| Семейство сети | `ChainFamily` / `chainFamily` | Перечисление `crypto.ChainFamily` | — |
| Идентификатор сети | `NetworkIdentifier` / `networkIdentifier` | Строка | — |
| Нативный токен | `NativeTokenSymbol` / `nativeTokenSymbol` | Строка | — |
| Десятичные знаки | `NativeDecimals` / `nativeDecimals` | Целое число | — |
| Подтверждения | `ConfirmationsNeeded` / `confirmationsNeeded` | Целое число | — |
| Режим финальности | `FinalityMode` / `finalityMode` | Перечисление `crypto.FinalityMode` | — |
| Время блока (сек) | `BlockTimeSeconds` / `blockTimeSeconds` | Целое число | — |
| Explorer URL (шаблон) | `ExplorerURL` / `explorerUrl` | Строка | — |
| Активна | `IsActive` / `isActive` | Булево | — |

### Токены — `token`

Модель: `Token`. Таблица: `cat_tokens`. Источник: [model.go](../internal/domain/catalogs/token/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Сеть | `NetworkID` / `networkId` | Ссылка → Блокчейн-сети | — |
| Адрес контракта | `ContractAddress` / `contractAddress` | Строка | — |
| Символ | `Symbol` / `symbol` | Строка | — |
| Десятичные знаки | `DecimalPlaces` / `decimalPlaces` | Целое число | — |
| Стандарт | `Standard` / `tokenStandard` | Перечисление `token.TokenStandard` | — |
| Активен | `IsActive` / `isActive` | Булево | — |
| Валюта | `CurrencyID` / `currencyId` | Ссылка → Валюты; допускает null в модели | — |
| Порог свипа | `SweepThreshold` / `sweepThreshold` | Криптосумма | — |
| Макс. возраст свипа (ч) | `SweepMaxAgeHours` / `sweepMaxAgeHours` | Целое число | — |
| Остаток после свипа | `SweepRetainAmount` / `sweepRetainAmount` | Криптосумма | — |

### Мерчанты — `merchant`

Модель: `Merchant`. Таблица: `cat_merchants`. Источник: [model.go](../internal/domain/catalogs/merchant/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Юр. наименование | `LegalName` / `legalName` | Строка | — |
| Webhook URL | `WebhookURL` / `webhookUrl` | Строка | — |
| Активен | `IsActive` / `isActive` | Булево | — |
| Статус KYB | `KYBStatus` / `kybStatus` | Перечисление `merchant.KYBStatus` | — |

### Кошельки — `wallet`

Модель: `Wallet`. Таблица: `cat_wallets`. Источник: [model.go](../internal/domain/catalogs/wallet/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Сеть | `NetworkID` / `networkId` | Ссылка → Блокчейн-сети | — |
| Мерчант назначения | `MerchantID` / `merchantId` | Ссылка → Мерчанты; допускает null в модели | Текущее назначение не заменяет custody owner |
| Владелец средств | `CustodyMerchantID` / `custodyMerchantId` | Ссылка → Мерчанты; допускает null в модели | Неизменяемый custody owner |
| Адрес выдан | `DepositAssignedAt` / `depositAssignedAt` | Дата и время; допускает null в модели | Persistent assignment |
| Адрес выведен из обращения | `DepositRetiredAt` / `depositRetiredAt` | Дата и время; допускает null в модели | Адрес сохраняется для наблюдения |
| AML display | `RiskDisplay` | `aml.WalletDisplay` | Вычисляемое внутреннее поле, JSON модели скрыт; DTO публикует разрешённую risk-проекцию |
| Адрес | `Address` / `address` | Строка | — |
| Нормализованный ключ адреса | `AddressKey` | Строка | Внутреннее поле; JSON скрыт |
| Деривация | `DerivationPath` / `derivationPath` | Строка | — |
| Vault Key ID | `VaultKeyID` / `vaultKeyId` | Строка | — |
| Уровень | `Tier` / `tier` | Перечисление `wallet.WalletTier` | — |
| Роль | `Role` / `walletRole` | Перечисление `wallet.WalletRole` | — |
| Статус | `Status` / `status` | Перечисление `wallet.WalletStatus` | — |
| Аренда до | `LeasedUntil` / `leasedUntil` | Дата и время; допускает null в модели | — |
| Арендован для | `LeasedForID` / `leasedForId` | Ссылка → Крипто-инвойс; допускает null в модели | — |
| Переиспользование после | `CooldownUntil` / `cooldownUntil` | Дата и время; допускает null в модели | — |
| Активен | `IsActive` / `isActive` | Булево | — |
| Режим аллокации | `AllocationMode` / `allocationMode` | Перечисление `wallet.AllocationMode` | — |
| ID клиента | `CustomerID` / `customerId` | Строка | — |
| Последний свип | `LastSweptAt` / `lastSweptAt` | Дата и время; допускает null в модели | — |
| Активация в сети | `ChainActivationStatus` / `chainActivationStatus` | Перечисление `wallet.ChainActivationStatus` | — |
| Аллокация заблокирована | `AllocationHeld` / `allocationHeld` | Булево | Вычисляемое поле или отдельное хранение |
| Причины блокировки аллокации | `AllocationHoldReasons` / `allocationHoldReasons` | Массив `string` | Вычисляемое поле или отдельное хранение |

### Аккаунты провайдеров ресурсов — `crypto_provider_account`

Модель: `CryptoProviderAccount`. Таблица: `cat_crypto_provider_accounts`. Источник: [model.go](../internal/domain/catalogs/crypto_provider_account/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Провайдер | `Provider` / `provider` | Строка | — |
| Сеть | `NetworkID` / `networkId` | Ссылка → Блокчейн-сети | — |
| Токен баланса | `BalanceTokenID` / `balanceTokenId` | Ссылка → Токены | — |
| Внешний аккаунт | `ExternalAccountRef` / `externalAccountRef` | Строка | — |
| Зашифрованные реквизиты доступа | `CredentialsEnc` | Байты / JSON | Внутреннее поле; JSON скрыт |
| Версия реквизитов доступа | `CredentialsVersion` / `credentialsVersion` | Целое число | — |
| Дата изменения реквизитов доступа | `CredentialsUpdatedAt` / `credentialsUpdatedAt` | Дата и время; допускает null в модели | — |
| Активен | `IsActive` / `isActive` | Булево | — |

Секретные реквизиты доступа записываются через специальные DTO и хранятся в `credentials_enc`; расшифрованные значения не являются обычными читаемыми реквизитами справочника.

### Источники курсов — `rate_source`

Модель: `RateSource`. Таблица: `cat_rate_sources`. Источник: [model.go](../internal/domain/catalogs/rate_source/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Тип источника | `SourceType` / `sourceType` | Перечисление `rate_source.SourceType` | — |
| API URL | `BaseURL` / `baseUrl` | Строка; допускает null в модели | — |
| Пул аккаунтов | `IntegrationPoolID` / `integrationPoolId` | Ссылка → Пулы аккаунтов интеграций; допускает null в модели | — |
| Ключ API | `APIKey` | Строка; допускает null в модели | Внутреннее поле; JSON скрыт; Только запись через специальный контракт |
| Лимит запросов/мин | `RateLimitRPM` / `rateLimitRpm` | Целое число | — |
| Приоритет | `Priority` / `priority` | Целое число | — |
| Активен | `IsActive` / `isActive` | Булево | — |

### Аккаунты интеграций — `integration_account`

Модель: `Account`. Таблица: `cat_integration_accounts`. Источник: [model.go](../internal/domain/integrations/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Провайдер | `Provider` / `provider` | Перечисление `integrations.Provider` | — |
| Внешний аккаунт | `ExternalAccountRef` / `externalAccountRef` | Строка | Сопоставление при импорте и защита от дублей; рабочий выбор аккаунта выполняется по UUID |
| Активен | `IsActive` / `isActive` | Булево | — |
| День обновления квоты | `ResetDay` / `resetDay` | Целое число | — |
| Часовой пояс квоты | `ResetTimezone` / `resetTimezone` | Строка | — |
| Зашифрованные реквизиты доступа | `CredentialsEnc` | Байты / JSON | Внутреннее поле; JSON скрыт |
| Версия ключей | `CredentialsVersion` / `credentialsVersion` | Целое число | Только чтение |
| Дата изменения реквизитов доступа | `CredentialsUpdatedAt` / `credentialsUpdatedAt` | Дата и время; допускает null в модели | Только чтение |
| Срок действия ключей | `CredentialsExpiresAt` / `credentialsExpiresAt` | Дата и время; допускает null | Ручной срок credentials |
| Начало ручного периода | `ManualPeriodStartedAt` / `manualPeriodStartedAt` | Дата и время; допускает null | Только чтение |
| Версия ручного периода | `ManualPeriodVersion` / `manualPeriodVersion` | Целое число | Только чтение |
| Входящие credentials | `PendingCredentials` | Credentials | Временное write-only поле, `db:"-" json:"-"` |

Секретные реквизиты доступа записываются через специальные DTO и хранятся в `credentials_enc`; расшифрованные значения не являются обычными читаемыми реквизитами справочника.

### Пулы аккаунтов интеграций — `integration_pool`

Модель: `Pool`. Таблица: `cat_integration_pools`. Источник: [model.go](../internal/domain/integrations/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Провайдер | `Provider` / `provider` | Перечисление `integrations.Provider` | — |
| Активен | `IsActive` / `isActive` | Булево | — |
| Аккаунты пула | `AccountIDs` / `accountIds` | Массив `id.ID` | Вычисляемое поле или отдельное хранение |

`AccountIDs` — упорядоченный состав пула; хранится отдельно в `sys_integration_pool_accounts`, а не в строке `cat_integration_pools`.

### Подключения RPC — `crypto_rpc_endpoint`

Модель: `RPCEndpoint`. Таблица: `sys_crypto_rpc_endpoints`. Источник: [rpc_endpoint.go](../internal/domain/integrations/rpc_endpoint.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Сеть | `NetworkID` / `networkId` | Ссылка → Блокчейн-сети | — |
| Роль | `Role` / `role` | Строка | — |
| Код провайдера | `ProviderID` / `providerId` | Строка | — |
| Группа доверия | `TrustGroup` / `trustGroup` | Строка | — |
| Адрес RPC | `URL` / `url` | Строка | — |
| Пул аккаунтов | `PoolID` / `poolId` | Ссылка → Пулы аккаунтов интеграций; допускает null в модели | — |
| Имя ключа | `Slot` / `credentialSlot` | Строка | — |
| Приоритет | `Priority` / `priority` | Целое число | — |
| Минимальный интервал (мс) | `MinRequestIntervalMS` / `minRequestIntervalMs` | Целое число | — |
| Отключить keep-alive | `DisableKeepAlives` / `disableKeepAlives` | Булево | — |
| Активен | `IsActive` / `isActive` | Булево | — |

## Подчинённые справочники

Эти объекты принадлежат мерчанту, имеют отдельные API и таблицы, но не включены в `RegisterDefaults` как обычные справочники. Общие реквизиты `entity.Catalog` к ним автоматически не добавляются.

### API-ключи мерчантов

Таблица: `cat_merchant_api_keys`. Источник: [api_key.go](../internal/domain/catalogs/merchant/api_key.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Ссылка | `ID` / `id` | UUID | — |
| Мерчант | `MerchantID` / `merchantId` | Ссылка → Мерчанты | — |
| Наименование | `Name` / `name` | Строка | — |
| Префикс ключа | `KeyPrefix` / `keyPrefix` | Строка | — |
| Хеш ключа | `KeyHash` | Строка | Внутреннее поле; JSON скрыт |
| Разрешённые операции | `Scopes` / `scopes` | Массив `APIKeyScope` | — |
| Активен | `IsActive` / `isActive` | Булево | — |
| Последнее использование | `LastUsedAt` / `lastUsedAt` | Дата и время; допускает null в модели | — |
| Срок действия | `ExpiresAt` / `expiresAt` | Дата и время; допускает null в модели | — |
| Создавший пользователь | `CreatedByUserID` / `createdByUserId` | Ссылка → Пользователи; допускает null в модели | — |
| Дата создания | `CreatedAt` / `createdAt` | Дата и время | — |
| Дата изменения | `UpdatedAt` / `updatedAt` | Дата и время | — |

### Webhook-подписки мерчантов

Таблица: `cat_merchant_webhook_endpoints`. Источник: [merchant_webhook_endpoint.go](../internal/domain/crypto/merchant_webhook_endpoint.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Ссылка | `ID` / `id` | UUID | — |
| Мерчант | `MerchantID` | Ссылка → Мерчанты | Внутреннее поле; JSON скрыт |
| Наименование | `Name` / `name` | Строка | — |
| Адрес | `URL` / `url` | Строка | — |
| Типы событий | `EnabledEvents` / `enabledEvents` | Массив `WebhookEventType` | — |
| Основной | `IsDefault` / `isDefault` | Булево | — |
| Активен | `IsActive` / `isActive` | Булево | — |
| Ссылка на секрет | `SecretRef` | Строка | Внутреннее поле; JSON скрыт |
| Версия секрета | `SecretVersion` / `secretVersion` | Целое число | — |
| Предыдущая ссылка на секрет | `PreviousSecretRef` | Строка | Внутреннее поле; JSON скрыт |
| Предыдущая версия секрета | `PreviousSecretVersion` | Целое число; допускает null в модели | Внутреннее поле; JSON скрыт |
| Предыдущий секрет действует до | `PreviousSecretValidUntil` / `previousSecretValidUntil` | Дата и время; допускает null в модели | — |
| Версия | `Version` / `version` | Целое число | — |
| Дата создания | `CreatedAt` / `createdAt` | Дата и время | — |
| Дата изменения | `UpdatedAt` / `updatedAt` | Дата и время | — |

## Документы

### Крипто-инвойс — `crypto_invoice`

Модель: `CryptoInvoice`. Таблица: `doc_crypto_invoices`. Источник: [model.go](../internal/domain/documents/crypto_invoice/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Публичная ссылка | `PublicRef` / `publicRef` | Строка | — |
| Мерчант | `MerchantID` / `merchantId` | Ссылка → Мерчанты | — |
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Кошелёк | `WalletID` / `walletId` | Ссылка → Кошельки; допускает null в модели | — |
| Ожидаемая сумма | `ExpectedAmount` / `expectedAmount` | Криптосумма | — |
| Полученная сумма | `ReceivedAmount` / `receivedAmount` | Криптосумма | — |
| Подтвержденная сумма | `ConfirmedAmount` / `confirmedAmount` | Криптосумма | — |
| Зачисленная сумма | `CreditedAmount` / `creditedAmount` | Криптосумма | — |
| Сумма переплаты | `OverpaidAmount` / `overpaidAmount` | Криптосумма | — |
| Статус | `Status` / `status` | Перечисление `crypto_invoice.InvoiceStatus` | — |
| Один платёж на инвойс | `SinglePayment` / `singlePayment` | Булево | — |
| Выбранный платёж | `SelectedPaymentID` | Ссылка → Крипто-платёж; допускает null в модели | Внутреннее поле; JSON скрыт |
| Истекает | `ExpiresAt` / `expiresAt` | Дата и время | — |
| Notification URL | `NotificationURL` / `notificationUrl` | Строка | — |
| Return URL | `ReturnURL` / `returnUrl` | Строка | — |
| Метаданные | `Metadata` / `metadata` | Структура / JSON | — |
| Требуемые подтверждения | `RequiredConfirmations` / `requiredConfirmations` | Целое число | — |
| Режим финальности | `FinalityMode` / `finalityMode` | Строка | — |
| Оплачен | `PaidAt` / `paidAt` | Дата и время; допускает null в модели | — |
| Подтвержден | `ConfirmedAt` / `confirmedAt` | Дата и время; допускает null в модели | — |
| Завершен | `CompletedAt` / `completedAt` | Дата и время; допускает null в модели | — |
| Отозван | `ReversedAt` / `reversedAt` | Дата и время; допускает null в модели | — |
| Число просмотров | `ViewCount` | Целое число | Внутреннее поле; JSON скрыт |
| Первый просмотр | `FirstViewedAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |
| Последний просмотр | `LastViewedAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |
| Внешний ID | `ExternalID` / `externalId` | Строка | — |
| Номер заказа | `OrderID` / `orderId` | Строка | — |
| TTL | `TTLMinutes` / `ttlMinutes` | Целое число | — |
| ID клиента | `CustomerID` / `customerId` | Строка | — |
| Источник создания | `CreationSource` / `creationSource` | Перечисление `crypto_invoice.CreationSource` | — |
| Назначение инвойса | `InvoiceKind` | Перечисление `crypto_invoice.InvoiceKind` | Внутреннее поле; JSON скрыт |
| API-ключ | `APIKeyID` / `apiKeyId` | Ссылка → API-ключи мерчантов; допускает null в модели | — |

### Крипто-платёж — `crypto_payment`

Модель: `CryptoPayment`. Таблица: `doc_crypto_payments`. Источник: [model.go](../internal/domain/documents/crypto_payment/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Инвойс | `InvoiceID` / `invoiceId` | Ссылка → Крипто-инвойс | — |
| Мерчант | `MerchantID` / `merchantId` | Ссылка → Мерчанты | — |
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Кошелёк | `WalletID` / `walletId` | Ссылка → Кошельки | — |
| Сеть | `NetworkID` / `networkId` | Ссылка → Блокчейн-сети | — |
| TX Hash | `TxHash` / `txHash` | Строка | — |
| Индекс события | `EventIndex` / `eventIndex` | Целое число | — |
| Тип события | `EventKind` / `eventKind` | Строка | — |
| Доказательство перевода | `VerificationID` / `verificationId` | UUID; допускает null в модели | — |
| Отправитель | `FromAddress` / `fromAddress` | Строка | — |
| Отправитель транзакции | `SourceAddress` / `sourceAddress` | Строка | — |
| Nonce отправителя | `SourceNonce` / `sourceNonce` | Целое число; допускает null в модели | — |
| Замещающая транзакция | `ReplacementTxHash` / `replacementTxHash` | Строка | — |
| Сумма | `Amount` / `amount` | Криптосумма | — |
| Блок | `BlockNumber` / `blockNumber` | Целое число | — |
| Хеш блока | `BlockHash` / `blockHash` | Строка | — |
| Финальность | `FinalityStatus` / `finalityStatus` | Строка | — |
| Каноничность | `Canonicality` / `canonicality` | Строка | — |
| Подтверждения | `Confirmations` / `confirmations` | Целое число | — |
| Требуемые подтверждения | `RequiredConfs` / `requiredConfs` | Целое число | — |
| Статус | `Status` / `status` | Перечисление `crypto_payment.PaymentStatus` | — |
| Комиссия фикс | `FeeFixed` / `feeFixed` | Криптосумма | — |
| Комиссия % | `FeePercentBP` / `feePercentBp` | Целое число | — |
| Мин. комиссия | `FeeMin` / `feeMin` | Криптосумма | — |
| Макс. комиссия | `FeeMax` / `feeMax` | Криптосумма | — |
| Обнаружен | `DetectedAt` / `detectedAt` | Дата и время | — |
| Подтверждён | `ConfirmedAt` / `confirmedAt` | Дата и время; допускает null в модели | — |
| Confirmation worker | `ConfirmationClaimedBy` | Строка | Внутреннее поле; JSON скрыт |
| Confirmation claim time | `ConfirmationClaimedAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |
| Confirmation lease expiry | `ConfirmationLeaseExpiresAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |
| Next confirmation check | `NextConfirmationCheckAt` | Дата и время | Внутреннее поле; JSON скрыт |
| Confirmation attempts | `ConfirmationCheckAttempts` | Целое число | Внутреннее поле; JSON скрыт |
| Last confirmation check | `LastConfirmationCheckedAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |
| Следующая проверка каноничности | `NextCanonicalityCheckAt` | Дата и время | Внутреннее поле; JSON скрыт |
| Последняя проверка каноничности | `LastCanonicalityCheckedAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |
| Попытки проверки каноничности | `CanonicalityCheckAttempts` | Целое число | Внутреннее поле; JSON скрыт |
| Последняя ошибка проверки каноничности | `CanonicalityLastError` | Строка | Внутреннее поле; JSON скрыт |

### Заявка на вывод — `withdrawal_request`

Модель: `WithdrawalRequest`. Таблица: `doc_withdrawal_requests`. Источник: [model.go](../internal/domain/documents/withdrawal_request/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Мерчант | `MerchantID` / `merchantId` | Ссылка → Мерчанты | — |
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Сумма | `Amount` / `amount` | Криптосумма | — |
| Адрес назначения | `DestAddress` / `destAddress` | Строка | — |
| Адрес из белого списка | `AddressID` / `addressId` | UUID | — |
| Статус | `Status` / `status` | Перечисление `withdrawal_request.Status` | — |
| Утвердил | `ApprovedBy` / `approvedBy` | Ссылка → Пользователи; допускает null в модели | — |
| Утверждено | `ApprovedAt` / `approvedAt` | Дата и время; допускает null в модели | — |
| Причина отклонения | `RejectionReason` / `rejectionReason` | Строка | — |
| Крипто-вывод | `WithdrawalID` / `withdrawalId` | Ссылка → Крипто-вывод; допускает null в модели | — |

Объект зарегистрирован через `RegisterCustomDocument`: создание, утверждение, отказ и подписание обслуживаются отдельным сценарием. При создании проведение уменьшает доступный баланс мерчанта; отказ или ошибка возвращают его компенсирующими движениями.

### Крипто-вывод — `crypto_withdrawal`

Модель: `CryptoWithdrawal`. Таблица: `doc_crypto_withdrawals`. Источник: [model.go](../internal/domain/documents/crypto_withdrawal/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Мерчант | `MerchantID` / `merchantId` | Ссылка → Мерчанты | — |
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Исходный кошелёк | `SourceWalletID` / `sourceWalletId` | Ссылка → Кошельки | — |
| Адрес назначения | `DestAddress` / `destAddress` | Строка | — |
| Сумма | `Amount` / `amount` | Криптосумма | — |
| Комиссия сети | `NetworkFee` / `networkFee` | Криптосумма | — |
| Токен комиссии | `NetworkFeeTokenID` / `networkFeeTokenId` | Ссылка → Токены; допускает null в модели | — |
| TX Hash | `TxHash` / `txHash` | Строка | — |
| Статус | `Status` / `status` | Перечисление `crypto_withdrawal.WithdrawalStatus` | — |
| Статус учёта сетевого результата | `SettlementStatus` / `settlementStatus` | Перечисление `outbound_settlement.Status` | — |
| Дата проверки сетевого результата | `SettlementCheckedAt` / `settlementCheckedAt` | Дата и время; допускает null в модели | — |
| Ошибка учёта сетевого результата | `SettlementError` / `settlementError` | Строка | — |
| Обработчик сетевого результата | `SettlementClaimedBy` | Строка | Внутреннее поле; JSON скрыт |
| Начало обработки сетевого результата | `SettlementClaimedAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |
| Срок блокировки обработки | `SettlementLeaseExpiresAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |
| Результат сети | `ChainResult` / `chainResult` | Перечисление `outbound_settlement.ChainResult` | — |
| Использовано Energy | `EnergyUsed` / `energyUsed` | Целое число | — |
| Комиссия Bandwidth | `NetFee` / `netFee` | Криптосумма | — |
| Комиссия Energy | `EnergyFee` / `energyFee` | Криптосумма | — |
| Использовано Bandwidth | `NetUsage` / `netUsage` | Целое число | — |

### Крипто-свип — `crypto_sweep`

Модель: `CryptoSweep`. Таблица: `doc_crypto_sweeps`. Источник: [model.go](../internal/domain/documents/crypto_sweep/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Hot Wallet | `HotWalletID` / `hotWalletId` | Ссылка → Кошельки | — |
| Итого | `TotalAmount` / `totalAmount` | Криптосумма | — |
| Комиссия | `TotalFee` / `totalFee` | Криптосумма | — |
| Статус | `Status` / `status` | Перечисление `crypto_sweep.SweepStatus` | — |

Схема 82: broadcast сохраняет snapshot ресурсных расходов, без principal. `posted` не означает подтверждённый principal; он появляется отдельно при успешном final settlement каждой строки с independent indexed proof. Reverted строка содержит только фактический network fee, `confirmed=false`. Exact AML coverage использует physical movement IDs конкретной строки, без ожидания общего пакета. Generic repost/unpost и новое поколение закрыты.

#### Табличная коллекция «Кошельки» — `lines`

Строки хранятся в `doc_crypto_sweep_lines`.

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Идентификатор строки | `LineID` / `lineId` | UUID | — |
| № | `LineNo` / `lineNo` | Целое число | — |
| Кошелёк | `WalletID` / `walletId` | Ссылка → Кошельки | — |
| Сумма | `Amount` / `amount` | Криптосумма | — |
| Комиссия | `NetworkFee` / `networkFee` | Криптосумма | — |
| TX Hash | `TxHash` / `txHash` | Строка | — |
| Подтверждён | `Confirmed` / `confirmed` | Булево | — |
| Токен сетевой комиссии | `NetworkFeeTokenID` / `networkFeeTokenId` | Ссылка → Токены; допускает null в модели | — |
| Статус учёта сетевого результата | `SettlementStatus` / `settlementStatus` | Перечисление `outbound_settlement.Status` | — |
| Дата проверки сетевого результата | `SettlementCheckedAt` / `settlementCheckedAt` | Дата и время; допускает null в модели | — |
| Ошибка учёта сетевого результата | `SettlementError` / `settlementError` | Строка | — |
| Обработчик сетевого результата | `SettlementClaimedBy` | Строка | Внутреннее поле; JSON скрыт |
| Начало обработки сетевого результата | `SettlementClaimedAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |
| Срок блокировки обработки | `SettlementLeaseExpiresAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |
| Результат сети | `ChainResult` / `chainResult` | Перечисление `outbound_settlement.ChainResult` | — |
| Использовано Energy | `EnergyUsed` / `energyUsed` | Целое число | — |
| Комиссия Bandwidth | `NetFee` / `netFee` | Криптосумма | — |
| Комиссия Energy | `EnergyFee` / `energyFee` | Криптосумма | — |
| Использовано Bandwidth | `NetUsage` / `netUsage` | Целое число | — |

#### Табличная коллекция «Расходы платформы» — `platformCostLines`

Коллекция присутствует в модели и распознаётся инспектором как табличная часть. Отдельная таблица документа для неё не заявлена; хранение и заполнение задаёт репозиторий.

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Сеть | `NetworkID` / `networkId` | Ссылка → Блокчейн-сети | — |
| Токен расхода | `CostTokenID` / `costTokenId` | Ссылка → Токены | — |
| Вид расхода | `CostType` / `costType` | Перечисление `entity.PlatformCostType` | — |
| Провайдер | `Provider` / `provider` | Строка | — |
| Внешний заказ | `ExternalOrderID` / `externalOrderId` | Строка | — |
| Сумма | `Amount` / `amount` | Криптосумма | — |

### Казначейский вывод — `treasury_withdrawal`

Модель: `TreasuryWithdrawal`. Таблица: `doc_treasury_withdrawals`. Источник: [model.go](../internal/domain/documents/treasury_withdrawal/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Исходный кошелёк | `SourceWalletID` / `sourceWalletId` | Ссылка → Кошельки | — |
| Адрес назначения | `DestAddress` / `destAddress` | Строка | — |
| Адрес назначения | `AddressID` / `addressId` | UUID | — |
| Сумма | `Amount` / `amount` | Криптосумма | — |
| Комиссия сети | `NetworkFee` / `networkFee` | Криптосумма | — |
| Токен комиссии | `NetworkFeeTokenID` / `networkFeeTokenId` | Ссылка → Токены; допускает null в модели | — |
| TX Hash | `TxHash` / `txHash` | Строка | — |
| Статус | `Status` / `status` | Перечисление `treasury_withdrawal.Status` | — |
| Утвердил | `ApprovedBy` / `approvedBy` | Ссылка → Пользователи; допускает null в модели | — |
| Утверждено | `ApprovedAt` / `approvedAt` | Дата и время; допускает null в модели | — |
| Причина отказа | `RejectionReason` / `rejectionReason` | Строка | — |
| Причина удержания | `HeldReason` / `heldReason` | Строка | — |
| Статус учёта сетевого результата | `SettlementStatus` / `settlementStatus` | Перечисление `outbound_settlement.Status` | — |
| Дата проверки сетевого результата | `SettlementCheckedAt` / `settlementCheckedAt` | Дата и время; допускает null в модели | — |
| Ошибка учёта сетевого результата | `SettlementError` / `settlementError` | Строка | — |
| Обработчик сетевого результата | `SettlementClaimedBy` | Строка | Внутреннее поле; JSON скрыт |
| Начало обработки сетевого результата | `SettlementClaimedAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |
| Срок блокировки обработки | `SettlementLeaseExpiresAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |
| Результат сети | `ChainResult` / `chainResult` | Перечисление `outbound_settlement.ChainResult` | — |
| Использовано Energy | `EnergyUsed` / `energyUsed` | Целое число | — |
| Комиссия Bandwidth | `NetFee` / `netFee` | Криптосумма | — |
| Комиссия Energy | `EnergyFee` / `energyFee` | Криптосумма | — |
| Использовано Bandwidth | `NetUsage` / `netUsage` | Целое число | — |

Схема 83: broadcast не создаёт physical principal или expense platform revenue. Wallet/revenue reservations сохраняются active до final outcome. Successful settlement атомарно проводит principal, точные fee partitions, actual network fee/cost и consume резервов; revert записывает только actual network fee и освобождает principal/revenue. `posted` на broadcast означает snapshot состояния.

#### Табличная коллекция «Источники комиссии» — `feeLines`

Коллекция присутствует в модели и распознаётся инспектором как табличная часть. Отдельная таблица документа для неё не заявлена; хранение и заполнение задаёт репозиторий.

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Мерчант | `MerchantID` / `merchantId` | Ссылка → Мерчанты | — |
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Вид комиссии | `FeeType` / `feeType` | Перечисление `entity.FeeType` | — |
| Сумма | `Amount` / `amount` | Криптосумма | — |

### Сетевая криптооперация — `crypto_network_operation`

Модель: `CryptoNetworkOperation`. Таблица: `doc_crypto_network_operations`. Источник: [model.go](../internal/domain/documents/crypto_network_operation/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Операция | `OperationType` / `operationType` | Перечисление `crypto_network_operation.OperationType` | — |
| Статус | `Status` / `status` | Перечисление `crypto_network_operation.Status` | — |
| Сеть | `NetworkID` / `networkId` | Ссылка → Блокчейн-сети | — |
| Целевой кошелёк | `TargetWalletID` / `targetWalletId` | Ссылка → Кошельки | — |
| Адрес назначения | `TargetAddress` / `targetAddress` | Строка | — |
| Кошелёк списания | `SourceWalletID` / `sourceWalletId` | Ссылка → Кошельки | — |
| Токен расхода | `CostTokenID` / `costTokenId` | Ссылка → Токены | — |
| Сумма расхода | `CostAmount` / `costAmount` | Криптосумма | — |
| Хеш транзакции | `TxHash` / `txHash` | Строка | — |
| Сетевой расчёт | `ChainSettlementID` / `chainSettlementId` | UUID; допускает null в модели | — |
| Дата финальности | `FinalizedAt` / `finalizedAt` | Дата и время; допускает null в модели | — |
| Редактирование разрешено | `ManualEditAllowed` / `manualEditAllowed` | Булево | — |

### Аренда TRON-ресурсов — `crypto_resource_rental`

Модель: `CryptoResourceRental`. Таблица: `doc_crypto_resource_rentals`. Источник: [model.go](../internal/domain/documents/crypto_resource_rental/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Аккаунт провайдера | `ProviderAccountID` / `providerAccountId` | Ссылка → Аккаунты провайдеров ресурсов | — |
| Сеть | `NetworkID` / `networkId` | Ссылка → Блокчейн-сети | — |
| Кошелёк | `ReceiveWalletID` / `receiveWalletId` | Ссылка → Кошельки | — |
| Токен расхода | `CostTokenID` / `costTokenId` | Ссылка → Токены | — |
| Ресурс | `ResourceType` / `resourceType` | Строка | — |
| Режим | `RentalMode` / `rentalMode` | Строка | — |
| Период | `RentalPeriod` / `rentalPeriod` | Строка | — |
| Запрошено | `RequestedAmount` / `requestedAmount` | Целое число | — |
| Делегировано | `DelegatedAmount` / `delegatedAmount` | Целое число | — |
| Зарезервировано | `ReservedAmount` / `reservedAmount` | Целое число | — |
| Потреблено | `ConsumedAmount` / `consumedAmount` | Целое число | — |
| Стоимость | `GrossCostAmount` / `grossCostAmount` | Криптосумма | — |
| Возврат | `RefundAmount` / `refundAmount` | Криптосумма | — |
| Чистая стоимость | `NetCostAmount` / `netCostAmount` | Криптосумма | — |
| Распределено | `AllocatedCostAmount` / `allocatedCostAmount` | Криптосумма | — |
| Не использовано | `UnusedCostAmount` / `unusedCostAmount` | Криптосумма | — |
| Провайдер | `Provider` / `provider` | Строка | — |
| Внешний ключ | `OutTradeNo` / `outTradeNo` | Строка | — |
| Заказ провайдера | `ExternalSerial` / `externalSerial` | Строка | — |
| Статус | `Status` / `status` | Перечисление `crypto_resource_rental.Status` | — |
| Учет стоимости | `CostStatus` / `costStatus` | Перечисление `crypto_resource_rental.CostStatus` | — |
| Баланс после | `ProviderBalanceAfter` / `providerBalanceAfter` | Криптосумма; допускает null в модели | — |
| Запрос провайдеру | `RequestJSON` | Байты / JSON | Внутреннее поле; JSON скрыт |
| Ответ провайдера | `ResponseJSON` | Байты / JSON | Внутреннее поле; JSON скрыт |
| Ошибка | `ErrorMessage` / `errorMessage` | Строка | — |
| Дата делегирования | `DelegatedAt` / `delegatedAt` | Дата и время; допускает null в модели | — |
| Дата активации | `ActivatedAt` / `activatedAt` | Дата и время; допускает null в модели | — |
| Дата запроса возврата ресурса | `ReclaimRequestedAt` / `reclaimRequestedAt` | Дата и время; допускает null в модели | — |
| Срок действия | `ExpiresAt` / `expiresAt` | Дата и время; допускает null в модели | — |
| Дата закрытия | `ClosedAt` / `closedAt` | Дата и время; допускает null в модели | — |
| Обработчик | `ClaimedBy` | Строка | Внутреннее поле; JSON скрыт |
| Начало обработки | `ClaimedAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |
| Срок блокировки обработки | `LeaseExpiresAt` | Дата и время; допускает null в модели | Внутреннее поле; JSON скрыт |

### Операция баланса провайдера — `crypto_provider_balance_operation`

Модель: `CryptoProviderBalanceOperation`. Таблица: `doc_crypto_provider_balance_operations`. Источник: [model.go](../internal/domain/documents/crypto_provider_balance_operation/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Аккаунт провайдера | `ProviderAccountID` / `providerAccountId` | Ссылка → Аккаунты провайдеров ресурсов | — |
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Наблюдение баланса | `ProviderObservationID` / `providerObservationId` | UUID; допускает null в модели | — |
| Кошелёк-источник | `SourceWalletID` / `sourceWalletId` | Ссылка → Кошельки; допускает null в модели | — |
| Операция | `OperationType` / `operationType` | Строка | — |
| Направление | `RecordType` / `recordType` | Перечисление `entity.RecordType` | — |
| Сумма | `Amount` / `amount` | Криптосумма | — |
| TX Hash | `TxHash` / `txHash` | Строка | — |
| Внешнее основание | `ExternalRef` / `externalRef` | Строка | — |
| Подтверждающие данные | `Evidence` | Байты / JSON | Внутреннее поле; JSON скрыт |
| Статус | `Status` / `status` | Строка | — |
| Утвердил | `ApprovedBy` / `approvedBy` | Ссылка → Пользователи; допускает null в модели | — |
| Дата одобрения | `ApprovedAt` / `approvedAt` | Дата и время; допускает null в модели | — |
| Причина одобрения | `ApprovalReason` / `approvalReason` | Строка | — |
| Отклонил | `RejectedBy` / `rejectedBy` | Ссылка → Пользователи; допускает null в модели | — |
| Дата отклонения | `RejectedAt` / `rejectedAt` | Дата и время; допускает null в модели | — |
| Причина отклонения | `RejectionReason` / `rejectionReason` | Строка | — |
| Режим решения | `DecisionMode` / `decisionMode` | Перечисление `crypto_provider_balance_operation.DecisionMode` | — |
| Версия политики решения | `DecisionPolicyVersion` / `decisionPolicyVersion` | Целое число | — |

## Документы отдельных сценариев

Следующие объекты хранятся в таблицах `doc_*`, но не зарегистрированы как стандартные документы в общем реестре. Их реквизиты описываются собственными моделями и SQL; использование базовых реквизитов `entity.Document` указано для каждого соответствующего объекта.

### Физическое криптопоступление — `crypto_custody_receipt`

Модель: `CryptoCustodyReceipt`, включает `entity.Document`. Таблица: `doc_crypto_custody_receipts`. Источники: [модель](../internal/domain/documents/crypto_custody_receipt/model.go), [intake service](../internal/application/cryptoapp/receipt_intake_service.go), [миграция 78](../db/migrations/00077_crypto_custody_accounting.sql).

| Реквизит | Поле модели / JSON | Тип / назначение |
|---|---|---|
| Сеть, кошелёк, актив | `NetworkID` / `networkId`, `WalletID` / `walletId`, `TokenID` / `tokenId` | UUID-ссылки, связанные с proof |
| Владелец custody | `CustodyMerchantID` / `custodyMerchantId` | Неизменяемый snapshot; при отсутствии мерчанта поле опущено |
| Доказательство | `VerificationID` / `verificationId` | Persisted transfer verification; совпадает с document basis |
| Включение в блок | `OccurrenceID` / `occurrenceId` | Accounting occurrence |
| Признание principal | `PostingID` / `postingId` | Posting owner одного logical effect |
| Полная сумма | `Amount` / `amount` | CryptoAmount, целые minor units |
| Строки регистров | `CustodyLineID` / `custodyLineId`, `SuspenseLineID` / `suspenseLineId` | Стабильные UUID физических и suspense movements |
| Причина | `ReasonCode` / `reasonCode` | Закрытый набор reason codes |

Документ создаётся и проводится внутренним сервисом в одном коммите с recognition, audit и outbox. Он не зарегистрирован в публичном API/UI или automatic watcher. Требуется отключённая сеть без незавершённых отправок; после receipt её включение и legacy movements по этому wallet/token блокируются до будущего cutover. Merchant/fee movements, modify/repost/unpost запрещены. Reason codes: `no_allocation`, `wrong_token`, `timestamp_before_lease`, `retired_deposit_address`, `additional_single_payment`, `dust`, `treasury_unidentified`, `matched_receipt`. Код причины не распределяет средства и не задаёт AML-риск.

### Учётная криптокорректировка — `crypto_adjustment`

Модель: `FinancialAdjustment`. Таблица: `doc_crypto_adjustments`. Источники: [domain-модель](../internal/domain/crypto/financial_adjustment.go), [application service](../internal/application/cryptoapp/financial_adjustment_service.go).

Схема 88 добавляет immutable `basis_kind` (`payment_attribution` для режима `payment_reversal`, `wallet_variance` для `wallet_balance`) и внутренний `proposal_txid` для замораживания sources. API возвращает `basisKind`. Basis выводится из режима и источника, не из пользовательского evidence/tx hash. Проведённая корректировка требует exact financial set и `financial_correction_posted` outbox; chain effect она не занимает. [Подробный контракт](reference/crypto-custody-writers.md#16-основание-ручных-корректировок).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Ссылка | `ID` / `id` | UUID | — |
| Крипто-платёж | `PaymentID` / `paymentId` | Ссылка → Крипто-платёж | — |
| Версия проведения | `PostedVersion` / `postedVersion` | Целое число | — |
| Режим корректировки | `Mode` / `mode` | Строка | — |
| Кошелёк | `WalletID` / `walletId` | Ссылка → Кошельки; допускает null в модели | — |
| Токен | `TokenID` / `tokenId` | Ссылка → Токены; допускает null в модели | — |
| Направление | `Direction` / `direction` | Строка | — |
| Сумма | `Amount` / `amount` | Строка | — |
| Режим согласования | `DecisionMode` / `decisionMode` | Строка | — |
| Код причины | `ReasonCode` / `reasonCode` | Строка | — |
| Подтверждающие данные | `Evidence` / `evidence` | JSON | — |
| Инициатор | `MakerID` / `makerId` | Ссылка → Пользователи | — |
| Согласующий | `CheckerID` / `checkerId` | Ссылка → Пользователи; допускает null в модели | — |
| Статус | `Status` / `status` | Строка | — |
| Причина решения | `DecisionReason` / `decisionReason` | Строка | — |
| Дата создания | `CreatedAt` / `createdAt` | Дата и время | — |
| Дата решения | `DecidedAt` / `decidedAt` | Дата и время; допускает null в модели | — |

Режимы: `payment_reversal` — полное или частичное сторно движений платежа; `wallet_balance` — корректировка остатка кошелька. Согласование: `two_person` или `direct`. Статусы: `pending_approval`, `posted`, `rejected`. В режиме `wallet_balance` поля `payment_id` и `posted_version` в БД пусты; модель чтения подставляет нулевые значения.

Выбранные исходные движения хранятся в `reg_crypto_adjustment_sources`: `adjustment_id`, `source_register`, `source_line_id`, `amount`. Расширенная модель строки для просмотра:

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Регистр | `Register` / `register` | Строка | — |
| Идентификатор строки | `LineID` / `lineId` | UUID | — |
| Сумма | `Amount` / `amount` | Строка | — |
| Уже сторнировано | `Reversed` / `reversed` | Строка | — |
| Доступно для сторно | `Available` / `available` | Строка | — |
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Измерение | `Dimension` / `dimension` | Строка | — |

### Корректировка хранения — `crypto_custody_adjustment`

Модель: `CryptoCustodyAdjustment`. Таблица: `doc_crypto_custody_adjustments`. Источник: [external_outflow.go](../internal/domain/crypto/external_outflow.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Ссылка | `ID` | UUID | — |
| Внешняя исходящая операция | `ExternalOutflowID` | UUID | — |
| Сеть | `NetworkID` | Ссылка → Блокчейн-сети | — |
| Кошелёк | `WalletID` | Ссылка → Кошельки | — |
| Токен | `TokenID` | Ссылка → Токены | — |
| Сумма | `Amount` | Криптосумма | — |
| Статус | `Status` | Строка | — |
| Инициатор | `MakerID` | Ссылка → Пользователи | — |
| Принявший решение | `DecisionBy` | Ссылка → Пользователи; допускает null в модели | — |
| Причина решения | `DecisionReason` | Строка | — |
| Версия проведения | `PostedVersion` | Целое число | — |
| Дата создания | `CreatedAt` | Дата и время | — |
| Дата решения | `DecidedAt` | Дата и время; допускает null в модели | — |

### Пополнение сетевой комиссии — `crypto_native_funding`

Модель: `NativeFunding`. Таблица: `doc_crypto_native_fundings`. Источник: [native_funding.go](../internal/domain/crypto/native_funding.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Ссылка | `ID` | UUID | — |
| Основание | `BasisID` | UUID | — |
| Сеть | `NetworkID` | Ссылка → Блокчейн-сети | — |
| Нативный токен | `NativeTokenID` | Ссылка → Токены | — |
| Кошелёк-источник | `SourceWalletID` | Ссылка → Кошельки | — |
| Кошелёк-получатель | `DestinationWalletID` | Ссылка → Кошельки | — |
| Тип основания | `BasisType` | Строка | — |
| Номер попытки | `Attempt` | Целое число | — |
| Сумма | `Amount` | Криптосумма | — |
| Резерв комиссии | `FeeReservationID` | UUID; допускает null в модели | — |
| Разрешение на подпись | `SigningAuthorizationID` | UUID; допускает null в модели | — |
| Отпечаток неподписанной транзакции | `UnsignedFingerprint` | Строка | — |
| Хеш транзакции | `TxHash` | Строка | — |
| Статус | `Status` | Строка | — |
| Последняя ошибка | `LastError` | Строка | — |

Модель содержит рабочие реквизиты перевода. Служебные поля хранения и состояния определены также в [миграции 00041](../db/migrations/00041_multichain_crypto_core.sql).

### Платёжная ссылка — `payment_link`

Таблица: `doc_payment_links`. Многоразовый шаблон, создающий крипто-инвойс при обращении по публичной ссылке. Полная структура хранения определена в [миграции 00029](../db/migrations/00029_payment_links.sql); рабочая проекция — `RedeemablePaymentLink` в [checkout_page_service.go](../internal/application/cryptoapp/checkout_page_service.go).

| Группа | Реквизиты |
|---|---|
| Идентификация | `id` — UUID; `public_ref` — публичный идентификатор; `deletion_mark` — пометка удаления; `version` — версия |
| Получатель и сумма | `merchant_id` → Мерчанты; `token_id` → Токены; `amount` — криптосумма; `description` — строка |
| Использование | `reusable` — булево; `max_uses`, `current_uses` — целые; `status` — `active` / `disabled`; `ttl_minutes` — срок инвойса в минутах |
| Аудит | `created_by`, `created_at`, `updated_at`, `_deleted_at`, `_txid` |

## Регистры накопления

Измерения задают разрез учёта, ресурс хранит сумму, реквизиты дополняют движение. Встроенные типы движений находятся в [register.go](../internal/core/entity/register.go) и [crypto_register.go](../internal/core/entity/crypto_register.go).

| Регистр | Измерения | Ресурс | Дополнительные реквизиты | Таблицы |
|---|---|---|---|---|
| Взаиморасчёты с контрагентами | Контрагент, договор, валюта | Сумма (`MinorUnits`) | Общие реквизиты движения | `reg_settlement_movements`, `reg_settlement_balances` |
| Криптоостатки кошельков | Кошелёк, токен | Сумма (`CryptoAmount`) | Общие реквизиты движения | `reg_crypto_balance_movements`, `reg_crypto_balance_balances` |
| Крипторасчёты с мерчантами | Мерчант, токен | Сумма (`CryptoAmount`) | Общие реквизиты движения | `reg_crypto_merchant_balance_movements`, `reg_crypto_merchant_balance_balances` |
| Криптокомиссии | Мерчант, токен, вид комиссии | Сумма (`CryptoAmount`) | Общие реквизиты движения | `reg_crypto_fee_movements`, `reg_crypto_fee_balances` |
| Расходы платформы | Сеть, токен операции, токен расхода, вид расхода, назначение, провайдер | Сумма (`CryptoAmount`) | Внешний заказ | `reg_crypto_platform_cost_movements` |
| Балансы провайдеров | Аккаунт провайдера, токен | Сумма (`CryptoAmount`) | Операция, внешнее основание | `reg_crypto_provider_balance_movements` |
| Излишки и недостачи кошельков | Кошелёк, токен, вид расхождения (`surplus` / `shortage`) | Сумма `NUMERIC(78,0)` | Корректировка, период, идентификатор строки | `reg_crypto_wallet_variance_movements` |

Последний регистр имеет собственную SQL-структуру из [миграции 00060](../db/migrations/00060_crypto_adjustment_modes.sql), без встраивания `MovementBase`. Регистр взаиморасчётов реализован на уровне модели и сервиса; стандартных документов-регистраторов для него в текущем `RegisterDefaults` нет.

### Общие реквизиты движения — `MovementBase`

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Идентификатор строки | `LineID` / `lineId` | UUID | — |
| Регистратор | `RecorderID` / `recorderId` | UUID | — |
| Тип регистратора | `RecorderType` / `recorderType` | Строка | — |
| Версия регистратора | `RecorderVersion` / `recorderVersion` | Целое число | — |
| Период | `Period` / `period` | Дата и время | — |
| Вид движения | `RecordType` / `recordType` | Перечисление `entity.RecordType` | — |
| Дата создания | `CreatedAt` / `createdAt` | Дата и время | — |

В финансовых SQL-регистрах есть дополнительные поля, отсутствующие в базовой Go-модели, в том числе `reversal_of_line_id` у криптоостатков, расчётов с мерчантами и комиссий. Изменение финансового результата выполняется новыми компенсирующими движениями; правила неизменности и частичного сторно заданы миграциями [00047](../db/migrations/00047_crypto_financial_movements_append_only.sql) и [00060](../db/migrations/00060_crypto_adjustment_modes.sql).

### Реквизиты конкретных движений

#### `SettlementMovement`

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Контрагент | `CounterpartyID` / `counterpartyId` | Ссылка → Контрагенты | — |
| Договор | `ContractID` / `contractId` | Ссылка → Договоры; допускает null в модели | — |
| Валюта | `CurrencyID` / `currencyId` | Ссылка → Валюты | — |
| Сумма | `Amount` / `amount` | Денежная сумма | — |

#### `CryptoBalanceMovement`

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Кошелёк | `WalletID` / `walletId` | Ссылка → Кошельки | — |
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Сумма | `Amount` / `amount` | Криптосумма | — |

#### `CryptoMerchantBalanceMovement`

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Мерчант | `MerchantID` / `merchantId` | Ссылка → Мерчанты | — |
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Сумма | `Amount` / `amount` | Криптосумма | — |

#### `CryptoFeeMovement`

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Мерчант | `MerchantID` / `merchantId` | Ссылка → Мерчанты | — |
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Вид комиссии | `FeeType` / `feeType` | Перечисление `entity.FeeType` | — |
| Сумма | `Amount` / `amount` | Криптосумма | — |

#### `CryptoPlatformCostMovement`

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Сеть | `NetworkID` / `networkId` | Ссылка → Блокчейн-сети | — |
| Токен операции | `OperationTokenID` / `operationTokenId` | Ссылка → Токены; допускает null в модели | — |
| Токен расхода | `CostTokenID` / `costTokenId` | Ссылка → Токены | — |
| Вид расхода | `CostType` / `costType` | Перечисление `entity.PlatformCostType` | — |
| Назначение расхода | `Purpose` / `purpose` | Перечисление `entity.PlatformCostPurpose` | — |
| Провайдер | `Provider` / `provider` | Строка | — |
| Внешний заказ | `ExternalOrderID` / `externalOrderId` | Строка | — |
| Сумма | `Amount` / `amount` | Криптосумма | — |

#### `CryptoProviderBalanceMovement`

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Аккаунт провайдера | `ProviderAccountID` / `providerAccountId` | Ссылка → Аккаунты провайдеров ресурсов | — |
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Вид операции | `OperationType` / `operationType` | Строка | — |
| Внешнее основание | `ExternalRef` / `externalRef` | Строка | — |
| Сумма | `Amount` / `amount` | Криптосумма | — |

### Основные документы-регистраторы

| Документ | Регистры, предусмотренные моделью проведения |
|---|---|
| Крипто-инвойс | Финансовые движения не формирует |
| Крипто-платёж | Криптоостатки кошельков; крипторасчёты с мерчантами; криптокомиссии |
| Заявка на вывод | Крипторасчёты с мерчантами |
| Крипто-вывод | Криптоостатки кошельков; криптокомиссии |
| Крипто-свип | Криптоостатки кошельков; расходы платформы |
| Казначейский вывод | Криптоостатки кошельков; криптокомиссии |
| Сетевая криптооперация | Криптоостатки кошельков; расходы платформы |
| Аренда TRON-ресурсов | Балансы провайдеров; расходы платформы |
| Операция баланса провайдера | Балансы провайдеров; криптоостатки кошельков, если предусмотрено операцией |

Конкретное движение зависит от состояния документа и операции; наличие источника движений не означает запись во все перечисленные регистры при каждом сохранении. Дополнительный учёт сетевых комиссий и корректировок выполняют специализированные сервисы.

## Регистры сведений

Термин «регистр сведений» здесь описывает назначение данных. Многие таблицы `reg_*` обслуживаются специализированными API и не публикуются отдельным объектом `/api/v1/meta`.

### Курсы валют

Таблица: `reg_exchange_rates`. Разрез: валюта, базовая валюта, источник; история неизменяемых котировок включает время провайдера и версии. Формула: `Multiplier × Currency = Rate × BaseCurrency`. Источник: [model.go](../internal/domain/registers/exchange_rate/model.go), [миграция 00023](../db/migrations/00023_crypto_invoice_and_register.sql).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Ссылка | `ID` / `id` | UUID | — |
| Валюта | `Currency` / `currency` | Структура `CurrencyRef` | — |
| Базовая валюта | `BaseCurrency` / `baseCurrency` | Структура `CurrencyRef` | — |
| Источник | `Source` / `source` | Структура `SourceRef` | — |
| Курс | `Rate` / `rate` | Десятичное число | — |
| Кратность | `Multiplier` / `multiplier` | Целое число | — |
| Дата котировки у провайдера | `ProviderUpdatedAt` / `providerUpdatedAt` | Дата и время | — |
| Дата получения | `ReceivedAt` / `receivedAt` | Дата и время | — |
| Дата записи | `RecordedAt` / `recordedAt` | Дата и время | — |
| Версия | `Version` / `version` | Целое число | — |
| Версия соответствия валюты | `MappingVersion` / `mappingVersion` | Целое число | — |
| Попытка загрузки | `FeedAttemptID` / `feedAttemptId` | UUID | — |

В модели чтения `Currency`, `BaseCurrency`, `Source` — вложенные представления ссылок с названием и кодом; в БД им соответствуют `currency_id`, `base_currency_id`, `rate_source_id`. Котировка также содержит служебные `_txid` и `_deleted_at`.

### Соответствия валют источникам

Таблица: `reg_rate_source_mappings`. Составной ключ: валюта, базовая валюта, источник. Источник: [model.go](../internal/domain/registers/exchange_rate/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Валюта | `Currency` / `currency` | Структура `CurrencyRef` | — |
| Базовая валюта | `BaseCurrency` / `baseCurrency` | Структура `CurrencyRef` | — |
| Источник курсов | `SourceID` / `sourceId` | UUID | — |
| Внешний идентификатор | `ExternalID` / `externalId` | Строка | — |
| Активен | `IsActive` / `isActive` | Булево | — |
| Версия | `Version` / `version` | Целое число | — |

### Настройки свипа по мерчанту и токену

Таблица: `reg_merchant_token_config`. Измерения: мерчант, токен. Пустое значение настройки означает отсутствие индивидуального переопределения. Источник: [sweep_config.go](../internal/domain/crypto/sweep_config.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Мерчант | `MerchantID` / `merchantId` | Ссылка → Мерчанты | — |
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Порог свипа | `SweepThreshold` / `sweepThreshold` | Криптосумма; допускает null в модели | — |
| Максимальный возраст свипа, ч | `SweepMaxAgeHours` / `sweepMaxAgeHours` | Целое число; допускает null в модели | — |
| Остаток после свипа | `SweepRetainAmount` / `sweepRetainAmount` | Криптосумма; допускает null в модели | — |

### Тарифы комиссий

Таблица: `reg_fee_schedule`. Измерения: мерчант, токен, направление операции. Пустой мерчант задаёт общий тариф. `PercentBP`: 100 базисных пунктов = 1%. Источник: [fee_config.go](../internal/domain/crypto/fee_config.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Мерчант | `MerchantID` / `merchantId` | Ссылка → Мерчанты; допускает null в модели | — |
| Токен | `TokenID` / `tokenId` | Ссылка → Токены | — |
| Направление | `Direction` / `direction` | Перечисление `crypto.FeeDirection` | — |
| Фиксированная комиссия | `FixedFee` / `fixedFee` | Криптосумма | — |
| Процент комиссии в базисных пунктах | `PercentBP` / `percentBp` | Целое число | — |
| Минимальная комиссия | `MinFee` / `minFee` | Криптосумма | — |
| Максимальная комиссия | `MaxFee` / `maxFee` | Криптосумма | — |
| Дата изменения | `UpdatedAt` / `updatedAt` | Дата и время | — |

### Остальные прикладные и технические регистры

Ниже приведены основные ключи и данные; полный состав технических колонок находится в миграциях и моделях подсистем.

| Объект | Основные ключи / данные | Хранение / источник |
|---|---|---|
| Разрешённые адреса выводов мерчанта | Мерчант, сеть, адрес, наименование, разрешение использования | `reg_withdrawal_addresses`; [00027](../db/migrations/00027_withdrawal_sweep.sql) |
| Адреса казначейских выводов | Сеть, адрес, наименование, активность, автор | `reg_treasury_withdrawal_addresses`; [00032](../db/migrations/00032_treasury_withdrawals_reconciliation.sql) |
| Состав пула интеграций | Пул, аккаунт, позиция | `sys_integration_pool_accounts`; [00012](../db/migrations/00012_sys_service_accounts.sql) |
| Состояние интеграций | Аккаунт / пул, квоты, состояние выбора | `sys_integration_account_state`, `sys_integration_pool_state`; [00012](../db/migrations/00012_sys_service_accounts.sql) |
| Политики сетей | Сеть, лимиты комиссии, пополнение комиссии, внешние ресурсы, повторы, блокировка, версия | `sys_crypto_network_policies`, `sys_crypto_external_resource_policies`; [network_policy.go](../internal/domain/crypto/network_policy.go) |
| Политики пулов кошельков | Сеть, целевой размер пула, условия активации и выдачи | `sys_crypto_wallet_pool_policies`; [wallet_pool.go](../internal/domain/crypto/wallet_pool.go) |
| Индивидуальные условия выдачи кошельков | Мерчант, сеть, переопределения лимитов | `reg_merchant_wallet_allocation_overrides`; [wallet_pool.go](../internal/domain/crypto/wallet_pool.go) |
| Выдача адресов | Кошелёк, мерчант, инвойс / клиент, сроки и причина закрытия | `reg_crypto_wallet_allocations`; [wallet_allocation.go](../internal/domain/crypto/wallet_allocation.go) |
| Входящие события и доказательства переводов | Сеть, транзакция, событие, блок, проверка, результат сопоставления | `reg_crypto_inbound_chain_events`, `reg_crypto_chain_observations`, `reg_crypto_transfer_verifications`; [00043](../db/migrations/00043_crypto_address_reuse_cooldown.sql), [00050](../db/migrations/00050_crypto_chain_observation_inbox.sql) |
| Активации и ресурсы | Кошелёк, операция, заказ аренды, выделенные ресурсы, состояние | `reg_crypto_account_activations`, `reg_crypto_resource_allocations`, `reg_crypto_resource_allocation_events`; [00034](../db/migrations/00034_tron_wallet_pre_activation.sql), [00036](../db/migrations/00036_crypto_resource_rentals.sql) |
| Резервы | Документ-основание, кошелёк / токен, сумма, статус, срок | `reg_crypto_balance_reservations`, `reg_crypto_revenue_reservations`, `reg_crypto_fee_reservations`; [00030](../db/migrations/00030_crypto_outbound_execution_guards.sql), [00032](../db/migrations/00032_treasury_withdrawals_reconciliation.sql), [00041](../db/migrations/00041_multichain_crypto_core.sql) |
| Согласования и подпись | Намерение перевода, решение, подписант, авторизация, задание подписи | `reg_money_movement_intents`, `reg_money_movement_approvals`, `reg_money_movement_signing_jobs`, `reg_crypto_signing_authorizations`; [00031](../db/migrations/00031_crypto_signing_authorizations.sql), [00038](../db/migrations/00038_money_movement_approval_policy.sql), [00053](../db/migrations/00053_withdrawal_approval_signing_jobs.sql) |
| Сетевые расчёты | Транзакция, итог исполнения, компоненты комиссии | `reg_crypto_chain_settlements`, `reg_crypto_chain_settlement_components`; [00041](../db/migrations/00041_multichain_crypto_core.sql) |
| Сверка и инциденты | Запуск сверки, кошелёк / токен, наблюдение, расхождение, блокировка | `reg_crypto_reconciliation_runs`, `reg_crypto_reconciliation_items`, `reg_crypto_reconciliation_observations`, `reg_crypto_wallet_allocation_holds`; [00032](../db/migrations/00032_treasury_withdrawals_reconciliation.sql), [00035](../db/migrations/00035_crypto_reconciliation_incidents.sql) |
| Внешние исходящие операции | Сеть, кошелёк, транзакция, сумма, классификация и решения | `reg_crypto_external_outflows`, `reg_crypto_external_outflow_actions`; [external_outflow.go](../internal/domain/crypto/external_outflow.go) |

## Индекс учётных событий

Источник: `db/migrations/00077_crypto_accounting_effect_identity.sql`, `internal/domain/crypto/accounting_effect.go`.

| Таблица | Содержимое | Ограничения |
|---|---|---|
| reg_crypto_accounting_effects | Network/family/tx, kind, transaction-local ordinal, полный TRON trace hash, logical key | UNIQUE logical key и natural identity; ключ вычисляется SQL по versioned preimage |
| reg_crypto_accounting_effect_occurrences | Logical effect, block height/hash, нормализованные economics и amount | Один payload на effect/block; данные неизменяемы |
| reg_crypto_accounting_effect_evidence | Persisted transfer verification → occurrence | Один proof не перепривязывается; exact-evidence guard и deferred coverage |

Это неизменяемый индекс доказательств. Он не создаёт custody movements, не определяет текущую canonical generation и не разрешает merchant credit/dispatch. Исторические proofs без accountingIdentity остаются неизменными. Детали: [канонические контракты](reference/crypto-canonical-contracts.md), [карта writers](reference/crypto-custody-writers.md).

Миграция [00078](../db/migrations/00077_crypto_custody_accounting.sql) добавляет финансовые связи и отдельный начальный suspense:

| Таблица | Назначение / ограничения |
|---|---|
| `reg_crypto_custody_effect_postings` | Один recorder владеет logical effect; block/recorder alias не разрешает повтор |
| `reg_crypto_custody_effect_recognitions` | Неизменяемые ссылки на точные wallet/token movement lines; не более одной стороны каждого направления |
| `reg_crypto_receipt_suspense_movements` | Приход gross по receipt; расход — распределение платежа или ручное зачисление (`CryptoMerchantCredit`); сторно возвращает сумму на receipt |
| `reg_crypto_receipt_suspense_balances` | Проекция нераспределённого остатка receipt/token |
| `reg_crypto_receipt_intake_audit` | Неизменяемая связь receipt, proof, posting owner и system actor |

Deferred constraints проверяют полный набор receipt/custody/suspense/recognition/audit/posted outbox при коммите. Rollback схемы 78 после финансового использования запрещён; доступна только будущая forward migration. Общий переход legacy writers, availability и компенсационные поколения receipt/funding остаются в [плане реализации](howto/crypto-inbound-provider-accounting-plan.md).

Миграция [00079](../db/migrations/00077_crypto_custody_accounting.sql) связывает `reg_crypto_wallet_fundings` с `occurrence_id` и `posting_id`; обе ссылки и `verification_id` — `NOT NULL`. Новые receipts используют occurrence вместо глобальной уникальности tx hash, поэтому разные доказанные effects одной tx учитываются раздельно. `reg_crypto_wallet_funding_audit` хранит funding/proof/posting/actor и запрещает UPDATE/DELETE. Funding receipt, recognition, physical movement, audit и один `CryptoWalletFunding.recorded` outbox обязательны в одном коммите. Двухсторонние SQL guards исключают пересечение funding и платежа по одному chain effect, в том числе при конкурентной вставке: обе стороны берут balance lock. CLI допускает только external funding platform-owned wallet и не создаёт merchant/fee/provider/cost movements. Финансовое использование схемы 79 запрещает её Down; история требует forward migration.

Раздел миграции [00080](../db/migrations/00077_crypto_custody_accounting.sql) делает CryptoPayment распределением custody receipt ([custody writers, §8](reference/crypto-custody-writers.md#8-cryptopayment-как-распределение-receipt)). `doc_crypto_payments.receipt_id` ссылается на receipt перевода; проведённый платёж записывает расход невыясненных (gross), приход мерчанту (net) и processing fee по снимку тарифа, движений по кошельку у него нет. Отложенный инвариант `fn_crypto_assert_payment_attribution` требует точный набор текущего поколения и полную компенсацию предыдущих. `trg_crypto_payment_frozen_basis` после распределения замораживает receipt, invoice/merchant/network/wallet/token, gross, tx и тариф и запрещает удаление. Custody-признание платежа (`reg_crypto_payment_custody_bindings`, predecessor generations, outbox `CryptoPayment.custody_recognized`) и adoption удалены.

Миграция [00082](../db/migrations/00077_crypto_custody_accounting.sql) добавляет `reg_crypto_sweep_custody_bindings`: posting_id (PK/FK), sweep_id, line_id (UNIQUE/FK), period, actor=`sweep-settlement`, created_at. Binding immutable. Каждая final успешная строка имеет один posting generation 1, две principal recognition links, точные fee/cost facts и `CryptoSweep.custody_recognized` outbox с lineId/postingId. Deferred constraints защищают весь набор и запрещают раннее освобождение fee reservation.

Миграция [00083](../db/migrations/00077_crypto_custody_accounting.sql) добавляет `reg_crypto_treasury_custody_bindings`: posting_id (PK/FK), treasury_id (UNIQUE/FK), signing_authorization_id (FK), period, actor=`treasury-settlement`, created_at. Binding immutable. Один expense recognition, principal/revenue/fee/reservation outcome и outbox обязательны в одном коммите. AccountingTransferBasis поддерживает expense-side с controlled source и external ToAddress; пустой RecordType сохраняет прежний receipt-side контракт.

## AML и merchant custody

Миграция [00081](../db/migrations/00077_crypto_custody_accounting.sql) добавляет `reg_crypto_native_funding_custody_bindings`: posting/funding, period, системный actor и created_at. Binding immutable; partial unique `uq_crypto_native_funding_custody` закрепляет один generation 1 за funding document. Две точные controlled principal стороны, normalized settlement, fee/cost и `CryptoNativeFunding.custody_recognized` outbox обязательны в одном коммите. SQL замораживает intent и запрещает generic reversal, повторную payment attribution и Down после использования. История не переписывается; новые principal writes требуют independent indexed proof. Детали и lock order: [custody writers](reference/crypto-custody-writers.md).

Эти объекты добавлены миграциями 61–76 и не регистрируются автоматически как generic CRUD-сущности. Они расширяют финансовый и custody-контур, а не заменяют merchant/wallet ledger. Финансовые crypto movements append-only: корректировка добавляет компенсирующие записи, UPDATE/DELETE исходных движений запрещены.

В `cat_wallets` custody owner отделён от текущего lease/assignment. В DTO кошельков и платежей доступна AML display-проекция, а deposit rotation сохраняет immutable историю и retired intake. Заявка AML-вывода получает отдельный `reg_crypto_aml_withdrawal_plans`/lines: выбранный risk class, explicit acceptance, sources, completed/refunded amounts и snapshot. Эти поля нельзя выводить только из прежнего header `WithdrawalRequest`.

| Миграция | Назначение | Основные таблицы |
|---|---|---|
| [00061](../db/migrations/00061_crypto_aml_foundation.sql) | Subjects, jobs, provider checks/reports, decisions и risk states | `reg_crypto_aml_subjects`, `reg_crypto_aml_jobs`, `reg_crypto_aml_provider_checks`, `reg_crypto_aml_reports`, `reg_crypto_aml_decisions`, `reg_crypto_wallet_risk_states`, `reg_crypto_withdrawal_risk_snapshots` |
| [00062](../db/migrations/00062_crypto_merchant_custody.sql) | Неизменяемый владелец кошелька и network custody policy | `sys_crypto_custody_policies` |
| [00063](../db/migrations/00063_crypto_aml_runtime.sql) | Accepted mappings, submit policy, локальные package grants/admissions | `sys_crypto_aml_provider_mappings`, `sys_crypto_aml_account_policies`, `reg_crypto_aml_package_credits`, `reg_crypto_aml_paid_admissions` |
| [00064](../db/migrations/00064_crypto_aml_routing.sql) | Verified effects, baselines, generations, очереди и изменения риска | `sys_crypto_aml_risk_policies`, `sys_crypto_aml_routing_scopes`, `reg_crypto_aml_decision_queue`, `reg_crypto_aml_wallet_baselines`, `reg_crypto_aml_effects`, `reg_crypto_aml_effect_checks`, `reg_crypto_aml_effect_chain_states`, `reg_crypto_aml_rescreen_requests`, `reg_crypto_aml_projection_queue`, `reg_crypto_aml_risk_changes` |
| [00065](../db/migrations/00065_crypto_aml_inventory.sql) | Principal/fee inventory, internal allocations и применение evidence | `reg_crypto_aml_inventory_positions`, `reg_crypto_aml_internal_allocations`, `reg_crypto_aml_allocation_cancellations`, `reg_crypto_aml_inventory_facts`, `reg_crypto_aml_inventory_queue` |
| [00066](../db/migrations/00066_crypto_aml_resources.sql) | Operating capital, resource budgets, facts и cancellations | `reg_crypto_aml_resource_positions`, `reg_crypto_aml_operating_receipts`, `reg_crypto_aml_resource_budgets`, `reg_crypto_aml_resource_cancellations`, `reg_crypto_aml_resource_facts` |
| [00067](../db/migrations/00067_crypto_aml_batches.sql) | Batch policy, collector capacity и история партий | `sys_crypto_aml_batch_policies`, `reg_crypto_aml_batches`, `reg_crypto_aml_batch_events` |
| [00068](../db/migrations/00068_crypto_aml_merchant_provisioning.sql) | Enrolled provisioning identity и durable wallet provisions | `sys_crypto_aml_provisioning_identity`, `reg_crypto_aml_wallet_provisions` |
| [00069](../db/migrations/00069_crypto_aml_merchant_pools.sql) | Merchant pool watermarks, версии политик и intents | `sys_crypto_aml_pool_policies`, `reg_crypto_aml_pool_policy_versions`, `reg_crypto_aml_pool_intents`, `reg_crypto_aml_pool_intent_cancellations` |
| [00070](../db/migrations/00070_crypto_aml_preflight.sql) | Watch coverage, независимый baseline preflight и observations | `reg_crypto_aml_watch_coverage`, `reg_crypto_aml_preflight_jobs`, `reg_crypto_aml_preflight_observations` |
| [00071](../db/migrations/00071_crypto_aml_activation_resources.sql) | Activation resource budget и final cost guards | Дополняет существующие AML/resource guards, отдельных таблиц нет |
| [00072](../db/migrations/00072_crypto_aml_dispatch_permits.sql) | Durable dispatch starts и first-dispatch facts | `reg_crypto_aml_dispatch_starts`, `reg_crypto_aml_first_dispatches` |
| [00073](../db/migrations/00073_crypto_aml_risk_withdrawals.sql) | Class-specific withdrawal plans/lines/facts/events | `reg_crypto_aml_withdrawal_plans`, `reg_crypto_aml_withdrawal_lines`, `reg_crypto_aml_withdrawal_cancellations`, `reg_crypto_aml_withdrawal_facts`, `reg_crypto_aml_withdrawal_events` |
| [00074](../db/migrations/00074_crypto_aml_testnet_emulator.sql) | Отдельный testnet AML emulator | `sys_crypto_aml_testnet_emulators` |
| [00075](../db/migrations/00075_crypto_aml_testnet_bootstrap.sql) | Однократный testnet bootstrap и component openings | `reg_crypto_aml_testnet_bootstraps`, `reg_crypto_aml_testnet_openings` |
| [00076](../db/migrations/00076_crypto_aml_opening_resource_and_signed_funding.sql) | Opening operating resource, signed funding и exact authorization renewal | `reg_crypto_aml_authorization_renewals` |

Базовые классы риска — `low`, `medium`, `high`, `unknown`. Оценка сама не является разрешением расходования: проверяются полнота inventory, owner, evidence TTL, holds, batch и dispatch gates. Testnet opening не выдаётся за отчёт провайдера. Daily liquidity-address screening и reviewed снятие исторического high остаются планом.

Доменные контракты: [AML](../internal/domain/crypto/aml), [custody](../internal/domain/crypto/custody_policy.go), [merchant pools](../internal/domain/crypto/merchant_pools.go). Подробные lifecycle и ограничения: [AML architecture](crypto/aml-kyt-architecture.md), [runtime](operations/crypto-aml-runtime.md), [outbound](operations/crypto-aml-outbound.md), [testnet bootstrap](operations/crypto-aml-testnet-bootstrap.md).

## Перечисления

Значения ниже взяты из строковых типов Go, используемых в описанных реквизитах, и регистраций метаданных. Наличие варианта не подтверждает поддержку соответствующего провайдера, стандарта токена или семейства сети в развёрнутом окружении.

| Тип / область | Значения | Источник |
|---|---|---|
| `entity.FeeType` | `processing`, `network`, `withdrawal`, `sweep` | [crypto_register.go](../internal/core/entity/crypto_register.go) |
| `entity.PlatformCostPurpose` | `sweep`, `withdrawal`, `treasury`, `activation`, `resource_rental`, `custody_loss` | [crypto_register.go](../internal/core/entity/crypto_register.go) |
| `entity.PlatformCostType` | `chain_resource_rent`, `chain_network_fee`, `custody_loss` | [crypto_register.go](../internal/core/entity/crypto_register.go) |
| `entity.RecordType` | `receipt`, `expense` | [register.go](../internal/core/entity/register.go) |
| `contract.ContractType` | `supply`, `sale`, `other` | [model.go](../internal/domain/catalogs/contract/model.go) |
| `counterparty.CounterpartyType` | `customer`, `supplier`, `both`, `other` | [model.go](../internal/domain/catalogs/counterparty/model.go) |
| `counterparty.LegalForm` | `individual`, `sole_trader`, `company`, `government` | [model.go](../internal/domain/catalogs/counterparty/model.go) |
| `merchant.KYBStatus` | `pending`, `approved`, `rejected` | [model.go](../internal/domain/catalogs/merchant/model.go) |
| `organization.TaxSystem` | `osno`, `usn_income`, `usn_income_expense`, `envd`, `patent` | [model.go](../internal/domain/catalogs/organization/model.go) |
| `rate_source.SourceType` | `coingecko`, `binance`, `coinmarketcap`, `manual` | [model.go](../internal/domain/catalogs/rate_source/model.go) |
| `token.TokenStandard` | `native`, `TRC-20`, `ERC-20`, `BEP-20`, `SPL`, `Jetton` | [model.go](../internal/domain/catalogs/token/model.go) |
| `wallet.AllocationMode` | `transient`, `persistent` | [model.go](../internal/domain/catalogs/wallet/model.go) |
| `wallet.ChainActivationStatus` | `unknown`, `activating`, `active`, `held`, `failed` | [model.go](../internal/domain/catalogs/wallet/model.go) |
| `wallet.WalletRole` | `merchant_pool`, `liquidity_hot`, `network_fee`, `treasury` | [model.go](../internal/domain/catalogs/wallet/model.go) |
| `wallet.WalletStatus` | `free`, `leased`, `assigned`, `sweep_pending`, `sweeping`, `frozen` | [model.go](../internal/domain/catalogs/wallet/model.go) |
| `wallet.WalletTier` | `pool`, `hot`, `warm`, `cold` | [model.go](../internal/domain/catalogs/wallet/model.go) |
| `crypto.ChainFamily` | `tron`, `evm`, `ton`, `solana` | [chain.go](../internal/domain/crypto/chain.go) |
| `crypto.FeeDirection` | `processing`, `withdrawal`, `payout`, `settlement`, `refund` | [fee_config.go](../internal/domain/crypto/fee_config.go) |
| `crypto.FinalityMode` | `confirmations`, `protocol_finalized`, `confirmations_or_finalized` | [chain.go](../internal/domain/crypto/chain.go) |
| `crypto_invoice.CreationSource` | `merchant_api`, `portal`, `system` | [model.go](../internal/domain/documents/crypto_invoice/model.go) |
| `crypto_invoice.InvoiceKind` | `payment`, `top_up` | [model.go](../internal/domain/documents/crypto_invoice/model.go) |
| `crypto_invoice.InvoiceStatus` | `created`, `partially_paid`, `paid`, `confirmed`, `completed`, `underpaid`, `expired`, `cancelled`, `reversed` | [model.go](../internal/domain/documents/crypto_invoice/model.go) |
| `crypto_network_operation.OperationType` | `wallet_activation` | [model.go](../internal/domain/documents/crypto_network_operation/model.go) |
| `crypto_network_operation.Status` | `broadcast`, `succeeded`, `reverted` | [model.go](../internal/domain/documents/crypto_network_operation/model.go) |
| `crypto_payment.PaymentStatus` | `detected`, `confirming`, `confirmed`, `settled`, `reorged`, `replaced`, `failed` | [model.go](../internal/domain/documents/crypto_payment/model.go) |
| `crypto_provider_balance_operation.DecisionMode` | `independent`, `admin_self` | [decision.go](../internal/domain/documents/crypto_provider_balance_operation/decision.go) |
| `crypto_resource_rental.CostStatus` | `unknown`, `recorded`, `partially_refunded`, `refunded`, `held` | [model.go](../internal/domain/documents/crypto_resource_rental/model.go) |
| `crypto_resource_rental.Status` | `created`, `ordering`, `delegated`, `active`, `reclaiming`, `closed`, `failed`, `held` | [model.go](../internal/domain/documents/crypto_resource_rental/model.go) |
| `crypto_sweep.SweepStatus` | `created`, `signed`, `broadcast`, `confirmed`, `partial_failed`, `held` | [model.go](../internal/domain/documents/crypto_sweep/model.go) |
| `crypto_withdrawal.WithdrawalStatus` | `created`, `signed`, `broadcast`, `confirmed`, `failed` | [model.go](../internal/domain/documents/crypto_withdrawal/model.go) |
| `outbound_settlement.ChainResult` | `SUCCESS` | [status.go](../internal/domain/documents/outbound_settlement/status.go) |
| `outbound_settlement.Status` | `unknown`, `repairing`, `recorded`, `not_applicable`, `held` | [status.go](../internal/domain/documents/outbound_settlement/status.go) |
| `treasury_withdrawal.Status` | `pending_signature`, `pending_approval`, `approved`, `signing`, `broadcast`, `confirmed`, `held`, `rejected`, `failed` | [model.go](../internal/domain/documents/treasury_withdrawal/model.go) |
| `withdrawal_request.Status` | `pending_signature`, `pending_approval`, `approved`, `signing`, `broadcast`, `confirmed`, `held`, `rejected`, `failed`, `partially_completed` | [model.go](../internal/domain/documents/withdrawal_request/model.go) |
| `integrations.Provider` | `alchemy`, `infura`, `drpc`, `coingecko_demo`, `tron_api`, `chainstack`, `getblock`, `getblock_net`, `testnet_aml`, `telegram`, `smtp`, `resource_api` | [model.go](../internal/domain/integrations/model.go) |

Список вариантов `integrations.Provider` в [integration_registrations.go](../internal/content/integration_registrations.go) строится из `integrations.ProviderDefinitions()`. Возможность создания и поддерживаемые назначения определяет definition: не каждый enum доступен обычному Create API; `testnet_aml` настраивается доверенным control plane. `rate_source.SourceType` получает варианты через [catalog_registrations.go](../internal/content/catalog_registrations.go).

Часть закрытых наборов хранится обычными строками, а не именованными перечислениями: режимы и статусы корректировок, типы операций провайдера, роли RPC. Их допустимые значения задаются валидацией и SQL-ограничениями. `outbound_settlement.ChainResult` хранит нормализованный результат сети; объявленная константа `SUCCESS` не ограничивает весь набор возможных результатов.

| Объект / реквизит | Значения | Источник |
|---|---|---|
| Операция баланса провайдера: операция | `opening`, `funding`, `adjustment`, `refund`; текущая валидация разрешает `opening`, остальные требуют отдельного подтверждённого сценария | [model.go](../internal/domain/documents/crypto_provider_balance_operation/model.go) |
| Операция баланса провайдера: статус | `pending_approval`, `approved`, `posted`, `rejected` | [model.go](../internal/domain/documents/crypto_provider_balance_operation/model.go) |
| Подключение RPC: роль | `rpc`, `indexer`, `fullnode`, `solidity` | [00022](../db/migrations/00022_crypto_catalogs.sql) |
| Аренда ресурсов: ресурс / режим | `energy`, `bandwidth` / `fixed` | [00036](../db/migrations/00036_crypto_resource_rentals.sql) |
| Пополнение сетевой комиссии: статус хранения | `created`, `reserved`, `signed`, `broadcast`, `solidified`, `settled`, `retry_wait`, `held`, `failed` | [00041](../db/migrations/00041_multichain_crypto_core.sql) |
| Учётная криптокорректировка: режим / согласование | `payment_reversal`, `wallet_balance` / `two_person`, `direct` | [00060](../db/migrations/00060_crypto_adjustment_modes.sql) |

## Настройки — аналог констант

Настройки арендатора хранятся одной записью в `sys_settings`, а в модели сгруппированы по подсистемам. Источник: [model.go](../internal/domain/settings/model.go).

| Группа | Путь реквизита | Тип | Назначение |
|---|---|---|---|
| Общие | `general.timezone` | Строка | Часовой пояс IANA |
| Нумерация | `numbering.autoNumbering` | Булево | Автоматическая нумерация |
| Нумерация | `numbering.numberPrefix` | Строка | Общий префикс номера |
| Производительность | `performance.batchConcurrency` | Целое | Параллелизм пакетных операций |
| Курсы валют | `exchangeRates.sourceId` | Ссылка → Источники курсов, допускает null | Выбранный источник курсов |
| Служебные | `version`, `updatedAt` | Целое; дата и время | Версия и дата изменения настроек |

Реквизиты организации и её учётная политика хранятся в справочнике «Организации». Политики сетей и пулов кошельков описаны среди регистров сведений. Дополнительные настройки среды находятся в конфигурации приложения и не являются объектами метаданных арендатора.

## Отчёты и варианты

### Журнал документов — `document-journal`

Единственный встроенный декларативный набор данных в `AllDatasets`. Источник: [report_datasets.go](../internal/content/report_datasets.go).

| Элемент | Состав |
|---|---|
| Источники | `crypto_invoice`, `crypto_payment`, `crypto_withdrawal`, `treasury_withdrawal` |
| Измерения | Тип документа, дата; скрытые ID токена и точность токена |
| Ресурс | Сумма с суммированием; для инвойса — `expected_amount`, для остальных источников — `amount` |
| Реквизиты | Номер, проведённость, мерчант, токен, скрытые ID и комментарий |
| Параметры | `from_date`, `to_date`, `posted` |
| Сортировка | Дата по убыванию |
| Выгрузка | CSV, XLSX |

В журнал входят четыре перечисленных типа документов. Представления остатков, курсов, сверки и панели мониторинга обслуживаются отдельными API; они не объявлены дополнительными наборами данных `AllDatasets`.

### Сохранённые варианты отчётов

Таблица: `sys_report_variants`. Источник: [model.go](../internal/domain/reports/variants/model.go).

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Ссылка | `ID` / `id` | UUID | — |
| Набор данных отчёта | `DatasetKey` / `datasetKey` | Строка | — |
| Наименование | `Name` / `name` | Строка | — |
| Автор | `AuthorID` / `authorId` | Ссылка → Пользователи; допускает null в модели | — |
| Видимость | `Visibility` / `visibility` | Перечисление `variants.Visibility` | — |
| Основной | `IsDefault` / `isDefault` | Булево | — |
| Настройки варианта | `Config` / `config` | Структура `VariantConfig` | — |
| Пометка удаления | `DeletionMark` / `deletionMark` | Булево | — |
| Версия | `Version` / `version` | Целое число | — |
| Дата создания | `CreatedAt` / `createdAt` | Дата и время | — |
| Дата изменения | `UpdatedAt` / `updatedAt` | Дата и время | — |

Состав `Config` — `VariantConfig`:

| Реквизит | Поле модели / JSON | Тип | Особенности |
|---|---|---|---|
| Выбранные поля | `SelectedFields` / `selectedFields` | Массив `string` | — |
| Видимые колонки | `VisibleColumns` / `visibleColumns` | Массив `string` | — |
| Группировки | `GroupBy` / `groupBy` | Массив `string` | — |
| Колонка сортировки | `SortColumn` / `sortColumn` | Строка; допускает null в модели | — |
| Направление сортировки | `SortDirection` / `sortDirection` | Строка | — |
| Отборы | `Filters` / `filters` | Структура / JSON | — |
| Расширенные отборы | `AdvancedFilters` / `advancedFilters` | Массив `filter.Item` | — |

Видимость варианта: `personal`, `shared`, `system`.

## Служебные объекты платформы

В терминах 1С это вспомогательные справочники, настройки, регистры и обработки. В Metapus они реализованы отдельными подсистемами; следующие строки дают карту объектов без полного перечня технических колонок.

| Подсистема / объекты | Основные реквизиты | Хранение / описание |
|---|---|---|
| Пользователи | ID, email, имя, активность, роли, параметры аутентификации | `auth_users`; [auth/model.go](../internal/domain/auth/model.go) |
| Роли и разрешения | Код, имя, ресурс, действие, связи пользователей и ролей | `auth_roles`, `auth_permissions`, `auth_user_roles`, `auth_role_permissions`; [auth-security.md](systems/auth-security.md) |
| Профили безопасности | Имя, измерения доступа, политики полей, правила, пользователи | `auth_security_profiles`, `auth_security_profile_dimensions`, `auth_security_profile_field_policies`, `auth_security_policy_rules`, `auth_user_security_profiles`; [model.go](../internal/domain/security_profile/model.go) |
| Нумераторы | Ключ последовательности и счётчик; правила префикса и периода | `sys_sequences`; [numerator.md](systems/numerator.md) |
| Автоматизация | Правила, расписания, каналы, подписчики, аккаунты, запуски, история | `sys_automation_rules`, `sys_automation_schedules`, `sys_automation_channels`, `sys_automation_subscribers`, `sys_automation_accounts`, `sys_automation_runs`, `sys_automation_history`; [automation-engine.md](systems/automation-engine.md) |
| Уведомления | Пользователь, событие, содержимое, прочтение | `sys_notifications`; [realtime-notifications.md](systems/realtime-notifications.md) |
| Фоновые задания | Имя задания, статус, начало, окончание, результат / ошибка | `sys_worker_jobs`; [worker-reliability.md](operations/worker-reliability.md) |
| Настройки пользователя и списков | Пользователь, ключ настройки, конфигурация представления | `sys_user_preferences`, `sys_list_views`; [settings-system.md](systems/settings-system.md) |
| Аудит и события | Объект, действие, автор, момент, детали | `sys_audit`, `sys_event_log`; [infrastructure-layer.md](systems/infrastructure-layer.md) |
| Надёжная доставка | Исходящие события, попытки, статус, повтор; ключи идемпотентности | `sys_outbox`, `sys_outbox_dlq`, `sys_idempotency`, `sys_merchant_events`, `sys_webhook_delivery_jobs`, `sys_webhook_deliveries` |

## Источники и актуализация

| Источник | Что определяет |
|---|---|
| [internal/content/register.go](../internal/content/register.go) | Полный состав встроенных справочников, документов, маршрутов регистров и наборов данных |
| [catalog_registrations.go](../internal/content/catalog_registrations.go), [integration_registrations.go](../internal/content/integration_registrations.go), [document_registrations.go](../internal/content/document_registrations.go) | Имена объектов, ключи прав, представления, команды и варианты перечислений |
| [internal/core/entity](../internal/core/entity), [internal/domain/catalogs](../internal/domain/catalogs), [internal/domain/documents](../internal/domain/documents) | Базовые и предметные реквизиты, типы, табличные части, источники движений |
| [internal/metadata/inspector.go](../internal/metadata/inspector.go) | Построение метаданных из Go-структур и тегов `meta` / `json` |
| [internal/infrastructure/http/v1/dto](../internal/infrastructure/http/v1/dto) | Поля чтения и записи фактического HTTP-контракта |
| [db/migrations](../db/migrations) | Физические таблицы, дополнительные служебные поля, ключи и ограничения |

При добавлении объекта обновляются дерево конфигурации и соответствующий раздел. При изменении модели сверяются реквизиты, коллекции и перечисления; при изменении миграций — ключи, ресурсы и отдельные сценарии. Классификация регистра определяется назначением данных, а не одним только префиксом таблицы.

Для проверки развёрнутой конфигурации доступны `GET /api/v1/meta/entities` (состав), `GET /api/v1/meta` (определения общего реестра) и `GET /api/v1/meta/:name` (конкретный объект с добавленными пользовательскими реквизитами). Эти методы не включают автоматически все таблицы и объекты отдельных сценариев.

### Merchant withdrawal recognition схемы 84

[00084](../db/migrations/00077_crypto_custody_accounting.sql) добавляет immutable `reg_crypto_withdrawal_custody_bindings`: posting_id (PK/FK), withdrawal_id (UNIQUE/FK), request_id (FK/index), nullable line_id (UNIQUE/FK), signing_authorization_id (FK), merchant_debit_line_id (FK), period, actor=`withdrawal-settlement`, created_at. Один outgoing principal owner связывается с исходным debit-first расходом без второго merchant debit. Actual fee/cost записываются на request/AML line basis, physical principal — на CryptoWithdrawal.

В `reg_crypto_aml_withdrawal_facts` expense_movement_id теперь nullable: failed fact имеет оба physical refs NULL; success обязан ссылаться на exact recognized expense. `refund_movement_id` всегда NULL, это закреплено CHECK. Valid-fact view принимает только этот контракт; materialized inventory уменьшает только успешную долю. Principal reservations остаются active на broadcast и становятся consumed/released только с final outcome. SQL защищает original merchant debit, frozen financial basis, fee/principal/refund/outbox set и expected owner.

### Custody loss recognition схемы 85

[00085](../db/migrations/00077_crypto_custody_accounting.sql) подключает CryptoCustodyAdjustment к registry: он сохраняет loss expense/cost и maker-checker, но требует exact indexed outgoing proof и shared registry owner. Detection/classification не создают финансовых движений.

Immutable `reg_crypto_custody_loss_bindings`: posting_id (PK/FK), adjustment_id (UNIQUE/FK), external_outflow_id (UNIQUE/FK), maker_id/checker_id (разные), period, created_at. Principal и cost получают отдельные стабильные line IDs и общий period из binding. Physical expense, custody-loss cost, checker action audit, reconciliation request, final decision и outbox обязательны в одном коммите. Posting transaction stamp включает новые CryptoCustodyAdjustment lines; исторические movements не обновляются. SQL защищает exact set, immutable basis, generic compensation и ownership через aliases. Generation/reorg recovery не добавлен.

### Network fee recognition схемы 86

[00086](../db/migrations/00077_crypto_custody_accounting.sql) добавляет отдельную независимую fee role; [NetworkFeeProof](../internal/domain/crypto/network_fee_proof.go) не подменяет transfer proof. Подробный [контракт](reference/crypto-custody-writers.md#14-независимое-признание-network-fee-схемы-86).

| Объект | Новые реквизиты / назначение |
|---|---|
| `reg_crypto_fee_reservations` | Признание требует independent fee proof и полного financial set |
| `reg_crypto_network_fee_proofs` | Immutable effect key, network/tx/family/payer/native asset/block/outcome/total/digest/confirmations, structured evidence с components и двумя независимыми sources, verified/created time |
| `reg_crypto_network_fee_bindings` | Immutable reservation/effect/proof/settlement, recorder, generation=1, nullable physical/cost line IDs при fee=0, общий period |
| `reg_crypto_chain_settlements` | Nullable `network_fee_reservation_id`; normalized components точно совпадают с proof |
| `doc_crypto_network_operations` | Nullable `network_fee_reservation_id`, API `networkFeeReservationId`; признанная activation использует stable line IDs/FinalizedAt и не допускает generic unpost |
| `reg_crypto_fee_reservation_events` | Nullable `transition_txid`, заполняемый БД для новых events; replacement требует old/new hash/fingerprint события в том же transaction, исторические события не меняются |

NativeFunding читает fee accounting version как внутреннюю метаинформацию для строгого terminal replay; она не входит в публичный JSON. Никакого merchant net, fee income или provider credit признание network fee не создаёт. История не принимается автоматически; reorg/compensation требуют отдельного workflow.

### Expected transfer routing схемы 87

[00087](../db/migrations/00077_crypto_custody_accounting.sql) не переписывает expected effects, старые event-index matches или финансовую историю. [Контракт](reference/crypto-custody-writers.md#15-маршрутизация-ожидаемых-переводов-схемы-87).

| Таблица | Назначение |
|---|---|
| `reg_crypto_expected_transfer_requests` | Immutable expected effect → original basis owner, первое полное экономическое observation, requested time; требует один settlement_requested outbox |
| `reg_crypto_expected_transfer_routes` | Immutable logical accounting effect → один expected effect/original owner, первый independent indexed proof |
| `reg_crypto_expected_transfer_evidence` | Immutable proof → logical route; новые aliases/occurrences не создают денег |
| `sys_crypto_expected_transfer_deliveries` | Mutable operational projection: next attempt, delivered time, attempts, version; отдельна от immutable producer facts |

Новые таблицы содержат transaction/deletion metadata; факты защищены append-only guard. Routing не является документом проведения, финансовым разрешением или новым источником available balance. Multi-output и будущие provider/refund intents требуют отдельного профиля. Новый HTTP/CRUD для routing не добавлен.


### Порядок ресурсов учёта схемы 89

[00089](../db/migrations/00077_crypto_custody_accounting.sql) добавляет SQL helpers `fn_crypto_lock_custody_scopes(uuid[])` и `fn_crypto_lock_custody_wallets(uuid[])` и переводит два fee/resource scope helpers на полный owner/network scope set. Новых документов, регистров, денежных fields или recognition roles нет. PostgreSQL CryptoBalanceRepo реализует domain storage port ResourceLocker для early preflight PostingEngine. Финансовая история не изменяется; [порядок и оставшиеся участки аудита](reference/crypto-custody-writers.md#17-порядок-ресурсов-учёта-схемы-89).

### Selectors и activation схемы 90

[00090](../db/migrations/00077_crypto_custody_accounting.sql) добавляет `fn_crypto_try_lock_custody_wallets(uuid[], uuid DEFAULT NULL, uuid DEFAULT NULL)` и `fn_crypto_lock_withdrawal_wallets(uuid[], uuid, uuid)`. Они блокируют объединение wallet-owner scopes и дополнительного scope заявки до wallets. Try-lock освобождает частичный набор при занятости. Withdrawal/sweep selectors, draft linking и activation coordinator получают ресурсы до workflow rows; sweep повторяет eligibility на свежем statement snapshot. Новых финансовых реквизитов, документов, регистров или recognition owners нет. [Контракт, проверки и оставшийся аудит](reference/crypto-custody-writers.md#18-selectors-и-activation-схемы-90).

### Native funding и bulk maintenance схемы 91

[00091](../db/migrations/00077_crypto_custody_accounting.sql) добавляет `fn_crypto_try_lock_funding_confirmation(uuid)`, который атомарно пропускает занятые scopes/wallets/funding row. Native-funding operational mutations и expected-transfer nudge используют общий transaction-only preflight; bulk sweep maintenance фиксирует candidates до получения ресурсов и ограничивает ими все дальнейшие изменения. Новых финансовых сущностей, реквизитов или recognition owners нет. [Контракт и оставшийся аудит](reference/crypto-custody-writers.md#19-native-funding-operational-updates-и-bulk-maintenance-схемы-91).

### Execution/authorization publication — этап 92

Схема остаётся 91. Go adapters получают полный execution resource prefix до replacement UPDATE и authorization INSERT. AML publication использует line basis; новые replacement generations требуют exact signed predecessor и свежего execution state. Expected-effect replay сравнивает полную identity и единственный ordinal текущего transfer profile. Новых документов, реквизитов, financial facts или DTO нет. [Контракт](reference/crypto-custody-writers.md#20-публикация-execution-и-authorization--этап-92).

### Queue barriers и semantic replay — этап 93

Схема остаётся 91. Capture/ack явно требует read-committed snapshot; signing tuple replay сравнивает полный bound body и canonical unsigned payload, сохраняя исходный deadline. Fee replay проверяет normalized outcome/components и direct movement payload, а pending collision требует reconciliation. Новых реквизитов, таблиц, DTO или финансовых owners нет. [Контракт и матрица аудита](reference/crypto-custody-writers.md#21-queue-barriers-и-semantic-replay--этап-93).

### Reconciliation identity и ручная AML cancellation — этап 94, схема 92

[00092](../db/migrations/00077_crypto_custody_accounting.sql) сохраняет identity заявки на сверку: ID, network/wallet/token, source type/ID и первоначальный requested_at. Для native funding source/destination и custody adjustment target проверяется по исходному документу; для остальных внутренних source types — общая принадлежность wallet/token сети. Status, attempts, leases и результат остаются операционной проекцией. Миграция останавливается при несовместимой существующей identity, требуя разбора до обновления; автоматического исправления истории нет. Go producers проверяют payload после конфликта и сохраняют исходный результат/время; native pair атомарна. Ручная отмена AML allocation/resource требует исходных reason/actor при повторе. Новых таблиц, колонок, финансовых owners или DTO нет. [Контракт и оставшиеся части аудита](reference/crypto-custody-writers.md#22-reconciliation-identity-и-ручная-aml-cancellation--этап-94).

### Пакетная отмена withdrawal и sweep expiry — этап 95

Схема остаётся 92. Go producers проверяют сохранённый cancellation reason/actor после коллизии INSERT; unsigned terminal replay не меняет основание. Remainder refund требует полностью новых cancellation facts, pending audit evidence требует reconciliation. AML approval/rejection/cancellation получают полный request/execution advisory и scope/wallet prefix до request rows, повторно проверяя resource set после ожидания. AML resource commands и maintenance planning требуют read committed. Таблицы, поля, DTO и financial owners не добавлены; история не переписывается. [Контракт](reference/crypto-custody-writers.md#23-пакетная-отмена-withdrawal-и-sweep-expiry--этап-95).

### Resource rental accounting replay — этап 96

Схема 92, новых таблиц/полей/DTO нет. Resource rental charge/refund проверяет полный provider/platform financial set и canonical cumulative refund sequence. Refund только нарастает и не превышает gross; pending/corrupt history требует reconciliation. Exact replay сохраняет первый header/period/expiry/close, terminal status не переоткрывается в held. Provider account предшествует rental row; rental предшествует allocation settlement row. Financial writes требуют read committed; allocation receipt связан с rental network и исходными usage/outcome. [Контракт](reference/crypto-custody-writers.md#24-resource-rental-charge-и-cumulative-refund--этап-96).

### Legacy compensation и allocation replay — этап 97

Схема 92. Legacy finalization этого этапа удалена вместе с двойной моделью учёта. Recorded replay сохраняет первоначальный период и проверяет outcome/fee/usage. Resource allocation reservation/broadcast проверяет сохранённый payload и semantic event; exact replay не меняет timestamps. Financial transaction/preflight precedes fee writes, rental precedes allocation replay locks. Таблицы/поля/DTO/financial owners не добавлены. [Контракт](reference/crypto-custody-writers.md#25-legacy-compensation-и-resource-allocation-replay--этап-97).


### Activation task identity и operational replay — этап 98, схема 93

[00093](../db/migrations/00077_crypto_custody_accounting.sql) сохраняет task ID/network/wallet/created_at и установленную operation link; link проверяется по exact task basis/network/target. Удаление identity запрещено; несовместимая history останавливает upgrade без автоматической правки. Activation selector/state используют advisories → policy → scopes/wallets → activation row, read committed и immutable terminal replay. Resource release сверяет первоначальный reason/event либо полный terminal rental close. Таблицы, поля, financial owners и DTO не добавлены. [Контракт](reference/crypto-custody-writers.md#26-activation-selectorsstate-и-resource-release--этап-98-схема-93).


### Rental maintenance fencing — этап 99, схема 94

[00094](../db/migrations/00077_crypto_custody_accounting.sql) добавляет UUID `maintenance_claim_token` к `doc_crypto_resource_rentals`; установленный token требует owner/claimed_at/lease_expires_at. Каждая попытка получает новый token, state/close/release проверяют token и wall-clock lease после account → rental locks. Legacy leases сохраняются без token до expiry, financial history не переписывается. Для выкладки остановить прежние workers; HTTP DTO и financial owners не меняются. [Контракт](reference/crypto-custody-writers.md#27-fencing-rental-maintenance--этап-99-схема-94).


### Activation cost-repair fencing — этап 100, схема 95

[00095](../db/migrations/00077_crypto_custody_accounting.sql) добавляет `cost_claim_token` в `reg_crypto_account_activations`; установленный token требует repairing status и owner/claimed_at/lease. Каждая попытка получает новый token; guarded completion проверяет его после общего prefix до fee writes. Authoritative finality сохраняется и отзывает repair token вместе с completion. Legacy leases сохраняются без token до expiry, financial history/owners и HTTP DTO не меняются. Для выкладки остановить прежние activation workers. [Контракт](reference/crypto-custody-writers.md#28-activation-cost-repair-fencing--этап-100-схема-95).


### Activation execution dispatch audit — этап 101, схема 96

[00096](../db/migrations/00077_crypto_custody_accounting.sql) добавляет immutable `reg_crypto_activation_dispatches` с exact authorization/activation/phase/claim/network/fingerprint. Token основной activation lease передаётся в actor commands и guarded dispatch; один Sign фиксируется до HSM и неизвестный результат не освобождает fee reserve. Pool slot и fee reserve создаются атомарно в serializable transaction; state/dispatch сохраняют read committed. Late signed/broadcast evidence сохраняет original owner и новую чужую lease; authoritative finality остаётся отдельной. Новых financial owners/HTTP DTO нет. Audit запрещено переписывать/удалять; Down после facts требует forward recovery. Для выкладки остановить старые activation workers. [Контракт](reference/crypto-custody-writers.md#29-activation-execution-fencing-и-dispatch-recovery--этап-101-схема-96).

### CryptoNetworkOperation metadata/protection prefix — этап 102

Схема остаётся 96, новых таблиц/полей/HTTP DTO нет. Metadata/protection/row locks существующего документа и post/unpost/repost получают activation workflow/operation → pool policy → scopes/wallets → activation row до document/balance locks. Набор включает current/proposed/historical wallets и проверяется повторно после ожидания. Manual-edit permission читается под prefix; exact protection replay не меняет version/actor/financial history. Document paths требуют read committed и explicit transaction для FOR UPDATE; serializable activation reserve сохраняется отдельно. [Контракт](reference/crypto-custody-writers.md#30-cryptonetworkoperation-metadataprotection-и-posting-prefix--этап-102).

### Основание финансового сторно платежей — этап 103, схема 97

[00097](../db/migrations/00077_crypto_custody_accounting.sql) добавляет immutable request_basis JSONB в reg_crypto_reorg_compensations. Старые записи сохраняют NULL; новые требуют schemaVersion=1, UPDATE/DELETE audit запрещены, Down после нового basis запрещён. Replay сверяет исходное основание платежа, compensation/incident/block/basis и полный результат сторно трёх регистров. Incident сохраняет первую observation и проверяет payment owner; network incident использует deterministic content key и semantic legacy lookup без переписывания history. Новых financial owners/HTTP DTO нет. Старые workers остановить перед upgrade, прямым compensation callers требуется read committed. [Контракт](reference/crypto-custody-writers.md#31-повторы-инцидентов-и-финансового-сторно-платежей--этап-103-схема-97).

### Повторы operational holds — этап 104

Схема остаётся 97, новых таблиц/полей/HTTP DTO и движений денег нет. Общий API постановки/снятия сверяет immutable acquisition details и resolution actor/reason, сериализует owner сразу для network/wallet tables и не добавляет targets resolved owner. Read committed обязателен; busy owner возвращает conflict без ожидания. Сверка остатков берёт hold basis из первого immutable observation, последующие delta/run остаются отдельными evidence facts. History не переписывается, прямые SQL hold writers остаются отдельным P3 audit. [Контракт](reference/crypto-custody-writers.md#32-повторные-команды-постановки-и-снятия-запретов--этап-104).

### SQL hold lifecycle guard — этап 105, схема 98

[00098](../db/migrations/00077_crypto_custody_accounting.sql) добавляет общие replay/identity guards на INSERT/UPDATE/DELETE hold tables и SQL owner gate, совместимый с Go key этапа 104. Включены raw provisioning/preflight/pool/baseline-release writers; NULL или changed basis не считается успешным replay. Новые pool/preflight owner IDs включают wallet и source, legacy identifiers не переписываются. First payload сохраняется, новое finding после resolution получает отдельный lifecycle. Новых денежных таблиц/полей/движений и HTTP DTO нет. Down после новых wallet-bound facts требует forward recovery; перед upgrade остановить старые workers/controllers. [Контракт](reference/crypto-custody-writers.md#33-прямые-sql-запреты-и-отдельные-основания-кошельков--этап-105-схема-98).


### Отмена заявки и первое наблюдение — этап 106, схема 99

[00099](../db/migrations/00077_crypto_custody_accounting.sql) проверяет reason/actor отмены pool intent до ON CONFLICT и дополняет SQL порядок блокировок scopes перед intent. Coverage guard проверяет действующие полномочия после ожидания по clock_timestamp(), сохраняя первую запись при смене исполнителя и обновлении списка. Приложение отклоняет неверную watcher identity/lease даже без новых адресов. Денежные таблицы, DTO и сохранённые данные не меняются; Down/Up сохраняет историю и переключает только функции. При выкладке/откате остановить старые workers/controllers. [Контракт](reference/crypto-custody-writers.md#34-отмена-заявки-на-кошелёк-и-первое-подтверждение-наблюдения--этап-106-схема-99).


### Первое решение AML и история расхождений счёта — этап 107, схема 100

[00100](../db/migrations/00077_crypto_custody_accounting.sql) добавляет nullable evaluated_at в reg_crypto_aml_decisions и episode_id/initial_details/incident_id в sys_invoice_reconciliation_drift. Первое основание и закрытая история защищены SQL; новая ошибка после устранения создаёт отдельный инцидент после двух наблюдений. Legacy NULL clocks/details не восстанавливаются. Новых денежных движений и DTO нет. Старые workers остановить до upgrade. Down оставляет evaluated_at для сохранения audit; после новых invoice episodes/links/history требуется forward recovery. [Контракт](reference/crypto-custody-writers.md#35-первое-решение-проверки-риска-и-история-расхождений-счёта--этап-107-схема-100).


### Основания preflight attempts и activation queue — этап 108, схема 101

[00101](../db/migrations/00077_crypto_custody_accounting.sql) добавляет immutable reg_crypto_aml_preflight_attempts и два initial version поля job. Claim требует snapshot при commit; Save/Retry сверяют весь basis и текущий token после общего resource prefix. Исторические неизвестные данные не backfill, observations FK добавлен NOT VALID для legacy. Activation queue выбирает только готовый fixed target и сохраняет имеющуюся task. HTTP DTO и деньги не меняются; внутренний job содержит PolicyID. Старые workers остановить; после новых registrations/attempts Down требует forward recovery. [Контракт](reference/crypto-custody-writers.md#36-основания-попыток-проверки-и-очередь-активации--этап-108-схема-101).


### Начальные блоки и watch ownership — этап 109, схема 102

[00102](../db/migrations/00077_crypto_custody_accounting.sql) добавляет nullable registration_basis/activation_basis в sys_crypto_block_ingestion и guards источников watch targets/catalog owners. Первый shadow/financial вход сохраняется, activated boundary не передвигается повтором или pause/resume. Общий read-committed network try-gate устраняет обратное ожидание callbacks и stale snapshot обход; TRON driver повторяет только busy. Legacy неизвестные bases не backfill, Up проверяет owner/key и retained history; financial mode без activated_at отклоняется, включая preflight старых rows. Down сохраняет столбцы, после новых activation facts требует forward recovery. Остановить старые workers/catalog writers перед upgrade. Новых денежных движений и HTTP DTO нет. [Контракт](reference/crypto-custody-writers.md#37-начальные-блоки-и-владельцы-наблюдения--этап-109-схема-102).


### Первое основание внешних списаний — этап 110, схема 103

[00103](../db/migrations/00077_crypto_custody_accounting.sql) добавляет nullable initial_basis и observation incident_id. DB сохраняет первое основание/clock и проверяет raw effect replay; Go сохраняет прежних catalog owners, согласованно обновляет block tuple/confirmations и не снижает floor. Incident/observation writes проверяют полный source; read-committed try-prefix предшествует workflow rows. Busy отложен в worker без исчерпания attempts. Legacy NULL не backfill; после новых source facts Down закрыт. Остановить старые writers перед upgrade. [Контракт](reference/crypto-custody-writers.md#38-первоначальные-сведения-внешних-списаний--этап-110-схема-103). P3 decision/reappearance audit и P4–P13 остаются в плане.


### Основания решений внешних списаний — этап 111, схема 104

[00104](../db/migrations/00077_crypto_custody_accounting.sql) сохраняет nullable decision bases classification/resolution/adjustment/audit. Go UUID seed, DB source/clock, AFTER audit и deferred exact check/run/incident/hold cohort обеспечивают один неизменяемый результат. Same-command replay не создаёт новых денег/audit/release; изменённое основание и superseded classification отклоняются. Legacy NULL не backfill; после новых decision facts Down закрыт. Перед upgrade остановить старые writers. [Контракт](reference/crypto-custody-writers.md#39-основания-и-повторы-решений-внешних-списаний--этап-111-схема-104). R1 реализован, R2–R8 и P4–P13 остаются открытыми.
