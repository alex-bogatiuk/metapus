package cryptohandlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"metapus/pkg/sdk/id"
)

func TestParseMerchantInvoiceListFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	merchantID := id.New()

	tests := []struct {
		name         string
		giveRawQuery string
		wantStatuses []string
		wantLimit    int
		wantErr      bool
	}{
		{
			name:         "valid filters with multiple statuses and time ranges",
			giveRawQuery: "status=completed,confirmed&currency=USDT-TRC20&orderId=ord-1&customerId=cust-1&createdFrom=2026-05-01T00:00:00Z&createdTo=2026-05-02T00:00:00Z&limit=25",
			wantStatuses: []string{"completed", "confirmed"},
			wantLimit:    25,
		},
		{
			name:         "rejects unknown status",
			giveRawQuery: "status=completed,unknown_state",
			wantErr:      true,
		},
		{
			name:         "rejects limit above 100",
			giveRawQuery: "limit=101",
			wantErr:      true,
		},
		{
			name:         "rejects inverted createdFrom and createdTo",
			giveRawQuery: "createdFrom=2026-05-02T00:00:00Z&createdTo=2026-05-01T00:00:00Z",
			wantErr:      true,
		},
		{
			name:         "rejects inverted completedFrom and completedTo",
			giveRawQuery: "completedFrom=2026-05-02T00:00:00Z&completedTo=2026-05-01T00:00:00Z",
			wantErr:      true,
		},
		{
			name:         "rejects invalid RFC3339 timestamp",
			giveRawQuery: "completedFrom=2026-05-01",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest(http.MethodGet, "/merchant/v1/invoices?"+tt.giveRawQuery, nil)
			c.Request = req

			got, err := parseMerchantInvoiceListFilter(c, merchantID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseMerchantInvoiceListFilter() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Limit != tt.wantLimit {
				t.Fatalf("Limit = %d, want %d", got.Limit, tt.wantLimit)
			}
			if len(got.Statuses) != len(tt.wantStatuses) {
				t.Fatalf("len(Statuses) = %d, want %d", len(got.Statuses), len(tt.wantStatuses))
			}
			for i := range tt.wantStatuses {
				if got.Statuses[i] != tt.wantStatuses[i] {
					t.Fatalf("Statuses[%d] = %q, want %q", i, got.Statuses[i], tt.wantStatuses[i])
				}
			}
		})
	}
}
