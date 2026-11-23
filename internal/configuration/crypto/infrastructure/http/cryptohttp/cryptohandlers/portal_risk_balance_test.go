package cryptohandlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/aml"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/middleware"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
)

type riskBalanceFixture struct {
	merchantID id.ID
	calls      int
}

func (f *riskBalanceFixture) ReadMerchantRiskBalance(_ context.Context, merchantID id.ID) (time.Time, []aml.MerchantBalanceEvidence, error) {
	f.merchantID = merchantID
	f.calls++
	return time.Now(), []aml.MerchantBalanceEvidence{{Scope: aml.BalanceScope{MerchantID: merchantID, NetworkID: id.New(), TokenID: id.New()}, LedgerMatches: true}}, nil
}

func TestPortalRiskBalanceRequiresAuthorizedExplicitMerchant(t *testing.T) {
	merchantID := id.New()
	fixture := &riskBalanceFixture{}
	h := &PortalHandler{amlBalanceService: cryptoapp.NewAMLBalanceService(fixture)}
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(merchant.WithPortalScope(c.Request.Context(), merchant.PortalScope{MerchantIDs: []id.ID{merchantID}}))
		c.Next()
	})
	router.GET("/balances/risk", h.GetRiskBalance)
	for _, tc := range []struct {
		query string
		code  int
	}{{"", http.StatusBadRequest}, {"?merchant_id=bad", http.StatusBadRequest}, {"?merchant_id=" + id.New().String(), http.StatusForbidden}} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/balances/risk"+tc.query, nil))
		require.Equal(t, tc.code, w.Code, w.Body.String())
	}
	require.Zero(t, fixture.calls, "authorization failure must not execute a financial read")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/balances/risk?merchant_id="+merchantID.String(), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, merchantID, fixture.merchantID)
	var response aml.MerchantBalanceSnapshot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Items, 1)
	require.Equal(t, merchantID, response.Items[0].MerchantID)
	require.Len(t, response.Items[0].Buckets, 4)
	require.NotEmpty(t, response.SnapshotID)
}
