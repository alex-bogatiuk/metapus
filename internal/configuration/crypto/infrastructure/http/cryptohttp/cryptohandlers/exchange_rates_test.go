package cryptohandlers

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/portal_repo"
	"metapus/pkg/sdk/http/middleware"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/registers/exchange_rate"
	"metapus/pkg/sdk/types"
	"net/http/httptest"
	"testing"
	"time"
)

type valuationFixture struct {
	cfg       exchange_rate.Configuration
	candidate exchange_rate.Candidate
	failure   error
}

func (f *valuationFixture) LoadConfiguration(context.Context) (exchange_rate.Configuration, error) {
	return f.cfg, nil
}
func (f *valuationFixture) Candidates(context.Context, exchange_rate.Configuration, []id.ID, time.Time) ([]exchange_rate.Candidate, error) {
	return []exchange_rate.Candidate{f.candidate}, nil
}
func (f *valuationFixture) GetMerchantBalances(_ context.Context, merchants []id.ID) ([]crypto.BalanceRow, error) {
	if f.failure != nil {
		return nil, f.failure
	}
	return []crypto.BalanceRow{{MerchantID: merchants[0], TokenID: id.New(), TokenSymbol: "USDT", CurrencyID: &f.candidate.Currency.ID, RawAmount: types.NewRawAmountFromInt64(5)}}, nil
}
func (f *valuationFixture) GetPendingAmounts(context.Context, []id.ID) (map[string]types.RawAmount, error) {
	return nil, nil
}
func TestPortalValuationContractScopeAndDatabaseErrors(t *testing.T) {
	currency := exchange_rate.CurrencyRef{ID: id.New(), Code: "USDT"}
	usd := exchange_rate.CurrencyRef{ID: id.New(), Code: "USD", DecimalPlaces: 2}
	source := exchange_rate.SourceRef{ID: id.New(), Name: "CoinGecko", SourceType: "coingecko"}
	quote := &exchange_rate.Quote{ID: id.New(), Currency: currency, BaseCurrency: usd, Source: source, Rate: decimal.RequireFromString("0.9997"), Multiplier: 1, MappingVersion: 1, FeedAttemptID: id.New(), Version: 1, ProviderUpdatedAt: time.Now().Add(-time.Minute)}
	f := &valuationFixture{cfg: exchange_rate.Configuration{BaseCurrency: usd, Source: &source, SourceActive: true}, candidate: exchange_rate.Candidate{Currency: currency, Mapping: &exchange_rate.Mapping{IsActive: true, Version: 1}, Quote: quote}}
	snapshot := func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
	h := &PortalHandler{repo: portal_repo.NewDashboardRepo(), balanceCalc: cryptoapp.NewValuationService(f, exchange_rate.NewService(f), snapshot)}
	merchantID := id.New()
	ctx := merchant.WithPortalScope(context.Background(), merchant.PortalScope{MerchantIDs: []id.ID{merchantID}, Roles: map[id.ID]merchant.PortalRole{merchantID: merchant.PortalRoleOwner}})
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.Use(func(c *gin.Context) { c.Request = c.Request.WithContext(ctx); c.Next() })
	router.GET("/balance", h.GetBalance)
	router.GET("/detailed", h.GetBalanceDetailed)
	get := func(path string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.Equal(t, want, w.Code, w.Body.String())
		return w
	}
	a := get("/balance", 200)
	b := get("/detailed", 200)
	var first, second crypto.MerchantBalance
	require.NoError(t, json.Unmarshal(a.Body.Bytes(), &first))
	require.NoError(t, json.Unmarshal(b.Body.Bytes(), &second))
	require.Equal(t, first.Valuation, second.Valuation)
	require.Equal(t, "5.00", *first.Valuation.Total.Amount)
	require.NotContains(t, a.Body.String(), "mappingVersion")
	require.NotContains(t, a.Body.String(), "feedAttemptId")
	require.NotContains(t, a.Body.String(), "externalId")
	get("/balance?source=", 400)
	get("/detailed?source=manual", 400)
	get("/balance?merchant_id="+id.New().String(), 403)
	f.failure = errors.New("database unavailable")
	w := get("/balance", 500)
	require.NotContains(t, w.Body.String(), `"coverage"`)
}
