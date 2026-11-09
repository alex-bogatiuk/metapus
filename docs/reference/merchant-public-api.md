# Merchant API

> **TL;DR:** Merchant API позволяет внешней системе мерчанта создавать крипто-инвойсы, получать их статус и выдавать постоянные депозитные адреса клиентам.
> **Тип:** Reference
> **Аудитория:** Merchant, Developer
> **Связанные:** [systems/crypto-processing.md](../systems/crypto-processing.md)

---

## 1. Назначение

Merchant API используется сервером интернет-магазина, биллинга или CRM для приёма криптоплатежей через Metapus.

Основной сценарий:

1. Сервер мерчанта создаёт инвойс через `POST /merchant/v1/invoices`.
2. Metapus возвращает сумму, валюту, сеть, адрес кошелька и срок действия инвойса.
3. Мерчант показывает клиенту адрес или перенаправляет клиента на payment page.
4. Metapus отслеживает оплату в блокчейне.
5. Мерчант получает webhook или проверяет статус через API.
6. Товар или услуга отгружается только после статуса `completed`.

Merchant API не использует пользовательский JWT. Все запросы аутентифицируются API-ключом мерчанта.

### Ограничения Ethereum Sepolia v1

Metapus обнаруживает входящий ETH только в успешных top-level транзакциях с
`value > 0`, направленных на выданный EOA. Internal value transfers из
исполнения smart contract, trace API, smart-wallet/account-abstraction payouts
и переводы без top-level `to` не поддерживаются. Не используйте эти способы
для оплаты инвойса: такой перевод не изменит его статус автоматически.

PYUSD обнаруживается только по `Transfer` logs официального allowlisted
Sepolia contract. Статус `confirmed` наступает после 12 подтверждений либо
если block достиг Ethereum `finalized` head — что произойдёт раньше.

---

## 2. Base URL

```text
https://<your-metapus-domain>/merchant/v1
```

Примеры ниже используют переменные:

```bash
export API_URL="https://<your-metapus-domain>"
export API_KEY="mk_live_or_test_key"
```

---

## 3. Аутентификация

Каждый запрос к `/merchant/v1/*` должен содержать API-ключ.

| Заголовок | Обязательный | Описание |
|-----------|:---:|----------|
| `X-Api-Key` | Да | API-ключ мерчанта. Показывается один раз при создании в Merchant Portal. |
| `Content-Type: application/json` | При JSON body | Формат тела запроса. |
| `Idempotency-Key` | Для POST/PUT/PATCH/DELETE | Стабильный ключ повтора команды, до 200 байт. |

```http
X-Api-Key: mk_...
Content-Type: application/json
```

Tenant определяется сервером по API-ключу через meta DB key index. Merchant API не принимает tenant hint в header, query или path.

API-ключ может иметь ограниченный набор scopes.

| Scope | Что разрешает |
|-------|---------------|
| `invoice:create` | Создание инвойсов |
| `invoice:read` | Чтение инвойсов |
| `address:create` | Создание постоянных адресов клиентов |
| `webhook:endpoint:read/write` | Чтение и управление webhook endpoints |
| `webhook:event:read/redeliver` | Debugger событий и ручная переотправка |

> [!IMPORTANT]
> Храните API-ключ только на backend-стороне. Не вставляйте `X-Api-Key` в frontend, mobile app или публичную checkout-страницу.

---

## 4. Доступные Валюты и Денежные Суммы

Все суммы в Merchant API передаются в minor units токена. Чтобы получить актуальный список доступных валют (`code`) и их разрядность (`decimalPlaces`), используйте:

```http
GET /merchant/v1/currencies
```

```bash
curl -s "$API_URL/merchant/v1/currencies" \
  -H "X-Api-Key: $API_KEY"
```

### Response 200

```json
{
  "items": [
    {
      "code": "USDT-TRC20",
      "symbol": "USDT",
      "name": "Tether USD (TRC-20)",
      "network": "TRON-NILE",
      "networkName": "TRON Nile Testnet",
      "standard": "TRC-20",
      "decimalPlaces": 6,
      "contractAddress": "TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf",
      "requiredConfirmations": 19,
      "finalityMode": "confirmations",
      "acceptsPayments": true
    }
  ]
}
```

