# Строгие системные документы (StrictDocument)

> **TL;DR:** Строгий документ создаётся системным workflow, заблокирован для ручных изменений и допускает ограниченную учётную коррекцию только системным администратором после явной разблокировки.

> **Тип:** Concept
> **Аудитория:** Developer, Admin
> **Связанные:** [crud-pipeline.md](crud-pipeline.md), [posting-engine.md](posting-engine.md), [auth-security.md](auth-security.md)

---

## 1. Назначение

`StrictDocument` применяется к документам, которые фиксируют уже произошедший
внешний или системный факт. Пользователь с read permission может просматривать
документ, движения, связанные документы и экспорт, но не может переписать его
вручную.

Первый потребитель механизма — `CryptoNetworkOperation`. Документ отражает
принятую сетью blockchain-транзакцию, поэтому его blockchain identity нельзя
менять даже в режиме ручной коррекции.

Защита состоит из двух независимых слоёв:

1. `StrictDocumentGuard` проверяет права и состояние блокировки в backend.
2. `DocumentProtection` и `EntityActionDef` управляют доступностью формы и команд во frontend.

Frontend не является границей безопасности. Прямой HTTP-запрос проходит те же
backend-проверки.

## 2. Контракт сервиса

`StrictDocument` предоставляет ID и признак `manualEditAllowed`. Guard оборачивает
обычный `DocumentService` и применяет следующую матрицу:

| Операция | Заблокирован | Разблокирован системным администратором | После успеха |
|---|---|---|---|
| `Create`, `PostAndSave` | Запрещено | Запрещено | — |
| `Update` | Запрещено | Разрешено для whitelist полей | Остаётся разблокирован |
| `Post` | Запрещено | Разрешено | Автоблокировка |
| `UpdateAndRepost` | Запрещено | Разрешено | Автоблокировка |
| `Unpost` | Запрещено | Разрешено | Остаётся разблокирован |
| `SetDeletionMark(true)` | Запрещено | Разрешено | Автоблокировка |
| `SetDeletionMark(false)` | Запрещено | Разрешено | Остаётся разблокирован |
| Физический `Delete` | Запрещено | Запрещено | — |

Копирование также недоступно, потому что оно создаёт новый пользовательский
документ через запрещённый Create path.

Автоблокировка выполняется в той же транзакции, что проведение, перепроведение
или пометка на удаление. Ошибка операции оставляет прежнее состояние документа.

Внутренний worker использует системный сервис без пользовательского guard. Это
не отменяет доменную валидацию, optimistic locking и проверки проведения.

## 3. Metadata-контракт

`DocumentProtection` публикуется в metadata сущности:

| Поле | Значение для `CryptoNetworkOperation` |
|---|---|
| `lockFinalDocuments` | `true` |
| `unlockField` | `manualEditAllowed` |
| `mutableFields` | `sourceWalletId`, `costTokenId`, `costAmount`, `description` |
| `requiresSystemAdmin` | `true` |
| `autoLockAfterPosting` | `true` |

`EntityActionDef` дополнительно задаёт:

- размещение только в меню «Ещё» через `ui.toolbarMenu`;
- допустимые бизнес-статусы;
- требуемое состояние защиты `locked` или `unlocked`;
- `requiresSystemAdmin`;
- идемпотентность custom action;
- confirmation-текст.

После custom action форма перечитывает документ через API. Кнопки и поля
пересчитываются без полной перезагрузки страницы. Даже после разблокировки поля,
которых нет в `mutableFields`, остаются read-only.

В заблокированном состоянии команды сохранения, проведения, распроведения и
пометки на удаление отображаются disabled. Страница ручного создания сообщает,
что документ создаётся другим workflow.

## 4. Режим ручной коррекции

Для `CryptoNetworkOperation` доступны действия:

