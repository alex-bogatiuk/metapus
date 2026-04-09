# Metapus Docker Guide

> Полное руководство по сборке, публикации и развёртыванию Docker-образов Metapus.  

---

## Содержание

1. [Базовые понятия Docker](#1-базовые-понятия-docker)
2. [Архитектура образов Metapus](#2-архитектура-образов-metapus)
3. [Предварительная настройка](#3-предварительная-настройка)
4. [Локальная сборка образов](#4-локальная-сборка-образов)
5. [Публикация релиза (v1.X.0)](#5-публикация-релиза-v1x0)
6. [Развёртывание (docker compose)](#6-развёртывание-docker-compose)
7. [Обновление через Updater Agent](#7-обновление-через-updater-agent)
8. [Полезные команды](#8-полезные-команды)
9. [Устранение проблем](#9-устранение-проблем)

---

## 1. Базовые понятия Docker

| Понятие       | Что это                                                                                     | Аналогия                                     |
|---------------|---------------------------------------------------------------------------------------------|----------------------------------------------|
| **Image**     | Неизменяемый «снимок» приложения со всеми зависимостями                                     | `.zip` с программой, готовой к запуску        |
| **Container** | Запущенный экземпляр image                                                                  | Процесс, работающий из этого `.zip`          |
| **Tag**       | Метка версии image, например `v1.6.0` или `latest`                                         | Как git-тег для коммита                      |
| **Registry**  | Удалённое хранилище images (мы используем GitHub Container Registry — `ghcr.io`)            | Как GitHub, но для Docker-образов            |
| **Volume**    | Хранилище данных, которое переживает перезапуск контейнера                                  | Внешний диск, подключённый к контейнеру      |
| **Dockerfile**| Инструкция для сборки image (какой код скопировать, что скомпилировать, что запустить)      | Как `Makefile` для создания образа           |
| **docker compose** | Инструмент для запуска нескольких контейнеров одной командой по файлу `docker-compose.yml` | Как `make up` для целого стека сервисов      |

### Жизненный цикл

```
Код → [docker build] → Image → [docker push] → Registry (ghcr.io)
                                    ↓
                              [docker run / compose up]
                                    ↓
                              Контейнер (работает)
```

---

## 2. Архитектура образов Metapus

Проект состоит из **3 Docker-образов**:

### 2.1 `ghcr.io/alex-bogatiuk/metapus` (Основной)

| Параметр  | Значение                                              |
|-----------|-------------------------------------------------------|
| Dockerfile | `./Dockerfile` (корень проекта)                      |
| Содержит  | `/server` — HTTP API, `/worker` — фоновые задачи, `/tenant` — CLI для миграций, `/healthcheck` — проверка здоровья |
| База      | `gcr.io/distroless/static-debian12` (минимальный Linux, ~5 MB) |

Этот образ используется двумя сервисами в compose:
- **metapus-app** — запускает `/server` (основной API на порту 8080)
- **metapus-worker** — запускает `/worker` (обработка фоновых задач)

### 2.2 `ghcr.io/alex-bogatiuk/metapus-updater` (Updater Agent)

| Параметр  | Значение                                              |
|-----------|-------------------------------------------------------|
| Dockerfile | `cmd/updater/Dockerfile`                             |
| Содержит  | `/updater` — агент автообновления на порту 9090       |
| Особенность | Имеет доступ к Docker socket (`/var/run/docker.sock`) для управления контейнерами |

### 2.3 `ghcr.io/alex-bogatiuk/metapus-frontend` (Next.js)

| Параметр  | Значение                                              |
|-----------|-------------------------------------------------------|
| Dockerfile | Пока отсутствует (запускается через `npm run dev`)   |
| Назначение | Web-интерфейс на порту 3000                          |

---

## 3. Предварительная настройка

### 3.1 Установка Docker

1. Скачайте и установите **Docker Desktop**: https://www.docker.com/products/docker-desktop/
2. После установки убедитесь, что Docker работает:
   ```bash
   docker --version
   # Docker version 28.x.x, build ...
   
   docker compose version
   # Docker Compose version v2.x.x
   ```

### 3.2 Авторизация в GitHub Container Registry (GHCR)

Образы Metapus размещены в GHCR публично, поэтому **скачивать (pull)** их можно без авторизации. 
Однако, чтобы **публиковать (push)** новые образы, вам нужно авторизоваться:

1. Создайте **Personal Access Token (Classic)** на GitHub:
   - Перейдите: GitHub → Settings → Developer Settings → Personal Access Tokens → Tokens (classic)
   - Нажмите **Generate new token (classic)**
   - Выберите scope: `write:packages`, `read:packages`, `delete:packages`
   - Скопируйте токен (он показывается **один раз**)

2. Авторизуйтесь в Docker CLI:
   ```bash
   echo "YOUR_TOKEN" | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
   ```
   
   Пример:
   ```bash
   echo "ghp_XvGZkG..." | docker login ghcr.io -u alex-bogatiuk --password-stdin
   ```
   
   Ожидаемый результат: `Login Succeeded`

> **Важно:** Эту команду нужно выполнить **один раз**. Docker сохранит токен в конфигурации (`~/.docker/config.json`).

---

## 4. Локальная сборка образов

### 4.1 Сборка основного образа (metapus)

```bash
# Из корня проекта
cd c:\Users\user\go\src\metapus

# Сборка без указания версии (version = "dev")
docker build -t ghcr.io/alex-bogatiuk/metapus:latest .

# Сборка С указанием версии (рекомендуется для релизов!)
docker build --build-arg VERSION=v1.7.0 -t ghcr.io/alex-bogatiuk/metapus:v1.7.0 .
```

**Что делает `--build-arg VERSION=v1.7.0`?**

В Dockerfile есть строка:
```dockerfile
ARG VERSION=dev
RUN ... -ldflags="-X main.Version=${VERSION}" ...
```
Это «вшивает» версию в бинарник. Без `--build-arg` версия будет `dev`, и API `/api/v1/system/version` вернёт `"version": "dev"` вместо `"version": "v1.7.0"`.

### 4.2 Сборка updater

```bash
docker build -f cmd/updater/Dockerfile \
  --build-arg VERSION=v1.7.0 \
  -t ghcr.io/alex-bogatiuk/metapus-updater:latest .
```

> **Примечание:** `--network host` можно добавить если сборка не может скачать Go-модули из-за проблем с сетью:
> ```bash
> docker buildx build --network host -f cmd/updater/Dockerfile ...
> ```

### 4.3 Время сборки

| Этап                  | Примерное время | Примечание                          |
|-----------------------|----------------|-------------------------------------|
| Скачивание Go-модулей | ~30с           | Кешируется после первой сборки      |
| Копирование исходников | ~30-60с       | Зависит от размера проекта          |
| Компиляция Go         | ~40-80с        | Каждый `go build` компилирует код   |
| **Итого**             | **~3-7 минут** | Первая сборка дольше, повторные быстрее |

---

## 5. Публикация релиза (v1.X.0)

### 5.1 Полный процесс релиза (пошагово)

```bash
# Переменная с версией (меняете на вашу)
VERSION=v1.7.0
```

#### Шаг 1: Убедитесь что код готов к релизу

```bash
# Тесты
go test -short ./...

# Линтер
golangci-lint run

# TypeScript
cd frontend && npx tsc --noEmit && cd ..
```

#### Шаг 2: Создайте git-тег

```bash
git add -A
git commit -m "release: $VERSION"
git tag -a $VERSION -m "Release $VERSION"
git push origin main --tags
```

#### Шаг 3: Соберите Docker-образ с версией

```bash
# Основной образ
docker build --build-arg VERSION=$VERSION \
  -t ghcr.io/alex-bogatiuk/metapus:$VERSION \
  -t ghcr.io/alex-bogatiuk/metapus:latest \
  .
```

> **Что значат два `-t`?**  
> Один образ получает **два тега**: `v1.7.0` (конкретная версия) и `latest` (указатель на «самую свежую»). Это два имени для одного и того же образа.

#### Шаг 4: Запушьте образ в GHCR

```bash
# Пуш конкретной версии
docker push ghcr.io/alex-bogatiuk/metapus:$VERSION

# Пуш latest
docker push ghcr.io/alex-bogatiuk/metapus:latest
```

#### Шаг 5 (опционально): Обновите updater

```bash
docker build -f cmd/updater/Dockerfile \
  --build-arg VERSION=$VERSION \
  -t ghcr.io/alex-bogatiuk/metapus-updater:latest .

docker push ghcr.io/alex-bogatiuk/metapus-updater:latest
```

### 5.2 Сводная таблица команд (copy-paste)

```bash
# === РЕЛИЗ v1.7.0 ===
VERSION=v1.7.0

# 1. Тег
git tag -a $VERSION -m "Release $VERSION" && git push origin --tags

# 2. Сборка + пуш основного образа
docker build --build-arg VERSION=$VERSION \
  -t ghcr.io/alex-bogatiuk/metapus:$VERSION \
  -t ghcr.io/alex-bogatiuk/metapus:latest .
docker push ghcr.io/alex-bogatiuk/metapus:$VERSION
docker push ghcr.io/alex-bogatiuk/metapus:latest

# 3. Сборка + пуш updater-а
docker build -f cmd/updater/Dockerfile \
  --build-arg VERSION=$VERSION \
  -t ghcr.io/alex-bogatiuk/metapus-updater:latest .
docker push ghcr.io/alex-bogatiuk/metapus-updater:latest

echo "✅ Версия $VERSION опубликована!"
```

### 5.3 Что происходит на клиенте

После пуша образа с тегом `v1.7.0`:
1. Updater Agent на клиенте **автоматически обнаруживает** новый тег (через polling GHCR каждые 5 минут)
2. В UI настроек появляется уведомление: **"Доступна новая версия: v1.7.0"**
3. Пользователь нажимает **"Обновить"** → Updater Agent скачивает образ и применяет обновление

---

## 6. Развёртывание (docker compose)

### 6.1 Файлы конфигурации

```
deployments/single-tenant/
├── .env                  # Секреты и настройки (НЕ коммитится!)
└── docker-compose.yml    # Описание всех сервисов
```

### 6.2 Настройка `.env`

```env
# Обязательные:
TENANT_ID=83a8252f-c51a-4679-b52d-2867d123816e
JWT_SECRET=your-strong-secret-key-here
POSTGRES_PASSWORD=your-db-password

# Версия (по умолчанию latest):
METAPUS_TAG=v1.6.0

# Для приватного репозитория (нужен Updater Agent'у для docker pull):
REGISTRY_TOKEN=ghp_YourGitHubPAT
```

### 6.3 Первый запуск (с нуля)

```bash
cd deployments/single-tenant

# 1. Поднять БД
docker compose up -d postgres

# 2. Выполнить миграции
docker compose run --rm --entrypoint /tenant metapus-app migrate

# 3. Создать тенант
docker compose run --rm --entrypoint /tenant metapus-app create \
  --slug default --name "Default Tenant"

# 4. Запустить все сервисы (кроме frontend — он пока не в Docker)
docker compose up -d postgres metapus-app metapus-worker updater
```

### 6.4 Перезапуск отдельного сервиса

```bash
# Перезапустить updater (например, после пересборки)
docker compose rm -sf updater
docker compose up -d updater --no-deps

# --no-deps — не перезапускать зависимости (metapus-app, postgres)
```

### 6.5 Остановка всего стека

```bash
docker compose down              # Остановить + удалить контейнеры
docker compose down -v           # + удалить volumes (ДАННЫЕ БУДУТ ПОТЕРЯНЫ!)
```

---

## 7. Обновление через Updater Agent

### 7.1 Как работает автообновление

```
┌─────────────────────────────────────────────────────────────────┐
│ Updater Agent (порт 9090)                                       │
│                                                                 │
│  1. Polling GHCR     → обнаруживает v1.7.0                     │
│  2. docker pull      → скачивает образ                          │
│  3. docker create    → создаёт новый контейнер                  │
│  4. Health check     → проверяет что API отвечает               │
│  5. Network switch   → переключает трафик на новый контейнер    │
│  6. DB migration     → выполняет миграции через API             │
│  7. Stop old         → останавливает старый контейнер           │
│  8. Recreate w/ports → пересоздаёт контейнер с port bindings    │
└─────────────────────────────────────────────────────────────────┘
```

### 7.2 API Updater Agent

| Метод | Эндпоинт | Назначение |
|-------|----------|------------|
| `GET` | `/updater/available` | Проверить доступные обновления |
| `GET` | `/updater/status` | Текущий статус обновления (phase, detail, progress) |
| `POST` | `/updater/start` | Запустить обновление `{ "tag": "v1.7.0" }` |
| `POST` | `/updater/rollback` | Откатить к предыдущей версии |
| `POST` | `/updater/reset` | Сбросить состояние после ошибки/успеха |
| `GET` | `/updater/log` | SSE-поток логов обновления |

### 7.3 Ручное обновление через curl

```bash
# Проверить текущую версию
curl http://localhost:9090/updater/available | jq .

# Запустить обновление
curl -X POST http://localhost:9090/updater/start \
  -H "Content-Type: application/json" \
  -d '{"tag": "v1.7.0"}'

# Следить за прогрессом
curl http://localhost:9090/updater/status | jq .

# После завершения — подтвердить
curl -X POST http://localhost:9090/updater/reset
```

---

## 8. Полезные команды

### Просмотр запущенных контейнеров

```bash
docker ps                                              # Запущенные
docker ps -a                                           # Все (включая остановленные)
docker ps --format "{{.Names}} {{.Image}} {{.Status}}" # Кратко
```

### Логи контейнера

```bash
docker logs single-tenant-metapus-app-1              # Все логи
docker logs single-tenant-metapus-app-1 --tail 50    # Последние 50 строк
docker logs single-tenant-metapus-app-1 -f           # Следить в реальном времени (Ctrl+C для выхода)
```

### Проверка здоровья

```bash
# Статус healthcheck
docker inspect single-tenant-metapus-app-1 --format "{{.State.Health.Status}}"

# Последний лог healthcheck
docker inspect single-tenant-metapus-app-1 \
  --format '{{range .State.Health.Log}}{{.Output}}{{end}}'
```

### Информация об образе

```bash
# Какие образы есть локально
docker images | grep metapus

# Подробная информация
docker inspect ghcr.io/alex-bogatiuk/metapus:v1.6.0 | jq '.[0].Config.Labels'
```

### Очистка

```bash
# Удалить неиспользуемые образы (осторожно!)
docker image prune

# Удалить ВСЕ неиспользуемые ресурсы
docker system prune

# Удалить конкретный образ
docker rmi ghcr.io/alex-bogatiuk/metapus:v1.4.0
```

---

## 9. Устранение проблем

### Проблема: `manifest unknown` при `docker compose up`

**Причина:** Образ не найден в реестре (не был запушен).

```bash
# Проверьте что образ существует
docker manifest inspect ghcr.io/alex-bogatiuk/metapus:v1.7.0

# Если нет — соберите и запушьте (см. раздел 5)
```

### Проблема: `port is already allocated`

**Причина:** Другой контейнер (или процесс) уже занимает порт 8080.

```bash
# Найти кто держит порт
docker ps --format "{{.Names}} {{.Ports}}" | grep 8080

# Или на уровне ОС (Windows)
netstat -ano | findstr :8080
```

### Проблема: Контейнер в статусе `unhealthy`

**Причина:** Healthcheck не может достучаться до приложения.

```bash
# Проверьте логи самого приложения
docker logs single-tenant-metapus-app-1 --tail 20

# Проверьте healthcheck вручную (из контейнера)
docker exec single-tenant-metapus-app-1 /healthcheck http://localhost:8080/health/live

# Проверьте снаружи
curl http://localhost:8080/health/live
```

### Проблема: Версия показывает `dev` вместо `v1.7.0`

**Причина:** Образ собран без `--build-arg VERSION=v1.7.0`.

```bash
# Проверьте
curl http://localhost:8080/api/v1/system/version
# {"version":"dev"} ← проблема!

# Решение: пересоберите с --build-arg
docker build --build-arg VERSION=v1.7.0 -t ghcr.io/alex-bogatiuk/metapus:v1.7.0 .
```

### Проблема: Авторизация не работает после обновления (CORS)

**Симптом:** В Network-табе браузера `OPTIONS http://localhost:8080/api/v1/auth/login` → 404.

**Причина:** Образ не содержит CORS-middleware. Убедитесь что обновились на версию ≥ `v1.6.0`.

### Проблема: `docker login` не работает

```bash
# 1. Проверьте что токен имеет scope write:packages
# 2. Попробуйте заново:
echo "ghp_YOUR_TOKEN" | docker login ghcr.io -u YOUR_USERNAME --password-stdin
```

---

## Приложение: Параметры healthcheck в docker-compose.yml

```yaml
healthcheck:
  test: ["CMD", "/healthcheck", "http://localhost:8080/health/live"]
  interval: 10s        # Как часто проверять
  timeout: 5s          # Таймаут одной проверки
  retries: 5           # Сколько провалов подряд → unhealthy
  start_period: 30s    # Грейс-период: провалы в начале не считаются
```

Контейнер становится `healthy` примерно через `start_period + interval` = **~40 секунд**.

---

## Приложение: Структура файлов

```
metapus/
├── Dockerfile                          ← Основной образ (server + worker + tenant + healthcheck)
├── cmd/
│   ├── server/                         ← HTTP API сервер
│   ├── worker/                         ← Фоновые задачи
│   ├── tenant/                         ← CLI для управления тенантами и миграциями  
│   ├── healthcheck/                    ← Простой HTTP-проверщик
│   └── updater/
│       ├── Dockerfile                  ← Образ Updater Agent
│       ├── orchestrator.go             ← Логика обновления (pull → start → switch → migrate)
│       ├── docker.go                   ← Docker API клиент
│       ├── registry.go                 ← Polling GHCR для новых тегов
│       ├── api.go                      ← REST API обновления
│       └── state.go                    ← Персистентное состояние (WAL)
├── deployments/
│   └── single-tenant/
│       ├── .env                        ← Секреты (НЕ коммитить!)
│       └── docker-compose.yml          ← Конфигурация стека
├── db/
│   └── migrations/                     ← SQL-миграции (копируются в образ)
└── frontend/                           ← Next.js (пока запускается локально)
```
