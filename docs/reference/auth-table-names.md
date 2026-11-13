# Имена таблиц безопасности

Физические SQL-имена отделены от HTTP-маршрутов, permission codes и логического
reference key `users`. Маршруты `/auth/users`, `/auth/roles`, permissions
`admin.users`/`admin.roles` и коды действий step-up сохраняют свой контракт.

| Ранее | Таблица свежей БД |
|---|---|
| users | auth_users |
| roles | auth_roles |
| permissions | auth_permissions |
| role_permissions | auth_role_permissions |
| user_roles | auth_user_roles |
| refresh_tokens | auth_refresh_tokens |
| security_profiles | auth_security_profiles |
| security_profile_dimensions | auth_security_profile_dimensions |
| security_profile_field_policies | auth_security_profile_field_policies |
| user_security_profiles | auth_user_security_profiles |
| security_policy_rules | auth_security_policy_rules |
| user_mfa_totp | auth_user_mfa_totp |
| user_preferences | sys_user_preferences |

`auth_policy_state`, `auth_sessions`, `auth_mfa_challenges` и
`auth_step_up_confirmations` уже имели префикс. Используйте новые физические имена
в SQL, migrations, seeds, fixtures и операционных скриптах. Исходные миграции
согласованно обновляют FK и trigger functions. Автоматического переименования таблиц уже применённой схемы нет: перенос старой БД требует отдельного операторского плана.
