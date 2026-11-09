# Metapus Python merchant SDK

Call `verify_webhook(raw_body, signature, secret)` before decoding or changing the request body. Fulfil only on `invoice.completed`.

Deduplicate `event["id"]` with a unique database constraint and update the order in the same transaction. If the insert conflicts, return HTTP 200 without repeating fulfilment. For high-value orders, call `client.get_invoice(event["data"]["object"]["id"])` and use that canonical status.

`client.cancel_invoice(id)` is idempotent before finality; a final invoice cannot be cancelled.

Use `client.bind_invoice_reference(id, retry_key, external_id="1C-entry-1")` for an automatic persistent-wallet top-up without an order. You may also pass `order_id`, or both references. Omitted references preserve stored values; assigned references cannot be changed or cleared. Omit `metadata` to preserve it, or pass `{}` to clear it.
