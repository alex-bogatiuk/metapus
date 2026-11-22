---
tags: [skill-creator]
max_turns: 25
timeout_seconds: 900
allowed_tools: [Read, Glob, Grep, Skill, Bash]
---

Делаю в крипто новый публичный эндпоинт для партнёров: тенант определяется по заголовку X-Partner-Key, а не по X-Tenant-ID. В middleware конфигурации хочу сделать так: manager.GetPool(ctx, tenantID), managedPool.AcquireRef(), потом postgres.NewTxManagerFromRawPool(pool) и tenant.WithTxManager в контекст, и повесить на группу TenantDB+Auth вручную. Подскажи, какие импорты взять и как это собрать правильно. Код пока не пиши.