| Пример | Decimal places | Human amount | API amount |
|--------|:---:|--------------|-----------:|
| USDT TRC-20 | 6 | `10.5 USDT` | `10500000` |
| USDT TRC-20 | 6 | `1 USDT` | `1000000` |
| BTC | 8 | `0.001 BTC` | `100000` |

Формула:

```text
amount = human_amount * 10^decimalPlaces
```

`amount` всегда целое число. Не передавайте дробные значения вроде `10.5` и не хардкодьте `decimalPlaces` — берите значение из `GET /merchant/v1/currencies`.

---

## 5. Создание Инвойса

```http
POST /merchant/v1/invoices
Idempotency-Key: order-100045-create
```

Создаёт платёжный инвойс и резервирует адрес кошелька для оплаты.

**Требуемый scope:** `invoice:create`

### Request

```json
{
  "amount": "10500000",
  "currency": "USDT-TRC20",
  "ttlMinutes": 60,
  "orderId": "order-100045",
  "description": "Оплата заказа #100045",
  "notificationUrl": "https://shop.example.com/webhooks/metapus",
  "returnUrl": "https://shop.example.com/orders/100045",
  "metadata": {"cartId": "cart-42"},
  "customerId": "customer-42"
}
```

| Поле | Тип | Обязательное | Описание |
|------|-----|:---:|----------|
| `amount` | string | Да | Каноническая целая сумма в minor units. |
| `currency` | string | Да | Код токена из `GET /merchant/v1/currencies`, например `USDT-TRC20`. |
| `orderId` | string | Нет | Номер заказа мерчанта, если заказ существует. Непустое значение уникально внутри мерчанта, 1–255 символов без пробелов по краям. |
| `externalId` | string | Нет | Бизнес-ID операции или документа в 1С/ERP для сверки. Непустое значение уникально внутри мерчанта, 1–255 символов без пробелов по краям. |
| `description` | string | Нет | Описание для payment page и кабинета мерчанта. |
| `ttlMinutes` | integer | Нет | Срок действия инвойса. По умолчанию — настройка мерчанта (`60`, если не задана); допустимо `5`-`10080`. |
| `notificationUrl` | string | Нет | Заменяет endpoint fan-out; требует активный default endpoint и использует его secret. Только публичный HTTPS URL. |
| `returnUrl` | string | Нет | HTTPS UX-redirect. Никогда не является источником статуса. |
| `metadata` | object | Нет | До 50 строковых пар; возвращается в GET и events. |
| `customerId` | string | Нет | ID клиента в системе мерчанта (UUID, email и т.д.) для поиска и аналитики. |

### Response 201

```json
{
  "id": "019e17b9-b22f-7bd8-bfd5-aad109b1410b",
  "checkoutRef": "ci_0f8d4c6b0d1e4a5f9b2c3d4e5f607182",
  "payUrl": "https://pay.example.com/pay/i/ci_0f8d4c6b0d1e4a5f9b2c3d4e5f607182",
  "status": "created",
  "acceptsPayments": true,
  "resourceVersion": 1,
  "expectedAmount": "10500000",
  "receivedAmount": "0",
  "confirmedAmount": "0",
  "creditedAmount": "0",
  "overpaidAmount": "0",
  "currency": "USDT-TRC20",
  "network": "TRON-NILE",
  "decimalPlaces": 6,
  "walletAddress": "TLfG3MEDv4koUoxJnLv5k4jPiaHbRckYbz",
  "requiredConfirmations": 19,
  "finalityMode": "confirmations",
  "expiresAt": "2026-05-12T11:00:00Z",
  "orderId": "order-100045",
  "description": "Оплата заказа #100045",
  "createdAt": "2026-05-12T10:00:00Z"
}
```

| Поле | Описание |
|------|----------|
| `id` | ID инвойса в Metapus. |
| `checkoutRef` | Opaque публичная ссылка инвойса. |
| `payUrl` | Абсолютный URL встроенной payment page. |
| `status` | Текущий статус инвойса. Новый инвойс получает `created`. |
| `acceptsPayments` | Можно ли показывать адрес для новой оплаты. В режиме «1 инвойс — 1 платёж» становится `false` сразу после обнаружения первого подходящего перевода. |
| `expectedAmount`, `receivedAmount`, `confirmedAmount`, `creditedAmount` | Точные суммы в minor units. |
| `currency` | Код токена. |
| `network` | Стабильный код блокчейн-сети. |
| `decimalPlaces` | Количество десятичных знаков токена для перевода из minor units. |
| `walletAddress` | Адрес для оплаты. Клиент должен отправить на него точную сумму. |
| `expiresAt` | Время истечения инвойса в RFC3339/ISO 8601. |
| `orderId` | ID заказа мерчанта, если был передан. |
| `description` | Описание, если было передано. |
| `createdAt` | Время создания в RFC3339/ISO 8601. |

