---
name: managing-migrations
description: Use when adding or updating database schemas across the multi-tenant landscape. Enforces the "Database-per-Tenant" rule and CDC field consistency.
---

# Managing Migrations in Metapus

This skill provides mandatory guidelines for managing database migrations in the Metapus multi-tenant architecture.

## Goal
Ensure schema consistency and physical isolation between tenants while maintaining high performance and Change Data Capture (CDC) capabilities.

## Core Rules
1. **DATABASE-PER-TENANT**: Physical isolation through separate databases.
2. **NO TENANT DISCRIMINATOR**: Do NOT add `tenant_id` columns to business tables.
3. **MANDATORY CDC**: All `cat_*` and `doc_*` tables must include `_txid` and `_deleted_at` fields.
4. **NAMING CONVENTION**: Tables must match entity prefixes (`cat_`, `doc_`, `reg_`).
5. **EARLY DEVELOPMENT STRATEGY**: While the project is in development and databases are recreated from scratch, **do NOT create new migration files** to modify existing objects. Instead, **edit the original migration file** that created the table/view/function.

## Instructions

### 1. Modifying Existing Objects
Since current databases are ephemeral and recreated during development:
- To add/remove columns, change constraints, or update logic, find the original migration in `db/migrations/` that created the object.
- Directly modify the `CREATE TABLE` or `CREATE VIEW` statement in that file.
- This maintains a clean and readable history of the "current schema state" without hundreds of incremental migrations.

### 2. Creating New Objects
- For entirely new entities (new catalog, new document), create a new migration in `db/migrations/`.
- Filename format: `NNNNN_description.sql` (e.g., `00031_cat_categories.sql`).

### 2. Standard Table Template
```sql
CREATE TABLE cat_brands (
    id UUID PRIMARY KEY,
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version INTEGER NOT NULL DEFAULT 1,
    code VARCHAR(50) NOT NULL,
    name VARCHAR(255) NOT NULL,
    country VARCHAR(100),
    attributes JSONB,
    -- CDC Fields
    _txid BIGINT,
    _deleted_at TIMESTAMPTZ
);

-- Index for code/name search
CREATE INDEX idx_cat_brands_code ON cat_brands(code);
CREATE INDEX idx_cat_brands_name ON cat_brands(name);
```

### 3. CDC Triggers
Always apply the universal CDC trigger to new catalog/document tables:
```sql
SELECT fn_create_cdc_trigger('cat_brands');
```

## Constraints
- **Do NOT** use `UPDATE` on registers (Immutable Ledger).
- **Do NOT** create cross-tenant queries.
- **Always** use `TIMESTAMPTZ` for timestamps.
- **Always** test migrations on a clean setup.
