# JSON-контракты

> **TL;DR:** Прикладной код использует только `internal/core/json`. Обычные
> HTTP/JSONB данные работают на строгих семантиках `encoding/json/v2`; v1
> изолирован только внутри явно названных, уже версионированных byte-протоколов.

> **Тип:** Reference
> **Аудитория:** Developer
> **Связанные:** [development-rules.md](development-rules.md), [request-lifecycle.md](../guide/04-request-lifecycle.md), [infrastructure-layer.md](../systems/infrastructure-layer.md)

---

## 1. Единая точка входа

Прямой импорт `encoding/json` в прикладном коде запрещён архитектурным тестом.
Основной facade — `internal/core/json`; Vault-плагины являются отдельными Go
modules и имеют локальный facade в `internal/json`.

Дефолт Metapus:

- case-sensitive имена полей;
- duplicate object names и невалидный UTF-8 отклоняются;
- nil slice/map сериализуются как `[]`/`{}`;
- HTML/JavaScript escaping выключен;
- `time.Duration` сохраняет существующий числовой контракт в наносекундах.

Глобальной v1-семантики `omitempty` нет. Для `bool` и числовых полей, которые
должны исчезать при Go zero value, используется явный тег `omitzero`. Это
делает wire-контракт видимым на самом поле и не меняет поведение остальных
типов скрытой глобальной опцией.

## 2. Профили

| Контур | API/адаптер | Семантика |
|---|---|---|
| Обычный marshal/unmarshal | `json.Marshal`, `json.Unmarshal` | product v2 defaults |
| Закрытый HTTP request | Gin `jsoncodec` / `json.UnmarshalStrict` | v2 + reject unknown + ровно одно значение |
| Dynamic `any` | `json.UnmarshalPreserveNumbers` / `Decoder.UseNumber` | number literals не становятся `float64` |
| PostgreSQL JSON/JSONB | `postgres.ConfigurePool`, `postgres.OpenPool` | те же product v2 defaults |
| Unsigned fingerprint schema v1 | `json.CanonicalizeLegacyV1` | точные исторические bytes `encoding/json` v1 |
| iTRX HMAC body | `json.MarshalLegacyV1NoHTMLEscape` | исторический v1 encoder с `SetEscapeHTML(false)` |
| Vault signing plugins | module-local `internal/json` | полный `DefaultOptionsV1()` |

`Unmarshal` намеренно не отклоняет unknown fields вне HTTP: JSONB и ответы
внешних провайдеров должны допускать additive schema evolution. Закрытый
request DTO, напротив, fail-fast отклоняет опечатки и неверный регистр.

## 3. Byte-stable протоколы

`Deterministic(true)` сам по себе не является межверсионным wire-стандартом:
Go гарантирует стабильность для одного и того же binary, но не для разных
build/toolchain. Поэтому generic `Canonical` в проекте отсутствует.

Существующие fingerprint/HMAC bytes заморожены golden-тестами и названы
`LegacyV1`. Новый подписываемый формат обязан:

1. получить новый `schema_version`;
2. определить канонизацию в спецификации (предпочтительно RFC 8785/JCS);
3. передавать финансовые целые значения строками, если они могут превышать
   безопасный IEEE-754 диапазон;
4. иметь golden vectors, проверяемые всеми производителями и потребителями;
5. не менять алгоритм существующей версии in-place.

## 4. PostgreSQL pools

Любой новый product-код создаёт raw `*pgxpool.Pool` через
`postgres.OpenPool`. Если требуется собственный `pgxpool.Config`, после всех
локальных `AfterConnect` hooks вызывается `postgres.ConfigurePool`; helper
композирует hooks и устанавливает JSON/JSONB codecs на каждое соединение.

Прямой `pgxpool.New` допустим только в изолированном integration test, который
не проверяет product JSON serialization.


## Денежные HTTP-контракты

ERP-суммы (`unitPrice`, `amount`, `vatAmount`, `totalAmount`, `totalVat`) передаются как канонические строки целых minor units. Пример: `"9007199254740993"`. JSON number, дроби, экспоненты, пробелы, ведущие нули, `-0` и значения вне int64 отклоняются. Названия полей сохраняются. Go DTO использует `httpvalue.MinorUnits`; generic HTTP-проекции используют scoped codec из `http/jsoncodec`. `core/json` и domain `types.MinorUnits` сохраняют числовой codec для хранения и подписываемых протоколов.

Frontend использует проверяемый `MinorUnits` из `lib/money.ts`; ввод major units преобразуется без `Number`/`parseFloat` и без неявного округления. Вычисленный НДС округляется по действующему backend-правилу (half away from zero). Этот контракт применяется к ERP money-полям и общим экспортам; текущие криптодокументы используют отдельный `RawAmount`-контракт.

Денежная колонка отчёта объявляет `money: {currencyId, currencyCode, decimalPlaces}` — имена полей строки с валютной единицей. Эти проекции обязательны и проходят FLS/RLS/CEL. Значение колонки — строковые minor units; при недоступной валюте оно отсутствует/null. Итоги по денежной колонке — массив `{amount, currencyId, currencyCode, decimalPlaces}`, отдельно для каждой валюты; итог нескольких валют без конвертации не создаётся. Сумма нескольких int64 может превышать диапазон одной записи и остаётся точной целой строкой. XLSX сохраняет более 15 значащих десятичных цифр как текст, иначе использует числовую ячейку с форматом масштаба валюты.

Экспорт табличной части передаёт денежные ячейки строками minor units и объявляет для колонки `type: "money"`, `decimalPlaces: 0..18`. Количество уже в major units и повторно не масштабируется. Отдельный `npm run test:unit` проверяет точные числовые преобразования без браузера; входит в CI.

## Генерируемые wire-типы и проверки во время выполнения

`internal/infrastructure/http/contracts.Registry()` явно регистрирует request/response DTO и дополнительные проекции отчётов, поиска и ссылок. `go run ./cmd/http-contracts` обновляет `frontend/types/generated/http-contracts.ts`, `frontend/lib/generated/http-contract-schemas.ts` и `frontend/lib/generated/http-contract-routes.ts`; `-check` проверяет актуальность всех трёх файлов в CI и Makefile. Новый DTO добавляется в реестр вместе с подключением схемы к API consumer. Coverage-тест обнаруживает пропущенные экспортируемые JSON DTO.

Генератор использует действующий `http/jsoncodec`, чтобы определить присутствие и nullable-представление полей. В JSON v2 `omitempty` не убирает false и numeric zero; `omitzero` убирает нулевое Go-значение. Указатель в response с `omitempty` — опциональное поле без null, указатель без omission — nullable. Для input учитывается `binding:"required"`. Required денежное поле принимает строку `"0"`, но не null или JSON number. Неизвестные custom codecs и JSON options требуют явного transport mapping и останавливают генерацию.

Zod-схемы проверяют wire-структуру и денежный codec, а не заменяют прикладную валидацию и авторизацию. Request-объекты закрыты; намеренно динамические attributes/raw JSON имеют JsonValue/JsonObject. Enum в форме может быть уже строкового поля Go DTO: masked FLS response не должен превращаться в выдуманный default. Выбранное значение проверяется при построении изменения. Ручные схемы с дополнительными правилами связываются с generated DTO через satisfies.

Фабрики CatalogApi/DocumentApi требуют схемы чтения, создания и изменения. Report grouping и export используют сохранённый запрос сформированного результата: несформированные правки фильтра не меняют данные экспортируемой таблицы. Preview и typed refs раскрывают денежную сумму только вместе с независимо авторизованными кодом и scale валюты. Отсутствие валютной единицы не означает scale 2.