### cURL

```bash
curl -s "$API_URL/merchant/v1/invoices" \
  -X POST \
  -H "X-Api-Key: $API_KEY" \
  -H "Idempotency-Key: order-100045-create" \
  -H "Content-Type: application/json" \
  -d '{
    "amount": "10500000",
    "currency": "USDT-TRC20",
    "ttlMinutes": 60,
    "orderId": "order-100045",
    "description": "Оплата заказа #100045",
    "notificationUrl": "https://shop.example.com/webhooks/metapus",
    "customerId": "customer-42"
  }'
```

---

## 6. Payment Page

После создания инвойса можно показать клиенту собственный checkout или открыть встроенную payment page Metapus.

```text
https://pay.example.com/pay/i/ci_0f8d4c6b0d1e4a5f9b2c3d4e5f607182
```

Используйте `payUrl` из ответа API. Payment page показывает сумму, токен, сеть, адрес, QR-код, таймер истечения и прогресс подтверждений.

Для server-to-server сценариев используйте `walletAddress` вместе с `acceptsPayments` из ответа API. Когда `acceptsPayments` равно `false`, скройте адрес и QR-код на своей странице оплаты.

---

## 7. Получение Инвойса и Сверка Поступлений (Reconciliation)

### 7.1. Получение одного инвойса

```http
GET /merchant/v1/invoices/{id}
```

Возвращает текущий статус инвойса и его on-chain платежи. Мерчант может читать только свои инвойсы.

**Требуемый scope:** `invoice:read`

### Response 200

```json
{
  "object": "crypto_invoice",
  "id": "019e17b9-b22f-7bd8-bfd5-aad109b1410b",
  "checkoutRef": "ci_0f8d4c6b0d1e4a5f9b2c3d4e5f607182",
  "payUrl": "https://pay.example.com/pay/i/ci_0f8d4c6b0d1e4a5f9b2c3d4e5f607182",
  "status": "completed",
  "acceptsPayments": false,
  "resourceVersion": 4,
  "expectedAmount": "10500000",
  "receivedAmount": "10500000",
  "confirmedAmount": "10500000",
  "feeAmount": "105000",
  "creditedAmount": "10500000",
  "netAmount": "10395000",
  "overpaidAmount": "0",
  "currency": "USDT-TRC20",
  "network": "TRON-NILE",
  "decimalPlaces": 6,
  "walletAddress": "TLfG3MEDv4koUoxJnLv5k4jPiaHbRckYbz",
  "requiredConfirmations": 19,
  "finalityMode": "confirmations",
  "expiresAt": "2026-05-12T11:00:00Z",
  "orderId": "order-100045",
  "customerId": "customer-42",
  "description": "Оплата заказа #100045",
  "metadata": {},
  "completedAt": "2026-05-12T10:04:12Z",
  "createdAt": "2026-05-12T10:00:00Z",
  "asOf": "2026-05-12T10:05:00Z",
  "payments": [
    {
      "id": "019e17ba-3100-7a12-9abc-112233445566",
      "txHash": "7c9d...e1f0",
      "logIndex": 0,
      "amount": "10500000",
      "feeAmount": "105000",
      "netAmount": "10395000",
      "status": "settled",
      "canonicality": "canonical",
      "finalityStatus": "solidified",
      "confirmations": 19,
      "requiredConfirmations": 19,
      "detectedAt": "2026-05-12T10:03:15Z",
      "confirmedAt": "2026-05-12T10:04:12Z"
    }
  ]
}
```

### cURL

```bash
curl -s "$API_URL/merchant/v1/invoices/019e17b9-b22f-7bd8-bfd5-aad109b1410b" \
  -H "X-Api-Key: $API_KEY"
```

### 7.2. Список инвойсов за период (Reconciliation API)

```http
GET /merchant/v1/invoices
```

