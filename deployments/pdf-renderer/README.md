# Внутренний PDF renderer

Основной `server` остаётся distroless. `cmd/pdf-renderer` поставляется отдельным
Linux-образом с Chromium и шрифтами DejaVu/Noto. Domain знает только порт
`printing.PDFRenderer`; HTTP-адаптер находится в `infrastructure/pdfrender`.

Обе single-tenant Compose-конфигурации подключают renderer через сеть
`pdf-internal` (`internal: true`), без опубликованного порта. Приложение получает
`PDF_RENDERER_URL=http://pdf-renderer:8081`. Отсутствующая настройка или отказ
renderer даёт `503 PDF_RENDERER_UNAVAILABLE` только для PDF. HTML и DOCX остаются
доступны. Проверка capability приложения: `GET /health/capabilities/pdf`.

Внутренний `POST /render` принимает `{ "html": "...", "options": { "paperSize":
"A4", "landscape": false } }` и возвращает `application/pdf`. Допустимы A4, A5 и
Letter. Произвольные URL и неизвестные поля запроса отклоняются. HTML должен быть
самодостаточным: разрешены стили и встроенные PNG/JPEG; активная разметка,
обработчики событий, внешние изображения и навигация удаляются. CSP запрещает
скрипты, внешние ресурсы, frames и network connections, включая CSS-загрузки.

Ограничения: 4 MiB HTML, 16 MiB PDF, 30 секунд на рендер, два одновременных
запроса (лишние получают 503 до чтения body). Отмена HTTP-запроса завершает всю
группу процессов Chromium; временный профиль удаляется. Shutdown прекращает
readiness и ждёт запросы в пределах десяти секунд. Readiness renderer становится
успешной только после реального пробного рендера кириллицы при старте.

Контейнер работает как UID/GID 10001 с read-only root, ограниченным tmpfs,
`init: true`, лимитами памяти/PIDs и включённым Chromium sandbox. Отключение sandbox
и privileged-режим не предусмотрены. Хост должен поддерживать user namespaces.
Seccomp-профиль разрешает необходимые Chromium `clone`, `setns`, `unshare`.
Он взят из [официального Playwright](https://github.com/microsoft/playwright/blob/ae935a43d9e376e4759548f6b3c6905c7b282333/utils/docker/seccomp_profile.json)
(Apache-2.0, копия лицензии — `LICENSE.playwright`); рекомендации по nonroot и
профилю описаны в [документации Docker](https://playwright.dev/docs/docker).

Проверка из корня репозитория: `go run ./cmd/pdf-renderer-smoke` или `make test-pdf`.
Этот же runner выполняется в CI. Он строит тестовый target, производный от
поставляемого runtime, запускает его с теми же ограничениями и без сети, извлекает
из PDF кириллицу и точную сумму через Poppler, проверяет запрет script/URL,
очистку временных файлов/процессов и graceful shutdown. Poppler и smoke binary
присутствуют только в target `smoke`; финальный target `release` их не содержит.
Runner создаёт уникальные контейнер/тег и удаляет только свои ресурсы.
