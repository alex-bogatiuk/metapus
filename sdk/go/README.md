# Metapus Go merchant SDK

Pass the unmodified request bytes and `Metapus-Signature` to `VerifyWebhook`. Fulfil only on `EventInvoiceCompleted`.

Insert `event.ID` into a table with a unique constraint and transition the order in the same SQL transaction. When the insert affects zero rows, acknowledge the duplicate with HTTP 200. For high-value orders, call `Client.GetInvoice` and verify the canonical `completed` state before commit.

`Client.CancelInvoice` is idempotent before finality; a final invoice returns a conflict.

`Client.BindInvoiceReference` binds `ExternalID` (your ERP transaction/document ID), `OrderID` (when an order exists), or both to an automatic persistent-wallet top-up. Pass a stable retry key. Omitted references preserve stored values; assigned references cannot be changed or cleared. `Metadata: nil` preserves the dictionary; a pointer to an empty map clears it.