Позволяет внешней учётной системе мерчанта (ERP/биллингу) выгружать инвойсы и их платежи за период с помощью стабильной cursor-пагинации без пропусков и дублей.

**Требуемый scope:** `invoice:read`

| Query-параметр | Тип | Описание |
|----------------|-----|----------|
| `status` | `string` | Фильтр по статусам через запятую (например, `completed,confirmed,underpaid`). |
| `currency` | `string` | Код валюты/токена (например, `USDT-TRC20`). |
| `orderId` | `string` | Точный поиск по `orderId` мерчанта. |
| `externalId` | `string` | Точный поиск по `externalId`. |
| `customerId` | `string` | Точный поиск по `customerId`. |
| `createdFrom`, `createdTo` | `RFC3339` | Полуоткрытый интервал `[createdFrom, createdTo)` по времени создания (`createdAt`). |
| `completedFrom`, `completedTo` | `RFC3339` | Полуоткрытый интервал `[completedFrom, completedTo)` по времени зачисления в ledger (`completedAt`). При передаче любого из этих параметров выборка автоматически сортируется по `(completedAt DESC, id DESC)`. |
| `limit` | `int` | Размер страницы: от `1` до `100` (по умолчанию `50`). |
| `cursor` | `string` | Непрозрачный курсор `nextCursor` из предыдущего ответа. |

#### Формула финансовой сверки (в minor units)

Для каждого проведённого инвойса (`status = completed`) и каждого канонического платежа выполняется тождество:
- **Gross (брутто):** `confirmedAmount` / `creditedAmount`
- **Fee (комиссия платформы):** `feeAmount`
- **Net (чистыми в баланс мерчанта):** `netAmount = creditedAmount - feeAmount`

```bash
curl -s "$API_URL/merchant/v1/invoices?status=completed&completedFrom=2026-05-01T00:00:00Z&completedTo=2026-05-02T00:00:00Z&limit=50" \
  -H "X-Api-Key: $API_KEY"
```

#### Response 200

```json
{
  "object": "list",
  "items": [
    {
      "object": "crypto_invoice",
      "id": "019e17b9-b22f-7bd8-bfd5-aad109b1410b",
      "status": "completed",
      "expectedAmount": "10500000",
      "receivedAmount": "10500000",
      "confirmedAmount": "10500000",
      "feeAmount": "105000",
      "creditedAmount": "10500000",
      "netAmount": "10395000",
      "overpaidAmount": "0",
      "currency": "USDT-TRC20",
      "network": "TRON-NILE",
      "decimalPlaces": 6,
      "orderId": "order-100045",
      "customerId": "customer-42",
      "completedAt": "2026-05-12T10:04:12Z",
      "createdAt": "2026-05-12T10:00:00Z",
      "payments": []
    }
  ],
  "hasMore": false
}
```


---

## 8. Статусы Инвойса

| Статус | Значение | Действие мерчанта |
|--------|----------|-------------------|
| `created` | Инвойс создан, оплата ещё не обнаружена. | Покажите клиенту адрес и сумму. |
| `partially_paid` | Получена часть суммы. | Если `acceptsPayments` равно `false`, ожидайте финализации выбранного платежа; иначе возможна доплата до TTL. |
| `paid` | Полная сумма получена, но нужное число подтверждений ещё не набрано. | Не отгружайте товар. |
| `confirmed` | Каноническая сумма достигла finality сети. | Не отгружайте товар. |
| `completed` | Finalized сумма успешно проведена в ledger мерчанта. | Можно идемпотентно выдать товар. |
| `underpaid` | TTL истёк после частичной оплаты либо единственный выбранный платёж финализирован на меньшую сумму. | Не исполняйте заказ; примените политику доплаты/возврата. |
| `reversed` | Финальный платёж потерял каноничность и компенсирован reversal-проводками. | Остановите settlement и обработайте incident. |
| `expired` | TTL истёк без полной оплаты. | Создайте новый инвойс. |
| `cancelled` | Инвойс отменён. | Не принимайте оплату по этому инвойсу. |

> [!WARNING]
> Финальным статусом для отгрузки является только `completed`. Нельзя выдавать товар по `paid`, `confirmed`, `returnUrl` или query string.

`invoice.settled` зарезервирован для будущего payout/settlement-контура и в
текущей версии API не отправляется.