| Endpoint | Защита | Результат |
|---|---|---|
| `POST /api/v1/document/crypto-network-operation/:id/allow-editing` | permission `crypto.network_operation.allow_editing`, system admin, fresh step-up с тем же action code | `manualEditAllowed=true` |
| `POST /api/v1/document/crypto-network-operation/:id/deny-editing` | тот же permission и system admin | `manualEditAllowed=false` |

System admin — пользователь с `isAdmin=true` или ролью `admin`. Роль
`security_admin` входит в общий admin security boundary для MFA/CIDR, но не даёт
права менять бухгалтерские факты и не проходит `RequireSystemAdmin`.

Разблокировать можно только финальную операцию со статусом `succeeded` или
`reverted`. Команда разблокировки показывает подтверждение:

> **Разрешить редактирование блокчейн-операции?**
>
> Ручные изменения проведённых блокчейн-операций могут нарушить консистентность
> учёта. Разрешить редактирование документа?

Кнопки диалога: «Разрешить редактирование» и «Отмена».

Смена `manualEditAllowed` через обычный Update DTO невозможна. Backend повторно
сравнивает immutable-поля с текущей версией документа.

## 5. Учёт и blockchain-факты

Распроведение и пометка на удаление отменяют только движения PostingEngine.
Blockchain-транзакция, `txHash`, settlement, finality и оперативный
`chainActivationStatus` кошелька не откатываются.

Для проведённого документа `SetDeletionMark(true)` атомарно дописывает связанные
обратные движения и устанавливает `deletionMark`. Исходные движения сохраняются
в финансовом журнале. Распроведение и перепроведение используют тот же механизм
сторно; уникальность расхода платформы проверяется в пределах версии проведения.
Жёсткого удаления через API нет.

Это аварийный учётный инструмент, а не blockchain rollback. Если нужен новый
on-chain эффект, его создаёт отдельный системный workflow.

### Отдельные ограничения распределённого CryptoPayment

Матрица ручной разблокировки выше относится к `CryptoNetworkOperation`. CryptoPayment, распределивший custody receipt, защищён независимым SQL guard `trg_crypto_payment_frozen_basis`: receipt, invoice/merchant/network/wallet/token, gross, tx и снимок тарифа неизменяемы также для администратора. Удаление и пометка удаления запрещены. Экономическое назначение исправляет ручная корректировка `payment_reversal` (основание `payment_attribution`).

Перепроведение сохраняет basis/fee и требует полной компенсации исходных строк невыясненных, merchant net и processing fee (`fn_crypto_assert_payment_attribution`). Unpost также требует полного сторно; произвольная смена posted flag не проходит commit. Повторное включение перевода после reorg находит исходный receipt по логическому эффекту, и следующее поколение распределяет его снова. Старые proof bytes не переписываются.

## 6. Аудит MVP

Отдельной истории версий, снимков до/после и автоматического восстановления нет.
Сохраняются стандартные `version`, `postedVersion` и события `sys_event_log`.
Команды allow/deny записывают факт действия и исполнителя; значения полей до и
после команды не сохраняются как отдельный снимок.

## 7. Файловая карта

```path
internal/domain/strict_document.go — StrictDocument и backend guard
internal/domain/strict_document_test.go — lifecycle и запреты
internal/metadata/registry.go — DocumentProtection и EntityActionDef
internal/content/document_registrations.go — защита и actions CryptoNetworkOperation
internal/infrastructure/http/v1/handlers/crypto_network_operation.go — allow/deny API и event log
internal/infrastructure/http/v1/middleware/admin_security.go — RequireSystemAdmin и SensitiveActionGuard
frontend/components/shared/auto-form.tsx — динамическая блокировка, actions и refetch
frontend/components/shared/form-toolbar.tsx — меню «Ещё»
```

## Связанные документы

- [posting-engine.md](posting-engine.md) — удаление и повторная генерация движений.
- [auth-security.md](auth-security.md) — admin MFA, permissions и step-up.
- [../crypto/crypto-processing-architecture.md](../crypto/crypto-processing-architecture.md) — жизненный цикл `CryptoNetworkOperation`.
