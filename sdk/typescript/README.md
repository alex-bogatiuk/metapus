# Metapus TypeScript merchant SDK

Pass the exact request bytes to `verifyWebhook`; parsing and re-serializing JSON invalidates the signature. Fulfil an order only for `invoice.completed`.

Store `event.id` in the same database transaction as the order transition. A minimal PostgreSQL pattern is:

```sql
BEGIN;
INSERT INTO processed_webhook_events(event_id) VALUES ($1) ON CONFLICT DO NOTHING;
-- Continue only when one row was inserted; then lock and complete the order.
COMMIT;
```

After verification, retrieve the canonical invoice with `client.getInvoice(event.data.object.id)` before high-value fulfilment.
Use `client.cancelInvoice(id)` only before finality; the operation is idempotent and a final invoice returns `409`.

Use `client.bindInvoiceReference(id, { externalId: "1C-entry-1" }, retryKey)` for an automatic persistent-wallet top-up without an order. You may also pass `orderId`, or both references. Omitted references preserve stored values; assigned references cannot be changed or cleared. Omit `metadata` to preserve it, or pass `{}` to clear it.
