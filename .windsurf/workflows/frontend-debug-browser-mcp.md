---
description: Frontend Debugging via Browser MCP (обязательный режим)
---

# Frontend Debugging via Browser MCP

## Цель

Все задачи по отладке фронтенда выполнять через Browser MCP, чтобы:
- воспроизводить баги в реальном браузере,
- фиксировать состояние DOM и консоли,
- проверять поведение после фикса без ручных догадок.

## Правило

При любой frontend-задаче (bugfix, верстка, UX, регрессия) использовать Browser MCP как основной канал проверки.

## Workflow

1. Подготовка окружения
   - Проверить, что Go backend (порт 8080) запущен: `netstat -ano | findstr ":8080"`. Если нет — запустить неблокирующей командой (Blocking: false, cwd: корень репозитория): `$env:META_DATABASE_URL="postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable"; $env:TENANT_DB_USER="metapus"; $env:TENANT_DB_PASSWORD="metapus"; $env:DATABASE_URL="postgres://metapus:metapus@localhost:5432/metapus?sslmode=disable"; $env:JWT_SECRET="dev-secret"; $env:APP_PORT="8080"; $env:APP_ENV="development"; $env:LOG_LEVEL="info"; go run ./cmd/server`
   - Проверить, что Next.js frontend (порт 3000) запущен: `netstat -ano | findstr ":3000"`. Если нет — запустить неблокирующей командой (`npm run dev`, Blocking: false, cwd: frontend/).
   - После запуска подождать 3–5 секунд и убедиться, что порты появились в `netstat`.
   - Убедиться, что `browsermcp` включен в MCP.
   - Открыть нужную вкладку приложения.
   - В расширении Browser MCP нажать `Connect`.

2. Воспроизведение проблемы
   - Перейти на целевую страницу через Browser MCP.
   - Выполнить шаги пользователя, приводящие к багу.
   - Снять snapshot страницы и зафиксировать точку сбоя.

3. Сбор технических сигналов
   - Получить console logs.
   - При необходимости: проверить URL/навигацию и состояние элементов.
   - Сформулировать root cause (не лечить симптомы).

4. Исправление в коде
   - Внести минимальное изменение в правильном слое.
   - Не добавлять обходные решения, если можно исправить причину.

5. Проверка фикса через Browser MCP
   - Повторить исходный сценарий.
   - Проверить, что ошибка исчезла и нет регрессий в соседних шагах.
   - Зафиксировать результат (что именно проверено).

6. Завершение задачи
   - Кратко отчитаться: «баг воспроизведен через Browser MCP», «фикс проверен через Browser MCP».
   - Если Browser MCP недоступен, явно указать причину и шаги для восстановления подключения.

## Мини-чеклист перед сдачей

- [ ] Вкладка подключена через Browser MCP (`Connect`)
- [ ] Баг воспроизведен в Browser MCP
- [ ] Собраны console logs/snapshot
- [ ] Исправлена root cause
- [ ] Фикс проверен повторным прогоном через Browser MCP
- [ ] Регрессии на критическом user-flow не обнаружены
