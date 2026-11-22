# CHANGELOG публичного API (`pkg/sdk`)

Каждое изменение `pkg/sdk` записывается здесь вместе с новой `sdk.APIVersion` ([правила](README.md#версионирование)).

## 3.0.0 — 2026-11-22

Первая версия фасада. Прежние `pkg/extension.ExtensionAPIVersion` и `internal/platform.ExtensionAPIVersion` (2.0.0) содержали только номер и нигде не проверялись; они удалены.

- `pkg/sdk`: `APIVersion`, `Compatible`.
- `entity`, `id`, `types`, `apperror`: модель справочников, документов и движений регистров, идентификаторы, `Quantity`, `MinorUnits`, ошибки.
- `registry`: `Catalog[T]`, `Document[T]`, `Registry`, `Presentation`, `MovementSourceProvider`.
- `posting`: `Module`, `Engine`, `Postable`, `MovementSet`, `RegisterVisitor`, `RegisterRecorder`, `PostingValidator`, `NewEngine` (для тестов).
- `registers`: `BaseAccumulationRepo`, `NewBaseAccumulationRepo`.
- `configuration`: `Configuration` (с новым методом `APIVersion`), `Server`, `Worker`, `ServerEnv`, `WorkerEnv`, `SeedEnv`, `TenantEnv`, `TenantWorker`, `OutboxRoute`, `Component`, `Cleanup`, `EmbeddedMigrations`.
- `integrations`: `Catalog`, `PoolConsumer`. `contracts`: `Bundle`.
- `sdktest`: `Check`, `NewTenant` (тег `integration`).