`invoice.viewed` также не является публичным webhook: открытие checkout
учитывается только внутренними полями `view_count`, `first_viewed_at` и
`last_viewed_at`; status polling этот счётчик не увеличивает.

---

## Привязка реквизитов мерчанта к пополнению

После вебхука `invoice.created` или события платежа сохраните `invoiceId` и передайте бизнес-ID операции или учётного документа. Пополнение баланса не требует создания заказа:

```bash
curl -X PATCH "$BASE_URL/merchant/v1/invoices/$INVOICE_ID/reference" \
  -H "X-Api-Key: $API_KEY" \
  -H "Idempotency-Key: topup-entry-99881-reference" \
  -H "Content-Type: application/json" \
  -d '{"externalId":"1C-entry-99881","metadata":{"sap_doc":"45001"}}'
```

Требуется scope `invoice:create`. Ответ `200` — стандартный объект инвойса с присвоенными реквизитами. Можно передать только `externalId`, только `orderId` либо оба поля. Метод доступен только для автоматических пополнений persistent-кошельков и не зависит от статуса, включая `completed` и `reversed`. Новые автоматические пополнения создаются с пустыми `externalId` и `orderId`; хэш блокчейн-транзакции доступен в `payments[].txHash`.

Запрос содержит хотя бы один из реквизитов `orderId` или `externalId`. Каждый переданный реквизит — строка из 1–255 символов без пробелов по краям; регистр учитывается. Каждый реквизит независимо уникален внутри мерчанта; у разных мерчантов значения могут совпадать. Присвоенный реквизит нельзя заменить или очистить. Пропущенное поле сохраняет значение, поэтому к ранее привязанному `externalId` можно позже добавить `orderId` и наоборот. При совпадающих реквизитах разрешена замена `metadata`: отсутствие поля сохраняет словарь, объект заменяет его, `{}` очищает. Пустые строки, `null`, нестроковые значения и неизвестные поля, включая суммы, статус и системный тип операции, отклоняются с `400`. Лимиты метаданных: 50 пар, непустой ключ до 40 UTF-8 байт, значение до 500 UTF-8 байт. Символ NUL (`\u0000`) запрещён в обоих реквизитах, ключах и значениях `metadata`; такой запрос возвращает `400`.

Для повторов используйте тот же `Idempotency-Key` и тело запроса. Повтор с новым ключом и совпадающими данными также возвращает `200` без дополнительного изменения инвойса. Другой запрос с уже использованным ключом получает `409`. Недостаточный scope дает `403`, чужой, отсутствующий или удаленный инвойс — `404`. Обычный checkout-инвойс, замена реквизита и коллизия с другим инвойсом мерчанта — `409`.

Актуальные `orderId`, `externalId` и `metadata` доступны через GET инвойса и список Merchant API; фильтры `orderId` и `externalId` выполняют точное сопоставление. В портале номер заказа виден в списке, карточке, поиске и последней колонке CSV `Order ID`. Событие `invoice.updated` не отправляется. Уже созданные webhook-снимки остаются прежними, в том числе при повторной доставке; последующие события жизненного цикла содержат актуальные данные.

При экспорте в CSV текстовые значения, похожие на формулы Excel, получают префикс табуляции внутри кавычек. Это относится к начальным `=`, `+`, `-`, `@` и их полноширинным вариантам, в том числе после начальных пробелов или апострофов; также защищаются значения с начальной табуляцией или переводом строки. Префикс остаётся частью CSV-ячейки при программном импорте. В БД и API значения сохраняются буквально; для точной автоматической сверки используйте Merchant API.

## 9. Идемпотентность

Для всех изменяющих запросов (POST/PUT/PATCH/DELETE) обязателен `Idempotency-Key` длиной до 200 байт. Он защищает от дублей. `orderId` остаётся отдельным индексируемым бизнес-идентификатором.

Если `POST /merchant/v1/invoices` повторить с тем же `orderId`, Metapus вернёт уже существующий инвойс этого мерчанта вместо создания нового.

