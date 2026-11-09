package rate_source

import (
	"context"
	"strings"
	"testing"
)

func TestRateSourceAPIKeyValidation(t *testing.T) {
	rs := NewRateSource("CG", "CoinGecko", "coingecko")
	rs.SetAPIKey("  demo-key  ")
	if rs.APIKey == nil || *rs.APIKey != "demo-key" {
		t.Fatalf("normalized api key = %v", rs.APIKey)
	}
	if err := rs.Validate(context.Background()); err != nil {
		t.Fatalf("valid api key rejected: %v", err)
	}

	rs.SetAPIKey("line\nbreak")
	if err := rs.Validate(context.Background()); err == nil {
		t.Fatal("api key with a line break must be rejected")
	}

	rs.SetAPIKey(strings.Repeat("a", 513))
	if err := rs.Validate(context.Background()); err == nil {
		t.Fatal("api key over 512 bytes must be rejected")
	}

	rs.SetAPIKey("   ")
	if rs.APIKey != nil {
		t.Fatal("blank api key must clear the requisite")
	}
}
