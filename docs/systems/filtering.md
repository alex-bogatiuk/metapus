# Система фильтрации

> **TL;DR:** Frontend строит интерфейс фильтрации на лету, получая метаданные от Backend (`/api/v1/meta/:name/filters`). Backend автоматически парсит JSON-фильтры в SQL с поддержкой подзапросов, иерархий и масштабирования чисел (денег и количества).

> **Тип:** Concept
> **Аудитория:** Developer

---

## 1. Архитектура: Metadata-Driven

Вместо того чтобы вручную писать фильтры для каждого справочника и документа:

1. **Backend** инспектирует структуру (например, `Invoice`) через reflection.
2. Определяет типы полей: `id.ID` → `reference`, `types.MinorUnits` → `money`, `time.Time` → `date`.
3. Отдаёт плоский массив `FilterFieldMeta` на фронтенд.
4. **Frontend** (используя `FilterSidebar`) строит нужные виджеты (Datepicker, Select, Input) на основе `fieldType`.
5. Frontend отправляет массив условий в query-параметре `?filter=[{"field":"...", "operator":"eq", "value":"..."}]`.

## 2. Фильтрация табличных частей (Dot-notation)

Если фронтенд отправляет фильтр с точкой в имени поля (например, `"lines.product_id"`), backend понимает, что нужно фильтровать по табличной части.

Он автоматически генерирует SQL `EXISTS` подзапрос:
```sql
EXISTS (
    SELECT 1 FROM doc_goods_receipt_lines
    WHERE doc_goods_receipt_lines.document_id = doc_goods_receipts.id
      AND product_id = $1
)
```
- Оператор `eq` означает: «найди документы, в которых **хотя бы одна строка** содержит этот товар».
- Негативные операторы (`neq`) генерируют `NOT EXISTS`.

> [!WARNING]
> Для работы `EXISTS` подзапросов репозиторий документа обязан зарегистрировать табличную часть (Whitelist) через метод `repo.RegisterTablePart(...)`.

## 3. Масштабирование числовых типов

Metapus хранит все числа в БД как целые числа (`int64`), чтобы избежать проблем с плавающей точкой (floats).

### Quantity (Статическое масштабирование)
Количество всегда умножается на 10 000. 
Фронтенд присылает `10`, backend видит `fieldType: number`, `scale: 10000` и преобразует значение в `100000` перед подстановкой в SQL `WHERE quantity = ?`.

### Money (Динамическое масштабирование)
Деньги хранятся с учётом валюты документа. 10 USD = 1000 центов, а 10 JPY = 10 иен.
Масштабирование происходит **на уровне SQL**:
```sql
WHERE total_amount = ROUND(CAST($1 AS NUMERIC) * (
    SELECT minor_multiplier FROM cat_currencies
    WHERE id = doc_goods_receipts.currency_id
))
```

## 4. Операторы и Иерархия

Поддерживаются стандартные операторы: `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `in`, `nin`, `contains`, `null`, `not_null`.

**Специальные операторы:**
Для справочников с иерархией (`ParentID`) доступны операторы `in_hierarchy` и `nin_hierarchy`.
Они автоматически разворачиваются в рекурсивный CTE (Common Table Expression), позволяя одним условием выбрать "Папку А и все вложенные в неё подпапки и элементы".

> [!NOTE]
> Защита от SQL-инъекций гарантируется Whitelist-проверкой: если `item.Field` нет в списке разрешённых полей, фильтр отклоняется до построения SQL-запроса.
