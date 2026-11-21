package cryptohttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegisterPaymentPageServesThemeableCheckout(t *testing.T) {
	t.Parallel()

	router := gin.New()
	RegisterPaymentPage(router)

	request := httptest.NewRequest(http.MethodGet, "/pay/i/ci_test", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", contentType)
	}

	body := response.Body.String()
	for _, want := range []string{
		`id="theme-toggle"`,
		`metapus-checkout-theme`,
		`prefers-color-scheme: dark`,
		`--background: 0 0% 3.9%; /* #0a0a0a */`,
		`--card: 0 0% 9%; /* #171717 */`,
		`--qr-frame-border-opacity: 1;`,
		`--qr-frame-border-opacity: 0;`,
		`max-width: 28rem;`,
		`overflow-wrap: anywhere;`,
		`id="receipt-shape"`,
		`id="receipt-outline"`,
		`.receipt-shape path`,
		`fill: hsl(var(--card));`,
		`stroke: hsl(var(--border));`,
		`vector-effect: non-scaling-stroke;`,
		`const RECEIPT_NOTCH_TARGET_WIDTH = 15;`,
		`const RECEIPT_NOTCH_GAP = 4;`,
		`parts.push('H', nextX - RECEIPT_NOTCH_GAP);`,
		`new ResizeObserver(updateReceiptShape)`,
		`setupReceiptShape();`,
		`setupThemeToggle();`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("payment page does not contain %q", want)
		}
	}

	for _, unwanted := range []string{
		`max-width: 26rem;`,
		`max-width: 30rem;`,
		`text-overflow: ellipsis;`,
		`.card::after`,
		`radial-gradient(`,
		`filter: drop-shadow(0 1px 0 hsl(var(--border)))`,
		`) 0 0 / 12px 8px repeat-x;`,
		`) 0 0 / 15px 8px repeat-x;`,
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("payment page contains obsolete receipt edge style %q", unwanted)
		}
	}
}