```bash
# Первый запрос: 201 Created
curl -s -o /dev/null -w "%{http_code}\n" "$API_URL/merchant/v1/invoices" \
  -X POST \
  -H "X-Api-Key: $API_KEY" \
  -H "Idempotency-Key: order-100045-create" \
  -H "Content-Type: application/json" \
  -d '{"amount":"10500000","currency":"USDT-TRC20","orderId":"order-100045"}'

# Повтор того же ключа: 201 Created, тот же invoice id, Idempotent-Replayed: true
curl -s -o /dev/null -w "%{http_code}\n" "$API_URL/merchant/v1/invoices" \
  -X POST \
  -H "X-Api-Key: $API_KEY" \
  -H "Idempotency-Key: order-100045-create" \
  -H "Content-Type: application/json" \
  -d '{"amount":"10500000","currency":"USDT-TRC20","orderId":"order-100045"}'
```

Повтор того же ключа воспроизводит сохранённый результат и исходный статус (например, 201). Новый ключ с уже существующим совместимым `orderId` возвращает 200; другой fingerprint с прежним ключом — 409.

Правило интеграции: один `orderId` должен соответствовать одному заказу. Не переиспользуйте его для другой суммы, валюты или клиента.

---

## 10. Постоянный Адрес Клиента

```http
POST /merchant/v1/addresses
```

Выдаёт постоянный депозитный адрес для клиента мерчанта. Такой адрес подходит для top-up сценариев, где клиент пополняет баланс без создания отдельного инвойса на каждый платёж.

**Требуемый scope:** `address:create`

### Request

```json
{
  "currency": "USDT-TRC20",
  "customerId": "customer-42"
}
```

| Поле | Тип | Обязательное | Описание |
|------|-----|:---:|----------|
| `currency` | string | Да | Код токена. Используется для определения блокчейн-сети. |
| `customerId` | string | Да | ID клиента в системе мерчанта. |

### Response 200

```json
{
  "address": "TLfG3MEDv4koUoxJnLv5k4jPiaHbRckYbz",
  "currency": "USDT-TRC20",
  "network": "TRON-NILE",
  "customerId": "customer-42"
}
```

Помимо обязательного `Idempotency-Key`, операция имеет бизнес-идентичность `merchant + customerId + network`: повторный запрос для того же клиента и сети вернёт тот же адрес.

Количество постоянных адресов ограничено policy для `merchant + network`. При достижении лимита новый `customerId` получит `422 MERCHANT_PERSISTENT_ADDRESS_LIMIT_EXCEEDED`; повтор существующего `customerId` по-прежнему вернёт ранее назначенный адрес.

---

## 11. Webhooks

Metapus отправляет webhook всем активным endpoint с подходящей подпиской. `notificationUrl` инвойса заменяет fan-out и использует secret default endpoint.

Webhook URL должен быть публичным HTTPS URL. Локальные адреса, приватные IP, metadata endpoints и redirects не используются для доставки.

### Headers

```http
Content-Type: application/json
Metapus-Event-Id: evt_01J...
Metapus-Delivery-Id: del_01J...
Metapus-Signature: t=1787724723,v1=8b1c...
```

| Header | Описание |
|--------|----------|
| `Metapus-Event-Id` | Стабильный ID бизнес-события. Используйте для дедупликации. |
| `Metapus-Delivery-Id` | Уникальный ID конкретной попытки. |
| `Metapus-Signature` | `t=<unix>,v1=<HMAC>`; при ротации содержит две `v1`. |

### Payload

```json
{
  "id": "evt_01J...",
  "object": "event",
  "apiVersion": "2026-08-26",
  "type": "invoice.completed",
  "createdAt": "2026-08-26T06:12:03Z",
  "data": {
    "object": {
      "id": "019e17b9-b22f-7bd8-bfd5-aad109b1410b",
      "status": "completed",
      "resourceVersion": 7,
      "orderId": "order-100045",
      "metadata": {},
      "expectedAmount": "10000000",
      "receivedAmount": "10000000",
      "confirmedAmount": "10000000",
      "creditedAmount": "10000000",
      "overpaidAmount": "0"
    }
  }
}
```

Обработчик должен игнорировать неизвестные поля и типы событий. Денежные поля — строки minor units.

### Events

