package metapus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientCreateInvoiceAndCancelContract(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("X-Api-Key") != "mk_test" {
			t.Errorf("X-Api-Key = %q", r.Header.Get("X-Api-Key"))
		}
		switch r.URL.EscapedPath() {
		case "/merchant/v1/invoices":
			if r.Method != http.MethodPost || r.Header.Get("Idempotency-Key") != "order-1-create" {
				t.Errorf("create request = %s, idempotency=%q", r.Method, r.Header.Get("Idempotency-Key"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "inv_test", "status": "created"})
		case "/merchant/v1/invoices/inv%2Ftest/cancel":
			if r.Method != http.MethodPost {
				t.Errorf("cancel method = %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: "mk_test", HTTPClient: server.Client()}
	var invoice map[string]any
	if err := client.CreateInvoice(context.Background(), "order-1-create", map[string]string{
		"amount": "100", "currency": "USDT-TRC20",
	}, &invoice); err != nil {
		t.Fatal(err)
	}
	if invoice["id"] != "inv_test" {
		t.Fatalf("invoice = %#v", invoice)
	}
	if err := client.CancelInvoice(context.Background(), "inv/test"); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestClientWebhookEndpointCRUDAndTestContract(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.URL.EscapedPath() != "/merchant/v1/webhook-endpoints/ep%2Ftest" &&
			r.URL.EscapedPath() != "/merchant/v1/webhook-endpoints/ep%2Ftest/test" {
			t.Errorf("path = %s", r.URL.EscapedPath())
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.EscapedPath() == "/merchant/v1/webhook-endpoints/ep%2Ftest/test" {
			_ = json.NewEncoder(w).Encode(WebhookTestResult{EventID: "evt_test", Success: true})
			return
		}
		_ = json.NewEncoder(w).Encode(WebhookEndpoint{ID: "ep_test", Object: "webhookEndpoint"})
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: "mk_test", HTTPClient: server.Client()}
	ctx := context.Background()
	var endpoint WebhookEndpoint
	if err := client.GetWebhookEndpoint(ctx, "ep/test", &endpoint); err != nil {
		t.Fatal(err)
	}
	active := false
	if err := client.UpdateWebhookEndpoint(ctx, "ep/test", UpdateWebhookEndpointRequest{Version: 1, IsActive: &active}, &endpoint); err != nil {
		t.Fatal(err)
	}
	var testResult WebhookTestResult
	if err := client.TestWebhookEndpoint(ctx, "ep/test", &testResult); err != nil {
		t.Fatal(err)
	}
	if !testResult.Success {
		t.Fatal("test endpoint result was not successful")
	}
	if err := client.DeleteWebhookEndpoint(ctx, "ep/test"); err != nil {
		t.Fatal(err)
	}
	want := []string{http.MethodGet, http.MethodPatch, http.MethodPost, http.MethodDelete}
	if len(methods) != len(want) {
		t.Fatalf("methods = %v", methods)
	}
	for i := range want {
		if methods[i] != want[i] {
			t.Fatalf("methods = %v, want %v", methods, want)
		}
	}
}
