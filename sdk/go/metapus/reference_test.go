package metapus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBindInvoiceReferenceCarriesRetryKeyAndMetadataPresence(t *testing.T) {
	var bodies []map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.EscapedPath() != "/merchant/v1/invoices/inv%2F1/reference" {
			t.Errorf("reference request = %s %s", r.Method, r.URL.EscapedPath())
		}
		if r.Header.Get("X-Api-Key") != "mk_test" || r.Header.Get("Idempotency-Key") != "reference-retry" {
			t.Error("authentication or supplied retry key missing")
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"inv/1","orderId":"ORD-1","externalId":"ERP-45001","metadata":{}}`))
	}))
	defer server.Close()
	client := &Client{BaseURL: server.URL, APIKey: "mk_test", HTTPClient: server.Client()}
	for _, metadata := range []*map[string]string{nil, {}, {"sap_doc": "45001"}} {
		invoice, err := client.BindInvoiceReference(context.Background(), "inv/1", "reference-retry", BindInvoiceReferenceRequest{OrderID: "ORD-1", Metadata: metadata})
		if err != nil {
			t.Fatal(err)
		}
		if invoice.OrderID != "ORD-1" || invoice.ExternalID != "ERP-45001" {
			t.Fatalf("unexpected invoice: %+v", invoice)
		}
	}
	if _, exists := bodies[0]["metadata"]; exists {
		t.Fatal("omitted metadata must stay absent")
	}
	if string(bodies[1]["metadata"]) != "{}" || string(bodies[2]["metadata"]) != `{"sap_doc":"45001"}` {
		t.Fatalf("metadata = %#v", bodies)
	}
	if _, err := client.BindInvoiceReference(context.Background(), "inv/1", "", BindInvoiceReferenceRequest{OrderID: "ORD-1"}); err == nil {
		t.Fatal("missing retry key accepted")
	}
	if _, err := client.BindInvoiceReference(context.Background(), "inv/1", "reference-retry", BindInvoiceReferenceRequest{ExternalID: "1C-entry-1"}); err != nil {
		t.Fatal(err)
	}
	if _, exists := bodies[3]["orderId"]; exists {
		t.Fatal("external-only binding must omit orderId")
	}
	if string(bodies[3]["externalId"]) != `"1C-entry-1"` {
		t.Fatalf("externalId = %s", bodies[3]["externalId"])
	}
}