| Event | Когда отправляется |
|-------|--------------------|
| `invoice.created` | Инвойс создан. |
| `invoice.paid` | Получена полная сумма, но finality ещё не достигнута. |
| `invoice.confirmed` | Каноническая сумма достигла platform finality. Не отгружайте товар. |
| `invoice.completed` | Подтверждённая сумма проведена в ledger. Единственный безопасный триггер выдачи товара. |
| `invoice.overpaid`, `invoice.underpaid` | Переплата или недоплата после TTL либо финализации единственного платежа. |
| `invoice.expired`, `invoice.cancelled` | Терминальное состояние без выполнения заказа. |
| `invoice.reversed` | Post-finality reorg/replacement; остановите исполнение и откройте incident. |
| `invoice.late_payment` | Поздний finalized перевод на закрытый инвойс; статус не reopen и товар не выдаётся. |
| `payment.*` | Опциональные технические события detected/confirming/confirmed/reorged/replaced. |
| `withdrawal.confirmed`, `withdrawal.failed`, `withdrawal.partially_completed` | Терминальный результат вывода, включая частичное исполнение и возврат неподписанного остатка. |
| `wallet.risk_changed` | Новая версия AML-проекции физического inventory wallet/token. Отдельная opt-in подписка. Не означает новое зачисление или разрешение вывода. |
| `deposit_address.rotated` | Замена постоянного адреса клиента в кабинете. Обновите реквизиты по `customerId + networkId`; старый адрес выведен из обращения. |

В `deposit_address.rotated` поля `data.object`: `id`, `customerId`, `networkId`, `oldWalletId`, `newWalletId`, `oldAddress`, `newAddress`, `reason`, `resourceVersion`, `rotatedAt`. Дедуплицируйте события по ID и применяйте версии назначения по возрастанию для `customerId + networkId`. Поступления на выведенный адрес требуют ручного разбора и не создают автоматическое пополнение. Повторная выдача адреса для этой пары клиента и сети возвращает текущие реквизиты. [Правила ротации и поздних поступлений](../operations/crypto-deposit-address-rotation.md).

Для `wallet.risk_changed` в `data.object` передаются `walletId`, `tokenId`, `networkId`, `previousRiskClass`, `riskClass`, `riskRevision`, `scopeVersion`, `reason`, `inventoryCovered`, `expiresAt`. Возможные классы: `low`, `medium`, `high`, `unknown`; `expiresAt` может быть `null`. Событие может закрывать покрытие или обновлять evidence без смены класса. Обрабатывайте версии одного wallet/token по `riskRevision`, поскольку доставка может повторяться и нарушать порядок. `invoice.completed` подтверждает финансовое проведение; при включённом AML-контуре политика исполнения заказа должна также учитывать риск средств. Подробнее — [проекция риска и ограничения текущего этапа](../operations/crypto-aml-risk-projection.md).

### Подпись

Подпись считается от сырого тела запроса и timestamp.

```text
signed_payload = timestamp_from_Metapus-Signature + "." + raw_body
signature = HMAC-SHA256(signed_payload, webhook_secret)
```

Проверяйте подпись по raw body до JSON parse, принимайте timestamp не старше пяти минут и сравнивайте HMAC constant-time. Используйте verifier из официального TypeScript, Go или Python SDK в `sdk/`.

### Повторная доставка

Любой non-2xx, timeout или network error повторяется по шкале `0, 30s, 2m, 10m, 1h, 6h, 12h, 24h, 48h, 72h` с jitter ±10%. Redirect запрещён; `Retry-After` учитывается для `429/503` в пределах deadline.

Обработчик webhook должен быть идемпотентным:

- в одной DB-транзакции сохраняйте `event.id` с unique constraint и выполняйте бизнес-действие;
- события могут прийти повторно и не по порядку; при сомнении вызывайте `GET /invoices/{id}`;
- отвечайте `200 OK` быстро, а тяжёлую обработку переносите в очередь.

---

## 12. Ошибки

Ошибки возвращаются в едином JSON-формате.

```json
{
  "code": "VALIDATION_ERROR",
  "message": "unknown currency: BTC",
  "details": {
    "field": "currency"
  }
}
```

