import { test } from "node:test";
import assert from "node:assert/strict";
import { MetapusClient } from "../dist/index.js";

test("reference binding preserves the supplied retry key and metadata presence", async () => {
  const originalFetch = globalThis.fetch;
  const bodies = [];
  globalThis.fetch = async (url, init) => {
    assert.equal(url, "https://merchant.example/merchant/v1/invoices/inv%2F1/reference");
    assert.equal(init.method, "PATCH");
    assert.equal(init.headers.get("X-Api-Key"), "mk_test");
    assert.equal(init.headers.get("Idempotency-Key"), "reference-retry");
    bodies.push(JSON.parse(init.body));
    return new Response(JSON.stringify({ id: "inv/1", orderId: "ORD-1", externalId: "ERP-45001", metadata: {} }), {
      headers: { "Content-Type": "application/json" }
    });
  };
  try {
    const client = new MetapusClient("https://merchant.example", "mk_test");
    for (const body of [{ orderId: "ORD-1" }, { orderId: "ORD-1", metadata: {} }, { orderId: "ORD-1", metadata: { sap_doc: "45001" } }]) {
      const invoice = await client.bindInvoiceReference("inv/1", body, "reference-retry");
      assert.equal(invoice.orderId, "ORD-1");
      assert.equal(invoice.externalId, "ERP-45001");
    }
    assert.equal(Object.hasOwn(bodies[0], "metadata"), false);
    assert.deepEqual(bodies[1].metadata, {});
    assert.deepEqual(bodies[2].metadata, { sap_doc: "45001" });
    await assert.rejects(client.bindInvoiceReference("inv/1", { orderId: "ORD-1" }, ""), /Idempotency-Key/);
    assert.equal(bodies.length, 3);
	await client.bindInvoiceReference("inv/1", { externalId: "1C-entry-1" }, "reference-retry");
	assert.deepEqual(bodies[3], { externalId: "1C-entry-1" });
  } finally {
    globalThis.fetch = originalFetch;
  }
});
