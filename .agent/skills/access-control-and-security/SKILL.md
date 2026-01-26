---
name: access-control-and-security
description: Use when implementing permission checks, roles, or audit trails. Standardizes how functional permissions and audit logs are managed.
---

# Access Control and Security in Metapus

This skill ensures that all platform operations are properly authorized and audited.

## Goal
To maintain a secure multi-tenant environment where data access is strictly governed by roles and functional scopes.

## Core Principles
1. **LEAST PRIVILEGE**: Grant only the minimum permissions required for a task.
2. **FUNCTIONAL SCOPES**: Use fine-grained permissions (e.g., `product:view_cost`, `invoice:post`) rather than generic ones.
3. **MANDATORY AUDIT**: Record who changed what and when for high-value business entities.

## Instructions

### 1. Permission Checks
Use `internal/core/security` helpers to verify permissions within the Domain layer services.
```go
if !security.Can(ctx, "nomenclature:update") {
    return apperror.NewForbidden("missing permission")
}
```

### 2. Audit Logging
Ensure that sensitive operations (Change of sensitive fields, Posting, Deletion) trigger an audit record in `sys_audit`.

### 3. Tenant Isolation Guardrails
Rely on the `Database-per-Tenant` model. Never manually inject tenant IDs into queries; verify that the connection pool in the context is the correct one for the user session.

## Constraints
- **NO** bypassing of security checks in production code.
- **NO** sensitive data (passwords, PII) in audit logs or error messages.
