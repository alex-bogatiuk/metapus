package metapus

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"
)

func TestVerifyWebhook(t *testing.T) {
	body, err := os.ReadFile("../../../api/fixtures/invoice.completed.json")
	if err != nil {
		t.Fatal(err)
	}
	timestamp := "1787724723"
	mac := hmac.New(sha256.New, []byte("whsec_test"))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(body)
	header := "t=" + timestamp + ",v1=" + strings.Repeat("00", 32) + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	event, err := VerifyWebhook[map[string]any](body, header, "whsec_test", time.Unix(1787724723, 0), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if event.ID != "evt_01K3MZ7V6X4H8Q2T9N5R1C0BFA" || event.Type != EventInvoiceCompleted {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestVerifyWebhookRejectsStaleTimestamp(t *testing.T) {
	_, err := VerifyWebhook[map[string]any]([]byte(`{}`), "t=1,v1=00", "secret", time.Unix(1000, 0), 5*time.Minute)
	if err == nil {
		t.Fatal("expected stale signature rejection")
	}
}
