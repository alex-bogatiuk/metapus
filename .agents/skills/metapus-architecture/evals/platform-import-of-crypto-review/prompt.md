---
tags: [skill-creator]
max_turns: 25
timeout_seconds: 900
allowed_tools: [Read, Glob, Grep, Skill, Bash]
---

Хочу запретить удаление пула интеграций, если на него ссылается RPC-подключение крипто. План: в internal/domain/integrations в сервисе пулов перед удалением вызвать crypto_integration_repo.CountEndpointsByPool из internal/configuration/crypto/infrastructure/storage/postgres/crypto_integration_repo и вернуть ошибку, если больше нуля. Это нормальное решение? Если нет — как правильно? Код пока не пиши.
