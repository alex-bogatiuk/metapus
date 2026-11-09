# Будущая миграция Vault-аутентификации в Kubernetes

> **TL;DR:** При переходе в Kubernetes меняется только auto-auth Vault Proxy с AppRole на Kubernetes auth; runtime-контракт worker и Go-код остаются прежними.
>
> **Тип:** Reference
>
> **Аудитория:** DevOps, Security, Developer
>
> **Связанные:** [Production-контур Vault и подписи TRON](vault-tron-production.md)

---

На текущем этапе Kubernetes manifests и Helm chart не поставляются. Этот
документ фиксирует будущий runtime-контракт, чтобы Docker/VM-реализация не
создавала последующую миграцию на уровне Go-кода.

## Граница Pod

- Запускать `metapus-worker` и один выделенный Vault Proxy в одном Pod.
- Привязать Proxy к `127.0.0.1:8100`; не создавать Service для этого порта.
- Сохранить `VAULT_ADDR=http://127.0.0.1:8100` и
  `CRYPTO_AUTH_SIGNER=transit`. Изменения Go-кода не нужны.
- Использовать отдельный ServiceAccount только для worker workload.
- Установить `automountServiceAccountToken: false` и у Pod, и у
  ServiceAccount.
- Смонтировать projected краткоживущий ServiceAccount token только в Vault
  Proxy с `audience: vault`. Worker container не должен видеть этот token.

## Vault Kubernetes auth

Меняется только блок `auto_auth.method` в конфигурации Proxy:

```hcl
auto_auth {
  method {
    type       = "kubernetes"
    mount_path = "auth/kubernetes"
    config = {
      role       = "metapus-worker"
      token_path = "/var/run/secrets/vault/token"
    }
  }
}
```

Блок `api_proxy { use_auto_auth_token = "force" }` остаётся без изменений.

Vault role должна быть связана с тремя точными измерениями identity:

- имя ServiceAccount;
- namespace Kubernetes;
- token audience `vault`.

Если используемые версии Vault/Kubernetes auth поддерживают эту возможность,
ServiceAccount UID следует использовать как alias identity. Wildcard для
ServiceAccount или namespace запрещён. Worker policy с явным перечнем tenant
переиспользуется из VM/AppRole-развёртывания.

## Что остаётся неизменным

- per-tenant Transit key names и versioned `kid`;
- authorization keyring TRON plugin;
- sweep destinations allowlist;
- replay protection approval intent;
- readiness contract и fail-closed поведение;
- отсутствие Vault credentials в worker environment.

AppRole RoleID, wrapped SecretID и bootstrap-файл исчезают: первичную identity
workload подтверждает projected ServiceAccount token, доступный только Proxy.

## Критерии приёмки будущей миграции

1. Worker не содержит Vault token и не имеет mount projected Kubernetes token.
2. Только Proxy читает projected token и проходит аутентификацию.
3. Token с неверными audience, namespace или ServiceAccount отклоняется.
4. Запросы worker через localhost получают только forced Proxy token policy.
5. Сетевые политики запрещают worker обращаться к удалённому Vault в обход
   localhost Proxy.
6. Transit signing, readiness plugin, approval replay protection и sweep
   allowlist проходят прежние тесты без изменения application code.
7. Потеря Proxy или Vault удерживает операцию до подписи и не приводит к
   broadcast.

Официальные материалы: [Vault Proxy auto-auth](https://developer.hashicorp.com/vault/docs/agent-and-proxy/proxy/apiproxy), [Kubernetes auto-auth](https://developer.hashicorp.com/vault/docs/agent-and-proxy/autoauth/methods/kubernetes).
