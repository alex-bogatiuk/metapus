import { createHmac, randomUUID, timingSafeEqual } from "node:crypto";

export type InvoiceStatus = "created" | "partially_paid" | "paid" | "confirmed" | "completed" | "underpaid" | "expired" | "cancelled" | "reversed";
export type EventType =
  | "invoice.created" | "invoice.paid" | "invoice.confirmed" | "invoice.completed"
  | "invoice.overpaid" | "invoice.underpaid" | "invoice.expired" | "invoice.cancelled" | "invoice.reversed"
  | "invoice.late_payment"
  | "payment.detected" | "payment.confirming" | "payment.confirmed" | "payment.reorged" | "payment.replaced"
  | "withdrawal.confirmed" | "withdrawal.failed" | "withdrawal.partially_completed" | "wallet.risk_changed" | "deposit_address.rotated";
export interface MerchantEvent<T = Record<string, unknown>> {
  id: string; object: "event"; apiVersion: string; type: EventType; createdAt: string;
  data: { object: T };
}
export interface Invoice {
  id: string; status: InvoiceStatus; resourceVersion: number;
  expectedAmount: string; receivedAmount: string; confirmedAmount: string;
  creditedAmount: string; overpaidAmount: string; currency: string; network: string;
  walletAddress: string; requiredConfirmations: number; finalityMode: string;
  orderId?: string; externalId?: string; metadata: Record<string, string>; paidAt?: string; confirmedAt?: string;
  completedAt?: string; reversedAt?: string; asOf: string; chainObservedAt?: string;
  payments: Payment[];
}
export type BindInvoiceReferenceRequest =
  ({ orderId: string; externalId?: string } | { orderId?: string; externalId: string }) &
  { metadata?: Record<string, string> };
export interface Payment {
  id: string; txHash: string; logIndex: number; amount: string; status: string;
  canonicality: "canonical" | "orphaned" | "unknown"; finalityStatus: string;
  confirmations: number; requiredConfirmations: number; sourceAddress?: string;
  sourceNonce?: number; replacementTxHash?: string; detectedAt: string; confirmedAt?: string;
}
export interface UpdateWebhookEndpointRequest {
  version: number; name?: string; url?: string; enabledEvents?: EventType[]; isDefault?: boolean; isActive?: boolean;
}
export interface WebhookEndpoint {
  id: string; object: "webhookEndpoint"; name: string; url: string;
  enabledEvents: EventType[]; isDefault: boolean; isActive: boolean;
  secretVersion: number; version: number; secret?: string; previousSecretValidUntil?: string;
  createdAt: string; updatedAt: string;
}
export interface MerchantEventRecord {
  id: string; object: "event"; type: EventType; apiVersion: string;
  resourceVersion: number; invoiceId?: string; createdAt: string; payload: MerchantEvent;
}
export interface WebhookDelivery {
  id: string; eventId: string; deliveryId: string; eventType: EventType;
  webhookUrl: string; statusCode?: number; responseTimeMs?: number; attempt: number;
  errorMessage?: string; errorClass?: string; responseBody?: string;
  nextRetryAt?: string; createdAt: string;
}
export interface WebhookTestResult {
  eventId: string; deliveryId?: string; success: boolean; statusCode?: number;
  durationMs?: number; error?: string;
}
export interface ListResponse<T> { object: "list"; items: T[]; total: number }

export function verifyWebhook(rawBody: Buffer, signature: string, secret: string, now = Date.now(), toleranceSeconds = 300): MerchantEvent {
  const fields = signature.split(",");
  const timestamp = fields.find((value) => value.startsWith("t="))?.slice(2);
  const signatures = fields.filter((value) => value.startsWith("v1=")).map((value) => value.slice(3));
  if (!timestamp || signatures.length === 0 || !/^\d+$/.test(timestamp)) throw new Error("invalid Metapus-Signature");
  const age = Math.abs(Math.floor(now / 1000) - Number(timestamp));
  if (age > toleranceSeconds) throw new Error("webhook timestamp is outside tolerance");
  const expected = createHmac("sha256", secret).update(timestamp).update(".").update(rawBody).digest();
  const valid = signatures.some((candidate) => {
    try { const supplied = Buffer.from(candidate, "hex"); return supplied.length === expected.length && timingSafeEqual(supplied, expected); }
    catch { return false; }
  });
  if (!valid) throw new Error("invalid webhook signature");
  return JSON.parse(rawBody.toString("utf8")) as MerchantEvent;
}

