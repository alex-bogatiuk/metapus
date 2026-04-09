# Metapus: Развёртывание Single Tenant

> Пошаговая инструкция по установке Metapus ERP для одной компании на вашем сервере.

---

## Системные требования

| Параметр | Минимум | Рекомендуется |
|----------|---------|---------------|
| **ОС** | Linux (Ubuntu 22.04+, Debian 12+) или Windows Server 2022+ | Ubuntu 24.04 LTS |
| **CPU** | 2 ядра | 4 ядра |
| **RAM** | 2 GB | 4 GB |
| **Диск** | 10 GB SSD | 20 GB SSD |
| **Docker** | Docker Engine 24+ | Docker Engine 27+ |
| **Сеть** | Доступ к `ghcr.io` для скачивания образов | — |

---

## Шаг 1. Установка Docker

### Linux (Ubuntu/Debian)

```bash
# Установка Docker
curl -fsSL https://get.docker.com | sudo sh

# Добавить текущего пользователя в группу docker (чтобы не использовать sudo)
sudo usermod -aG docker $USER

# Перелогиньтесь (или выполните: newgrp docker)

# Проверка
docker --version
docker compose version
```

### Windows

1. Скачайте и установите [Docker Desktop](https://www.docker.com/products/docker-desktop/)
2. Запустите Docker Desktop
3. Откройте PowerShell или терминал и проверьте:
   ```powershell
   docker --version
   docker compose version
   ```



## Шаг 3. Скачивание файлов развёртывания

Создайте папку для Metapus и скопируйте файлы конфигурации:

```bash
mkdir -p ~/metapus && cd ~/metapus
```

### 3.1. Файл `docker-compose.yml`

Создайте файл `docker-compose.yml` со следующим содержимым:

```yaml
# Metapus Single-Tenant Deployment
services:
  # --- База данных ---
  postgres:
    image: postgres:17-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: metapus
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-metapus}
      POSTGRES_DB: metapus
    volumes:
      - pgdata:/var/lib/postgresql/data
    networks:
      - metapus-net
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U metapus"]
      interval: 5s
      timeout: 5s
      retries: 5

  # --- Сервер Metapus ---
  metapus-app:
    image: ${METAPUS_IMAGE:-ghcr.io/alex-bogatiuk/metapus}:${METAPUS_TAG:-latest}
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      APP_PORT: "8080"
      APP_ENV: "production"
      LOG_LEVEL: "info"
      JWT_SECRET: ${JWT_SECRET:?JWT_SECRET is required}
      META_DATABASE_URL: postgres://metapus:${POSTGRES_PASSWORD:-metapus}@postgres:5432/tenants?sslmode=disable
      DATABASE_URL: postgres://metapus:${POSTGRES_PASSWORD:-metapus}@postgres:5432/metapus?sslmode=disable
      TENANT_DB_USER: metapus
      TENANT_DB_PASSWORD: ${POSTGRES_PASSWORD:-metapus}
      TENANT_DB_HOST: postgres
    ports:
      - "${APP_PORT:-8080}:8080"
    networks:
      metapus-net:
        aliases:
          - metapus-app
    healthcheck:
      test: ["CMD", "./healthcheck", "http://localhost:8080/health/live"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 30s

  # --- Фоновые задачи ---
  metapus-worker:
    image: ${METAPUS_IMAGE:-ghcr.io/alex-bogatiuk/metapus}:${METAPUS_TAG:-latest}
    restart: unless-stopped
    entrypoint: ["./worker"]
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      META_DATABASE_URL: postgres://metapus:${POSTGRES_PASSWORD:-metapus}@postgres:5432/tenants?sslmode=disable
      TENANT_DB_USER: metapus
      TENANT_DB_PASSWORD: ${POSTGRES_PASSWORD:-metapus}
      LOG_LEVEL: "info"
    networks:
      - metapus-net

  # --- Агент обновлений ---
  updater:
    image: ${UPDATER_IMAGE:-ghcr.io/alex-bogatiuk/metapus-updater}:${UPDATER_TAG:-latest}
    restart: unless-stopped
    depends_on:
      metapus-app:
        condition: service_healthy
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - updater-data:/data
    environment:
      UPDATER_PORT: "9090"
      SERVER_URL: http://metapus-app:8080
      TENANT_ID: ${TENANT_ID:?TENANT_ID is required}
      REGISTRY_IMAGE: ${METAPUS_IMAGE:-ghcr.io/alex-bogatiuk/metapus}
      DOCKER_NETWORK: ${COMPOSE_PROJECT_NAME:-metapus}_metapus-net
      CONTAINER_NAME: metapus-app
      STATE_FILE: /data/state.json
      HEALTH_TIMEOUT: "60s"
      DRAIN_TIMEOUT: "30s"
      LOG_LEVEL: "info"
    ports:
      - "${UPDATER_PORT:-9090}:9090"
    networks:
      - metapus-net

networks:
  metapus-net:
    driver: bridge

volumes:
  pgdata:
  updater-data:
```

### 3.2. Файл `.env`

Создайте файл `.env` рядом с `docker-compose.yml`:

```env
# === ОБЯЗАТЕЛЬНЫЕ ПАРАМЕТРЫ ===

# Уникальный идентификатор вашей компании (будет получен при регистрации).
TENANT_ID=ваш-uuid-тенанта

# Секретный ключ для авторизации (придумайте сложную строку, минимум 32 символа).
# ВАЖНО: Не меняйте после запуска!
JWT_SECRET=ваш-секретный-ключ-минимум-32-символа-!@#$

# Пароль базы данных PostgreSQL.
POSTGRES_PASSWORD=надёжный-пароль-для-бд

# Версия Metapus для установки (уточните у поставщика).
METAPUS_TAG=v1.6.0
```

---

## Шаг 4. Первый запуск

Выполните команды **последовательно**:

```bash
cd ~/metapus

# 4.1. Запустить базу данных
docker compose up -d postgres

# Подождите 10 секунд, пока БД инициализируется
sleep 10

# 4.2. Создать структуру мета-базы данных
docker compose run --rm --entrypoint ./tenant metapus-app init-meta

# 4.3. Создать структуру тенанта
docker compose run --rm --entrypoint ./tenant metapus-app create \
  --slug default --name "Название вашей компании"
```

> **Для пользователей Windows:**
> Благодаря использованию относительного пути `./tenant` (вместо `/tenant`), пути в Git Bash не искажаются.

> **Важно:** Команда `create` выведет UUID тенанта. Если он отличается от указанного
> в `.env`, обновите значение `TENANT_ID` в файле `.env`.

```bash
# 4.4. Запустить все сервисы
docker compose up -d
```

---

## Шаг 5. Проверка работоспособности

### 5.1. Проверить что все контейнеры запущены

```bash
docker compose ps
```

Ожидаемый результат:

```
NAME                    IMAGE                                    STATUS
metapus-postgres-1      postgres:17-alpine                       Up (healthy)
metapus-metapus-app-1   ghcr.io/alex-bogatiuk/metapus:v1.6.0    Up (healthy)
metapus-metapus-worker-1 ghcr.io/alex-bogatiuk/metapus:v1.6.0   Up
metapus-updater-1       ghcr.io/alex-bogatiuk/metapus-updater    Up
```

### 5.2. Проверить API

```bash
curl http://localhost:8080/health/live
# Ожидаемый ответ: {"status":"ok"}

curl http://localhost:8080/api/v1/system/version
# Ожидаемый ответ: {"version":"v1.6.0", ...}
```

### 5.3. Открыть веб-интерфейс

Откройте в браузере: **http://ваш-сервер:3000**

Учётные данные по умолчанию:

| Поле | Значение |
|------|----------|
| **Логин** | `admin` |
| **Пароль** | `Admin123!` |

> ⚠️ **Обязательно смените пароль администратора** после первого входа!

---

## Шаг 6. Обновление системы

Metapus обновляется автоматически через встроенный агент обновлений.

### Через веб-интерфейс

1. Перейдите в **Настройки** (шестерёнка в меню)
2. Найдите блок **«Обновление системы»**
3. Если доступна новая версия — появится уведомление с кнопкой **«Обновить»**
4. Нажмите **«Обновить»** и подтвердите действие
5. Наблюдайте за прогрессом:
   - ⬇️ Скачивание нового образа (с прогресс-баром)
   - 🖥️ Запуск нового контейнера
   - ❤️ Проверка здоровья
   - 🔀 Переключение трафика
   - 🗄️ Миграция базы данных (с таймером)
   - ✅ Готово!
6. Проверьте работоспособность и нажмите **«Подтвердить»**

### Через командную строку

```bash
# Проверить доступные обновления
curl http://localhost:9090/updater/available

# Запустить обновление до конкретной версии
curl -X POST http://localhost:9090/updater/start \
  -H "Content-Type: application/json" \
  -d '{"tag": "v1.7.0"}'

# Следить за прогрессом
curl http://localhost:9090/updater/status

# Подтвердить обновление (после проверки)
curl -X POST http://localhost:9090/updater/reset

# Откатить обновление (при проблемах)
curl -X POST http://localhost:9090/updater/rollback
```

---

## Управление системой

### Запуск / Остановка

```bash
cd ~/metapus

# Запустить все сервисы
docker compose up -d

# Остановить все сервисы (данные сохраняются)
docker compose down

# Перезапустить конкретный сервис
docker compose restart metapus-app
```

### Просмотр логов

```bash
# Логи всех сервисов
docker compose logs

# Логи конкретного сервиса (последние 100 строк)
docker compose logs metapus-app --tail 100

# Следить за логами в реальном времени
docker compose logs metapus-app -f
```

### Резервное копирование

```bash
# Создать бэкап базы данных
docker compose exec postgres pg_dumpall -U metapus > backup_$(date +%Y%m%d).sql

# Восстановить из бэкапа
cat backup_20260402.sql | docker compose exec -T postgres psql -U metapus
```

---

## Переменные окружения

| Переменная | Обязательна | По умолчанию | Описание |
|------------|:-----------:|:------------:|----------|
| `TENANT_ID` | ✅ | — | UUID вашей компании |
| `JWT_SECRET` | ✅ | — | Секрет для токенов авторизации (мин. 32 символа) |
| `POSTGRES_PASSWORD` | — | `metapus` | Пароль PostgreSQL |
| `METAPUS_TAG` | — | `latest` | Версия Metapus (`v1.6.0`, `v1.7.0`, ...) |
| `APP_PORT` | — | `8080` | Порт API сервера |
| `UPDATER_PORT` | — | `9090` | Порт агента обновлений |

---

## Порты

| Порт | Сервис | Назначение |
|------|--------|------------|
| **8080** | Metapus API | REST API для фронтенда и интеграций |
| **9090** | Updater Agent | API агента обновлений |
| **5432** | PostgreSQL | База данных (по умолчанию не экспонируется наружу) |

> Порт PostgreSQL (5432) **не публикуется** наружу по умолчанию.
> Если нужен внешний доступ к БД, добавьте в сервис `postgres` в compose:
> ```yaml
> ports:
>   - "5432:5432"
> ```

---

## Устранение неполадок

### Контейнер не запускается

```bash
# Проверить логи
docker compose logs metapus-app --tail 50

# Частые причины:
# - Неправильный POSTGRES_PASSWORD
# - База данных не инициализирована (не выполнен шаг 4.2)
# - Порт 8080 уже занят другим приложением
```

### Ошибка авторизации в веб-интерфейсе

1. Убедитесь что `TENANT_ID` в `.env` совпадает с UUID тенанта в базе
2. Проверьте что `JWT_SECRET` не менялся после создания пользователей
3. Очистите cookies/cache в браузере

### Обновление зависло

1. Проверьте прогресс: `curl http://localhost:9090/updater/status`
2. Если фаза `migrating` — дождитесь завершения (миграции могут занять до 5 минут)
3. Если статус `failed` — проверьте ошибку и выполните откат:
   ```bash
   curl -X POST http://localhost:9090/updater/rollback
   ```

### Нет доступа к обновлениям

```bash
# Проверить что образ вообще доступен в публичном реестре
docker pull ghcr.io/alex-bogatiuk/metapus:v1.6.0
```

---

## Контакты поддержки

При возникновении проблем обращайтесь к вашему поставщику Metapus.
Для ускорения диагностики приложите к обращению:

1. Вывод команды `docker compose ps`
2. Логи проблемного сервиса: `docker compose logs metapus-app --tail 200`
3. Версию системы: `curl http://localhost:8080/api/v1/system/version`
