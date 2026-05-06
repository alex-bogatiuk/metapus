// internal/domain/crypto/sweep_config_test.go
package crypto

import (
	"math/big"
	"testing"

	"metapus/internal/core/types"
)

func TestSweepConfig_IsZeroThreshold(t *testing.T) {
	tests := []struct {
		give string
		cfg  SweepConfig
		want bool
	}{
		{
			give: "zero amount → true",
			cfg:  SweepConfig{Threshold: types.NewCryptoAmount(big.NewInt(0))},
			want: true,
		},
		{
			give: "positive amount → false",
			cfg:  SweepConfig{Threshold: types.NewCryptoAmount(big.NewInt(1000))},
			want: false,
		},
		{
			give: "zero-value struct (nil internal) → true",
			cfg:  SweepConfig{},
			want: true,
		},
		{
			give: "large threshold 10M → false",
			cfg:  SweepConfig{Threshold: types.NewCryptoAmount(big.NewInt(10_000_000))},
			want: false,
		},
		{
			give: "negative amount → true (treated as non-positive)",
			cfg:  SweepConfig{Threshold: types.NewCryptoAmount(big.NewInt(-1))},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			got := tt.cfg.IsZeroThreshold()
			if got != tt.want {
				t.Errorf("IsZeroThreshold() = %v, want %v", got, tt.want)
			}
		})
	}
}