export class MetapusClient {
  constructor(private readonly baseUrl: string, private readonly apiKey: string) {}
  async createInvoice(body: Record<string, unknown>, idempotencyKey: string): Promise<Invoice> {
    return this.request("/invoices", { method: "POST", headers: { "Idempotency-Key": idempotencyKey }, body: JSON.stringify(body) });
  }
  async bindInvoiceReference(id: string, body: BindInvoiceReferenceRequest, idempotencyKey: string): Promise<Invoice> {
    if (!idempotencyKey.trim()) throw new Error("Idempotency-Key is required");
    return this.request(`/invoices/${encodeURIComponent(id)}/reference`, {
      method: "PATCH", headers: { "Idempotency-Key": idempotencyKey }, body: JSON.stringify(body)
    });
  }
  async getInvoice(id: string): Promise<Invoice> { return this.request(`/invoices/${encodeURIComponent(id)}`); }
  async cancelInvoice(id: string): Promise<void> {
    await this.request<void>(`/invoices/${encodeURIComponent(id)}/cancel`, { method: "POST" });
  }
  async listWebhookEndpoints(): Promise<ListResponse<WebhookEndpoint>> { return this.request("/webhook-endpoints"); }
  async createWebhookEndpoint(body: { name: string; url: string; enabledEvents: EventType[]; isDefault?: boolean }): Promise<WebhookEndpoint> {
    return this.request("/webhook-endpoints", { method: "POST", body: JSON.stringify(body) });
  }
  async getWebhookEndpoint(endpointId: string): Promise<WebhookEndpoint> {
    return this.request(`/webhook-endpoints/${encodeURIComponent(endpointId)}`);
  }
  async updateWebhookEndpoint(endpointId: string, body: UpdateWebhookEndpointRequest): Promise<WebhookEndpoint> {
    return this.request(`/webhook-endpoints/${encodeURIComponent(endpointId)}`, { method: "PATCH", body: JSON.stringify(body) });
  }
  async deleteWebhookEndpoint(endpointId: string): Promise<void> {
    await this.request<void>(`/webhook-endpoints/${encodeURIComponent(endpointId)}`, { method: "DELETE" });
  }
  async rotateWebhookSecret(endpointId: string): Promise<WebhookEndpoint> {
    return this.request(`/webhook-endpoints/${encodeURIComponent(endpointId)}/rotate-secret`, { method: "POST" });
  }
  async testWebhookEndpoint(endpointId: string): Promise<WebhookTestResult> {
    return this.request(`/webhook-endpoints/${encodeURIComponent(endpointId)}/test`, { method: "POST" });
  }
  async listEvents(limit = 20, offset = 0): Promise<ListResponse<MerchantEventRecord>> {
    return this.request(`/events?limit=${limit}&offset=${offset}`);
  }
  async listEventDeliveries(eventId: string): Promise<ListResponse<WebhookDelivery>> {
    return this.request(`/events/${encodeURIComponent(eventId)}/deliveries`);
  }
  async redeliverEvent(eventId: string, endpointId: string): Promise<{ status: "pending" }> {
    return this.request(`/events/${encodeURIComponent(eventId)}/redeliver`, { method: "POST", body: JSON.stringify({ endpointId }) });
  }
  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(init.headers);
    headers.set("Content-Type", "application/json");
    headers.set("X-Api-Key", this.apiKey);
    const method = (init.method ?? "GET").toUpperCase();
    if (["POST", "PUT", "PATCH", "DELETE"].includes(method) && !headers.has("Idempotency-Key")) {
      headers.set("Idempotency-Key", randomUUID());
    }
    const response = await fetch(`${this.baseUrl.replace(/\/$/, "")}/merchant/v1${path}`, {
      ...init, headers, redirect: "error"
    });
    if (!response.ok) throw new Error(`Metapus API returned ${response.status}: ${await response.text()}`);
    if (response.status === 204) return undefined as T;
    return response.json() as Promise<T>;
  }
}
