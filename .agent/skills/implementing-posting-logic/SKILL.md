---
name: implementing-posting-logic
description: Use when implementing document posting logic (moving data from documents to accumulation/information registers). Enforces "Immutable Ledger" and Delta-update principles.
---

# Implementing Posting Logic in Metapus

This skill guides the implementation of document posting (transactional processing), ensuring data integrity and account balance consistency.

## Goal
Implement reliable, consistent, and performant document posting that follows the Metapus account model.

## Core Principles
1. **IMMUTABLE LEDGER**: Register movements are never updated. Re-posting creates a new version of movements.
2. **DELTA UPDATES**: Balances are updated by calculating the difference (delta) between old and new movements.
3. **EXPLICIT TRANSACTIONS**: All operations within a posting session must use the `tx.Manager`.

## Instructions

### 1. The Posting Process
1. Start a transaction via `tx.Manager`.
2. Lock necessary resources (Optimistic or Pessimistic `FOR UPDATE`).
3. Generate new movements based on document content.
4. If the document was previously posted:
   - Identify old movements.
   - Calculate deltas.
   - Update balances.
5. Save new movements.
6. Commit transaction.

### 2. Implementation Template (service/posting.go)
```go
func (s *PostingService) Post(ctx context.Context, doc *GoodsReceipt) error {
    return s.txManager.WithTransaction(ctx, func(ctx context.Context) error {
        // 1. Validation
        if err := doc.Validate(ctx); err != nil {
            return err
        }

        // 2. Generate movements
        movements := s.generateMovements(doc)

        // 3. Apply movements to registers
        if err := s.stockRegister.Apply(ctx, doc.ID, movements); err != nil {
            return err
        }

        // 4. Mark document as posted
        doc.Posted = true
        return s.repo.Update(ctx, doc)
    })
}
```

### 3. Resource Ordering
To prevent deadlocks, always sort dimension keys (e.g., WarehouseID + ProductID) before performing updates or locks in registers.

## Constraints
- **NO** `UPDATE` statements on `reg_*_movements` tables.
- **NO** global locks; prefer row-level locking.
- **Always** handle `CONCURRENT_MODIFICATION` errors.
