---
name: handling-concurrency-and-locks
description: Use when implementing complex business logic with high contention risk, ensuring safe execution and preventing deadlocks.
---

# Handling Concurrency and Locks in Metapus

This skill provides strategies for managing concurrent access to data in a high-performance environment.

## Goal
To maintain data consistency under load while avoiding performance bottlenecks and deadlocks.

## Core Strategies
1. **OPTIMISTIC LOCKING**: Use the `Version` field and compare-and-swap (CAS) for most catalog and document updates.
2. **PESSIMISTIC LOCKING**: Use `SELECT FOR UPDATE` for critical calculations (e.g., checking balances before posting).
3. **RESOURCE ORDERING**: Sort resources globally to prevent circular waits (deadlocks).

## Instructions

### 1. Optimistic Locking (Update Pattern)
```sql
UPDATE cat_nomenclature 
SET name = $1, version = version + 1 
WHERE id = $2 AND version = $3; -- CAS pattern
```

### 2. Pessimistic Locking in Registers
When updating balances, always lock dimensions in a fixed order (e.g., alphabetized WarehouseID then ProductID).

### 3. Handling Conflicts
Detect `AffectedRows == 0` during updates and return `CodeConcurrentModification`.

## Constraints
- **NO** indefinite locks; keep transactions short.
- **Always** specify timeouts for pessimistic locks if supported by the driver.
- **Prefer** Retries (with backoff) for transient concurrency conflicts.
