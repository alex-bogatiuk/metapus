package dto

import (
	"strings"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/domain/catalogs/rate_source"
)

func TestRateSourceAPIKeyIsWriteOnly(t *testing.T) {
	rs := rate_source.NewRateSource("CG", "CoinGecko", "coingecko")
	rs.SetAPIKey("stored-demo-key")

	raw, err := json.Marshal(FromRateSource(rs))
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if strings.Contains(string(raw), "stored-demo-key") || strings.Contains(string(raw), `"apiKey"`) {
		t.Fatalf("response exposed api key: %s", raw)
	}
	if !strings.Contains(string(raw), `"apiKeyConfigured":true`) {
		t.Fatalf("response omitted configured marker: %s", raw)
	}

	(&UpdateRateSourceRequest{APIKey: nil}).ApplyTo(rs)
	if rs.APIKey == nil || *rs.APIKey != "stored-demo-key" {
		t.Fatal("omitted apiKey must preserve the stored value")
	}
	replacement := "replacement"
	(&UpdateRateSourceRequest{APIKey: &replacement}).ApplyTo(rs)
	if rs.APIKey == nil || *rs.APIKey != replacement {
		t.Fatal("supplied apiKey must replace the stored value")
	}
	empty := ""
	(&UpdateRateSourceRequest{APIKey: &empty}).ApplyTo(rs)
	if rs.APIKey != nil {
		t.Fatal("empty apiKey must clear the stored value")
	}
}
