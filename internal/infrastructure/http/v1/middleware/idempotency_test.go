package middleware

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/application/command"
	corejson "github.com/alex-bogatiuk/metapus/internal/core/json"
)

func TestReadCanonicalCommandBodyRejectsTrailingJSONValue(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/merchant/v1/invoices", strings.NewReader(`{"amount":"10"} {"amount":"20"}`))

	_, _, err := readCanonicalCommandBody(request)
	if err == nil {
		t.Fatal("expected trailing JSON value to be rejected")
	}
}

func TestReadCanonicalCommandBodyIgnoresObjectFieldOrder(t *testing.T) {
	first := httptest.NewRequest(http.MethodPost, "/merchant/v1/invoices", strings.NewReader(`{"amount":"10","network":"tron"}`))
	second := httptest.NewRequest(http.MethodPost, "/merchant/v1/invoices", strings.NewReader(`{"network":"tron","amount":"10"}`))

	_, firstCanonical, firstErr := readCanonicalCommandBody(first)
	_, secondCanonical, secondErr := readCanonicalCommandBody(second)
	if firstErr != nil || secondErr != nil {
		t.Fatalf("canonicalize: first=%v second=%v", firstErr, secondErr)
	}
	if string(firstCanonical) != string(secondCanonical) {
		t.Fatalf("canonical bodies differ: %s != %s", firstCanonical, secondCanonical)
	}
}

func TestCommandFingerprintCoversBusinessBodyPathAndQuery(t *testing.T) {
	base := requestFingerprintForTest(t, "/portal/v1/invoices/inv_123?merchant_id=m_1&expand=payments", `{"amount":"10","ttlMinutes":30}`)
	queryReordered := requestFingerprintForTest(t, "/portal/v1/invoices/inv_123?expand=payments&merchant_id=m_1", `{"ttlMinutes":30,"amount":"10"}`)
	if base != queryReordered {
		t.Fatalf("equivalent request fingerprints differ: %s != %s", base, queryReordered)
	}

	variations := []struct {
		name string
		url  string
		body string
	}{
		{name: "ttl", url: "/portal/v1/invoices/inv_123?merchant_id=m_1&expand=payments", body: `{"amount":"10","ttlMinutes":31}`},
		{name: "resource path", url: "/portal/v1/invoices/inv_456?merchant_id=m_1&expand=payments", body: `{"amount":"10","ttlMinutes":30}`},
		{name: "merchant query", url: "/portal/v1/invoices/inv_123?merchant_id=m_2&expand=payments", body: `{"amount":"10","ttlMinutes":30}`},
		{name: "significant query", url: "/portal/v1/invoices/inv_123?merchant_id=m_1&expand=invoice", body: `{"amount":"10","ttlMinutes":30}`},
	}
	for _, variation := range variations {
		t.Run(variation.name, func(t *testing.T) {
			if got := requestFingerprintForTest(t, variation.url, variation.body); got == base {
				t.Fatalf("variation did not change fingerprint: %s", got)
			}
		})
	}
}

func requestFingerprintForTest(t *testing.T, target, body string) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	_, canonicalBody, err := readCanonicalCommandBody(request)
	if err != nil {
		t.Fatalf("canonicalize request body: %v", err)
	}
	fingerprint, err := command.Fingerprint(struct {
		Path  string              `json:"path"`
		Query string              `json:"query"`
		Body  corejson.RawMessage `json:"body"`
	}{
		Path:  request.URL.EscapedPath(),
		Query: canonicalQuery(request),
		Body:  canonicalBody,
	})
	if err != nil {
		t.Fatalf("fingerprint request: %v", err)
	}
	return fingerprint
}

func TestCommandResponseBufferBuildsApplicationEnvelope(t *testing.T) {
	writer := httptest.NewRecorder()
	buffer := newCommandResponseBuffer(responseWriterForTest{ResponseRecorder: writer})
	buffer.WriteHeader(http.StatusCreated)
	_, _ = buffer.WriteString(`{"id":"inv_123","object":"crypto_invoice","version":7}`)

	result := buffer.commandResult()
	if result.Outcome != "created" || result.ResourceType != "crypto_invoice" || result.ResourceID != "inv_123" || result.ResourceVersion != 7 {
		t.Fatalf("unexpected result envelope: %#v", result)
	}
}

func TestCommandResponseBufferAcceptsResourceVersionContract(t *testing.T) {
	writer := httptest.NewRecorder()
	buffer := newCommandResponseBuffer(responseWriterForTest{ResponseRecorder: writer})
	_, _ = buffer.WriteString(`{"id":"inv_456","object":"crypto_invoice","resourceVersion":9}`)

	result := buffer.commandResult()
	if result.ResourceVersion != 9 {
		t.Fatalf("resource version = %d, want 9", result.ResourceVersion)
	}
}

// responseWriterForTest supplies Gin's extra ResponseWriter methods while the
// production buffer remains tested through the same public contract.
type responseWriterForTest struct {
	*httptest.ResponseRecorder
}

func (w responseWriterForTest) CloseNotify() <-chan bool {
	return make(chan bool)
}

func (w responseWriterForTest) Pusher() http.Pusher { return nil }

func (w responseWriterForTest) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, http.ErrNotSupported
}

func (w responseWriterForTest) WriteString(value string) (int, error) {
	return w.ResponseRecorder.WriteString(value)
}

func (w responseWriterForTest) WriteHeaderNow() {}

func (w responseWriterForTest) Status() int { return w.Code }

func (w responseWriterForTest) Size() int { return w.Body.Len() }

func (w responseWriterForTest) Written() bool { return w.Code != 0 }
