package tron

import (
	"testing"
	"vault-plugin-secrets-tron/internal/json"
)

func TestRawAmountJSONUsesCanonicalString(t *testing.T) {
	const maximumUint256 = "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	var amount RawAmount
	if err := json.Unmarshal([]byte(`"`+maximumUint256+`"`), &amount); err != nil {
		t.Fatalf("unmarshal uint256: %v", err)
	}
	encoded, err := json.Marshal(amount)
	if err != nil {
		t.Fatalf("marshal uint256: %v", err)
	}
	if string(encoded) != `"`+maximumUint256+`"` {
		t.Fatalf("encoded = %s", encoded)
	}
}

func TestRawAmountRejectsJSONNumberAndNonCanonicalValues(t *testing.T) {
	for _, encoded := range []string{`1`, `null`, `""`, `"01"`, `"+1"`, `"-1"`, `"1.0"`} {
		var amount RawAmount
		if err := json.Unmarshal([]byte(encoded), &amount); err == nil {
			t.Fatalf("expected %s to be rejected", encoded)
		}
	}
}
