# Cloud Deployment: Независимое версионирование тенантов

> Руководство по развертыванию нескольких версий для режима SaaS в Metapus.
> Каждый тенант может быть независимо обновлен до новой версии сервера без влияния на остальных.

---

## Обзор архитектуры

```
                         ┌──────────────────────┐
                         │      Nginx :80       │
                         │  (обратный прокси)   │
                         └──────┬───────────────┘
                                │
         ┌──────────────────────┼──────────────────────┐
         │ auth_request         │                      │
         ▼                      │                      │
┌────────────────┐    ┌────────┴────────┐    ┌────────┴────────┐
│ metapus-router │    │ metapus v1.2.0  │    │ metapus v1.3.0  │
│ /internal/route│    │ VERSION_GROUP=  │    │ VERSION_GROUP=  │
│(любая версия)  │    │ v1.2.0          │    │ v1.3.0          │
└────────┬───────┘    └─────────────────┘    └─────────────────┘
         │
         ▼
┌────────────────┐
│   PostgreSQL   │
│  metapus_meta  │
│(таблица tenants)│
└────────────────┘
```

### Жизненный цикл запроса

1. Клиент отправляет `GET /api/v1/catalog/items` с заголовком `X-Tenant-ID: acme-uuid`
2. Nginx перехватывает запрос и выполняет `auth_request` к `/internal/route`
3. Экземпляр router делает запрос к мета-базе: `SELECT version_group FROM tenants WHERE id = $1`
4. Router возвращает `200` с заголовком `X-Version-Group: v1.3.0`
5. Nginx через директиву `map` перенаправляет `v1.3.0` → в upstream `metapus_v1.3.0`
6. Запрос проксируется к правильному экземпляру сервера
7. Если группа версий тенанта не совпадает с сервером (ошибка конфигурации), сервер возвращает ошибку `421 Misdirected Request`

---

## Быстрый старт

### 1. Сборка версионированных образов

```bash
# Сборка v1.2.0
docker build --build-arg VERSION=v1.2.0 -t metapus:v1.2.0 -f Dockerfile .

# Сборка v1.3.0 (из другой ветки или тега)
git checkout v1.3.0
docker build --build-arg VERSION=v1.3.0 -t metapus:v1.3.0 -f Dockerfile .

# Сборка latest (для router и сервера по умолчанию)
docker build --build-arg VERSION=latest -t metapus:latest -f Dockerfile .
```

### 2. Запуск стека

```bash
docker compose -f docker-compose.cloud.yml up -d
```

### 3. Инициализация мета-базы

```bash
# Создание схемы мета-базы (выполняется один раз)
docker compose -f docker-compose.cloud.yml exec metapus-router \
  /server migrate-meta  # или накатите db/meta/00001_tenants.sql вручную
```

### 4. Создание и привязка тенантов

```bash
# Создание тенанта
go run cmd/tenant/main.go create --slug acme --name "ACME Corp"
# Результат: Created tenant: 550e8400-...

# Миграция базы данных тенанта
go run cmd/tenant/main.go migrate --id 550e8400-...

# Назначение группы версий
go run cmd/tenant/main.go promote --id 550e8400-... --to v1.3.0

# Проверка
go run cmd/tenant/main.go list
# TENANT_ID   SLUG   NAME        DATABASE     SCHEMA  VERSION_GRP  STATUS
# 550e8400... acme   ACME Corp   mt_acme      20      v1.3.0       active
```

### 5. Проверка маршрутизации

```bash
# Этот запрос будет направлен на экземпляр metapus-v1_3_0
curl -H "X-Tenant-ID: 550e8400-..." http://localhost/api/v1/system/version
# {"version":"v1.3.0","buildTime":"...","expectedSchemaVersion":20}
```

---

## Процесс обновления: Одиночный тенант

```bash
# Шаг 1: Сборка новой версии
docker build --build-arg VERSION=v1.4.0 -t metapus:v1.4.0 .

# Шаг 2: Добавление нового сервиса в docker-compose.cloud.yml
#   metapus-v1_4_0:
#     image: metapus:v1.4.0
#     environment:
#       VERSION_GROUP: "v1.4.0"

# Шаг 3: Добавление upstream в конфигурацию nginx
#   upstream metapus_v1.4.0 { server metapus-v1_4_0:8080; }
#   map: "v1.4.0" metapus_v1.4.0;

# Шаг 4: Развертывание
docker compose -f docker-compose.cloud.yml up -d metapus-v1_4_0
nginx -s reload

# Шаг 5: Миграция схемы тенанта
go run cmd/tenant/main.go migrate --id 550e8400-...

# Шаг 6: Переключение тенанта (атомарно — один запрос UPDATE)
go run cmd/tenant/main.go promote --id 550e8400-... --to v1.4.0

# Готово! Следующий запрос от этого тенанта пойдет на экземпляр v1.4.0.
```

---

## Откат (Rollback)

```bash
# Возврат тенанта на предыдущую группу версий (схема БД должна быть обратно совместимой)
go run cmd/tenant/main.go promote --id 550e8400-... --to v1.3.0
```

---

## Массовое обновление (Canary)

```bash
# 1. Обновление одного тенанта (canary/канареечный релиз)
go run cmd/tenant/main.go promote --id <canary-uuid> --to v1.4.0

# 2. Мониторинг в течение 24 часов (проверка логов, частоты ошибок)

# 3. Обновление 10% тенантов
for id in $BATCH_10_PERCENT; do
  go run cmd/tenant/main.go promote --id $id --to v1.4.0
done

# 4. Обновление оставшихся тенантов
for id in $ALL_REMAINING; do
  go run cmd/tenant/main.go promote --id $id --to v1.4.0
done

# 5. Вывод из эксплуатации (остановка) старой версии
docker compose -f docker-compose.cloud.yml stop metapus-v1_3_0
```

---

## Переменные окружения

| Переменная | Обязательно | Описание |
|----------|----------|-------------|
| `META_DATABASE_URL` | Да | Подключение к мета-базе данных metapus_meta |
| `TENANT_DB_USER` | Да | Имя пользователя для баз данных тенантов |
| `TENANT_DB_PASSWORD` | Да | Пароль для баз данных тенантов |
| `VERSION_GROUP` | Нет | Фильтр: инстанс обслуживает только тенантов в этой группе |
| `APP_PORT` | Нет | Порт сервера (по умолчанию: 8080) |

---

## Вопросы безопасности

- Эндпоинт `/internal/route` помечен как `internal` в Nginx — он недоступен извне.
- Экземпляр router (маршрутизатора) НЕ должен быть доступен публично.
- Каждый экземпляр сервера имеет свой «Шлюз версий» (Version Gate) — даже если Nginx удастся обойти, сервер отклонит запросы к чужим версиям с кодом `421`.
- Рабочие процессы (workers) также фильтруют задачи по `VERSION_GROUP` — фоновые процессы не пересекаются между версиями.

---

## Связанные документы

- [07-multi-tenancy.md](07-multi-tenancy.md) — Архитектура Database-per-Tenant
- [17-migration-status.md](17-migration-status.md) — Инструмент управления миграциями (CLI)
- [21-extension-api.md](21-extension-api.md) — Система расширений