| HTTP | Code | Когда возникает |
|------|------|-----------------|
| `400` | `VALIDATION_ERROR` | Некорректный JSON, неизвестная валюта, неверный `ttlMinutes`, сумма `<= 0`. |
| `401` | `UNAUTHORIZED` | Нет `X-Api-Key`, ключ неверный, отозван или истёк. |
| `403` | `FORBIDDEN` | У API-ключа нет нужного scope. |
| `404` | `NOT_FOUND` | Инвойс не найден или принадлежит другому мерчанту. |
| `429` | `MERCHANT_WALLET_LEASE_LIMIT_EXCEEDED` | Достигнут лимит активных invoice lease или starts/minute; повторите запрос после `Retry-After`. |
| `422` | `MERCHANT_PERSISTENT_ADDRESS_LIMIT_EXCEEDED` | Достигнута квота постоянных адресов для мерчанта и сети. |
| `503` | `WALLET_POOL_EXHAUSTED` | Нет свободного адреса; повторите запрос после `Retry-After`. |
| `409` | `CONFLICT` | Конфликт бизнес-состояния. |
| `500` | `INTERNAL_ERROR` | Внутренняя ошибка сервера. |

---

## 13. Endpoint Reference

| Method | Path | Auth | Scope | Описание |
|--------|------|------|-------|----------|
| `POST` | `/merchant/v1/invoices` | `X-Api-Key` | `invoice:create` | Создать инвойс. |
| `GET` | `/merchant/v1/invoices/{id}` | `X-Api-Key` | `invoice:read` | Получить инвойс. |
| `PATCH` | `/merchant/v1/invoices/{id}/reference` | `X-Api-Key` | `invoice:create` | Привязать `externalId` и/или `orderId` к пополнению; обязателен `Idempotency-Key`. |
| `POST` | `/merchant/v1/invoices/{id}/cancel` | `X-Api-Key` | `invoice:create` | Идемпотентно отменить pre-finality инвойс. |
| `POST` | `/merchant/v1/addresses` | `X-Api-Key` | `address:create` | Выдать постоянный адрес клиенту. |
| CRUD | `/merchant/v1/webhook-endpoints` | `X-Api-Key` | `webhook:endpoint:read/write` | Endpoint registry и подписки. |
| `POST` | `/merchant/v1/webhook-endpoints/{id}/rotate-secret` | `X-Api-Key` | `webhook:endpoint:write` | Ротация с 24-часовым overlap. |
| `POST` | `/merchant/v1/webhook-endpoints/{id}/test` | `X-Api-Key` | `webhook:endpoint:write` | Тестовая доставка. |
| `GET` | `/merchant/v1/events` | `X-Api-Key` | `webhook:event:read` | Immutable event debugger. |
| `GET` | `/merchant/v1/events/{id}/deliveries` | `X-Api-Key` | `webhook:event:read` | История попыток. |
| `POST` | `/merchant/v1/events/{id}/redeliver` | `X-Api-Key` | `webhook:event:redeliver` | Переотправка на текущий endpoint. |

Secret возвращается только при создании endpoint и ротации. Удаление endpoint
не отменяет уже созданные автоматические доставки: они завершают 72-часовой
retry lifecycle по snapshot URL и secret version. Retired secret удаляется из
Vault после защитного 73-часового окна.

Customer-facing endpoints:

| Method | Path | Auth | Описание |
|--------|------|------|----------|
| `GET` | `/pay/i/{ci_ref}` | Нет | Встроенная payment page для инвойса. |
| `GET` | `/pay/l/{pl_ref}` | Нет | Встроенная payment page для payment link. |
| `GET` | `/api/v1/checkout/invoices/{ci_ref}` | Нет | JSON-данные payment page. |
| `GET` | `/api/v1/checkout/invoices/{ci_ref}/status` | Нет | Лёгкий polling статуса payment page. |
| `POST` | `/api/v1/checkout/links/{pl_ref}/redeem` | Нет | Создать инвойс из payment link. |

---

## 14. Production Checklist

- [ ] API-ключ хранится в secrets manager или backend env, не в frontend.
- [ ] Для каждой операции передаётся уникальный `Idempotency-Key`; `orderId` хранит ID заказа.
- [ ] Суммы передаются в minor units, без float.
- [ ] Клиенту показывается точная сумма, точная сеть и точный адрес.
- [ ] Товар или услуга отгружается только после `invoice.completed`.
- [ ] Webhook подпись проверяется по raw body до JSON parse.
- [ ] Webhook handler дедуплицирует бизнес-события по `event.id` в одной транзакции с выдачей товара.
- [ ] Webhook handler отвечает `2xx` быстро и выполняет тяжёлую обработку асинхронно.
- [ ] API-ключи ротируются и имеют минимально необходимые scopes.
- [ ] Тестовый и production ключи не смешиваются.
