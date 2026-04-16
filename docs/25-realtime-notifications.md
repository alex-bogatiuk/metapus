# 25. Системы Реального Времени и Уведомления (Real-Time Notifications)

В системе реализован полномасштабный механизм push-уведомлений реального времени с изоляцией по тенантам и интеграцией с подсистемой автоматизации (Automation Engine).

## 1. Архитектурный Обзор

Весь путь от наступления бизнес-события до отображения всплывающего уведомления в UI (счетчик и toast) состоит из нескольких слоев архитектуры:

1. **Trigger (Событие)**: CRUD операции или специфичные бизнес-действия (например, проведение документа) перехватываются `EventDispatcher` и помещаются в Message Broker (PostgreSQL queue / Redis).
2. **Background Worker (Automation Engine)**: Воркер достает события отложенно или параллельно, прогоняет через CEL-кондер (условия), и если сработало, исполняет CEL-шаблон тела (Payload).
3. **InternalNotificationAdapter**: Вспомогательный адаптер внутри Engine (при `ActionType = "internal_notification"`), который принимает сгенерированный Payload и массив пользователей, пробрасывая их в `NotificationService`.
4. **Notification Repository**: Выполняет Batch-Insert всех экземпляров уведомлений в БД `sys_notifications` с авто-вычислением изолированной среды тенанта.
5. **WebSocket Hub**: Синхронно (или через Redis/Go Channels) `GlobalHub` берет сохраненные в БД объекты и распушивает их только тем пользователям (в рамках тенанта), которые сейчас в онлайне. 
6. **Frontend Hook / Component**: `useWebsocket` слушает входящие данные, инвалидирует кэши и заставляет выскочить глобальный Toast (`sonner`), а счетчик колокольчика в `SiteHeader` обновляется без перезагрузки вкладки.

## 2. Backend Подсистема (Golang)

### 2.1 Изоляция по тенантам в WebSocket

Подсистема WebSocket находится в пакете `internal/infrastructure/websocket`. Главный объект — `GlobalHub`.

```go
type Hub struct {
    // tenantID -> userID -> Connections
    clients map[string]map[string]map[*Client]bool
    mu      sync.RWMutex
}
```

Все соединения жестко закреплены за комбинацией `tenant_id` + `user_id`, что гарантирует, что Broadcast-события доставляются строго в нужную SaaS-область:
- Функция `BroadcastToUser(ctx context.Context, userID string, message interface{})` достает `tenant_id` из переданного `ctx`, гарантируя безопасную рассылку.

### 2.2 Аутентификация 

Для работы по протоколам `ws://` и `wss://`, где нельзя отправить заголовок `Authorization: Bearer <token>` при Upgrade-запросе в браузере, используется Middleware `auth.go` с поддержкой извлечения JWT из параметров запроса:  
`/api/v1/ws?token=<token>`

### 2.3 Автоматизация и Internal Adapter

Уведомления редко генерируются "жестким кодом". Вместо этого используется подсистема `Automation Engine` (Automation Rules).

В доменной логике:
- У правил, чье действие направлено на внутренние системы (`internal_notification`), поле `ServiceAccountID` является **опциональным** (`*id.ID`).
- Шаблон Payload (пишется на CEL) генерирует JSON следующего формата:
```json
{
  "target_users": ["uuid1", "uuid2"],
  "title": "Документ Проведен",
  "message": "Счет на оплату № 35 успешно проведен",
  "link": "/document/invoice/uuid3"
}
```

Внутри Worker'а на этапе `Execute` вызывается `InternalNotificationAdapter`, который:
1. Маппит массив `target_users` и подготавливает пачку `domain/notifications.Notification`.
2. Сохраняет всю пачку в `sys_notifications` за 1 SQL запрос (`squirrel.Insert`).
3. Для каждого пользователя использует `hub.BroadcastToUser`, отправляя объект.

## 3. Frontend Подсистема (React/Next.js)

### 3.1 `useWebsocket.ts` Хук

Жизненный цикл соединения абстрагирован в хук:
- Авто-формирование `ws/wss` URL из `NEXT_PUBLIC_API_URL`.
- Автоматически получает `tokens` из глобального стора `useAuthStore` (zustand).
- **Exponential Backoff**: В случае обрыва интервала или падения сервера бэкенда, клиент пытается наладить соединение с экспоненциально увеличивающейся задержкой (1с, 2с, 4с... max 30с).

### 3.2 Компонент `NotificationBell`

Является главным агрегатором:
- В фоне подтягивает непрочитанные события раз в 60 секунд на случай отключения сети.
- Подписывается на возвращаемый `lastMessage` объект из `useWebsocket`.
- При получении `type === "notification"`, немедленно триггерит перезагрузку (fetch) локального состояния, обновляет счетчик Badge и заставляет библиотеку `sonner` показать `toast()`.
- Имплементирует методы API:
  - `list(unreadOnly: true)`
  - `markAsRead(id)`
  - `markAllAsRead()`

### 3.3 Структура API

API методы инкапсулированы в `frontend/lib/api.ts` в блоке `system.notifications`. Используется стандартная структура, включая работу с курсорной пагинацией (`limit`, `after`, `before`).

В UI соблюдается принцип Optimistic UI / мягкой инвалидации — после клика по уведомлению (чтение), вызывается `markAsRead` и локальный кеш сразу обновляется, убирая выделение непрочитанного элемента, без нужды обновлять всю страницу.
