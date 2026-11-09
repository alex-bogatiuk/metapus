import { createHmac } from "node:crypto";
import { readFile } from "node:fs/promises";
import test from "node:test";
import assert from "node:assert/strict";

import { MetapusClient, verifyWebhook } from "../dist/index.js";

const body = await readFile(new URL("../../../api/fixtures/invoice.completed.json", import.meta.url));
const timestamp = "1787724723";
const secret = "whsec_test";
const signature = createHmac("sha256", secret).update(timestamp).update(".").update(body).digest("hex");

test("verifies raw body and accepts one of dual rotation signatures", () => {
  const event = verifyWebhook(body, `t=${timestamp},v1=${"00".repeat(32)},v1=${signature}`, secret, 1787724723000);
  assert.equal(event.type, "invoice.completed");
  assert.equal(event.data.object.status, "completed");
});

test("rejects stale delivery timestamps", () => {
  assert.throws(() => verifyWebhook(body, `t=${timestamp},v1=${signature}`, secret, 1787725723000), /outside tolerance/);
});

test("API client sends idempotency and supports empty cancel response", async () => {
  const requests = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init });
    if (String(url).endsWith("/cancel")) return new Response(null, { status: 204 });
    return new Response(JSON.stringify({ id: "inv_test", status: "created" }), {
      status: 201,
      headers: { "Content-Type": "application/json" },
    });
  };
  try {
    const client = new MetapusClient("https://api.example/", "mk_test");
    await client.createInvoice({ amount: "100", currency: "USDT-TRC20" }, "order-1-create");
    await client.cancelInvoice("inv/test");
  } finally {
    globalThis.fetch = originalFetch;
  }

  assert.equal(requests[0].url, "https://api.example/merchant/v1/invoices");
  assert.equal(new Headers(requests[0].init.headers).get("Idempotency-Key"), "order-1-create");
  assert.equal(new Headers(requests[0].init.headers).get("X-Api-Key"), "mk_test");
  assert.equal(requests[1].url, "https://api.example/merchant/v1/invoices/inv%2Ftest/cancel");
  assert.equal(requests[1].init.method, "POST");
});

test("API client covers webhook endpoint CRUD and test operations", async () => {
  const requests = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (url, init = {}) => {
    requests.push({ url: String(url), init });
    if (init.method === "DELETE") return new Response(null, { status: 204 });
    if (String(url).endsWith("/test")) {
      return new Response(JSON.stringify({ eventId: "evt_test", success: true }), { status: 200 });
    }
    return new Response(JSON.stringify({ id: "ep_test", object: "webhookEndpoint" }), { status: 200 });
  };
  try {
    const client = new MetapusClient("https://api.example", "mk_test");
    await client.getWebhookEndpoint("ep/test");
    await client.updateWebhookEndpoint("ep/test", { version: 1, isActive: false });
    await client.testWebhookEndpoint("ep/test");
    await client.deleteWebhookEndpoint("ep/test");
  } finally {
    globalThis.fetch = originalFetch;
  }
  assert.deepEqual(requests.map((request) => request.init.method ?? "GET"), ["GET", "PATCH", "POST", "DELETE"]);
  assert.ok(requests.every((request) => request.url.includes("/webhook-endpoints/ep%2Ftest")));
});
