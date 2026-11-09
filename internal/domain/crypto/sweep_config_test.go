// internal/domain/crypto/sweep_config_test.go
package crypto

import (
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
			cfg:  SweepConfig{Threshold: types.NewCryptoAmountFromInt64(0)},
			want: true,
		},
		{
			give: "positive amount → false",
			cfg:  SweepConfig{Threshold: types.NewCryptoAmountFromInt64(1000)},
			want: false,
		},
		{
			give: "zero-value struct (nil internal) → true",
			cfg:  SweepConfig{},
			want: true,
		},
		{
			give: "large threshold 10M → false",
			cfg:  SweepConfig{Threshold: types.NewCryptoAmountFromInt64(10_000_000)},
			want: false,
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

func TestSweepConfig_SweepableAmountRetainsRawDust(t *testing.T) {
	cfg := SweepConfig{RetainAmount: types.NewCryptoAmountFromInt64(10)}

	tests := []struct {
		name        string
		giveBalance int64
		want        int64
	}{
		{name: "zero balance", giveBalance: 0, want: 0},
		{name: "balance below reserve", giveBalance: 9, want: 0},
		{name: "balance equals reserve", giveBalance: 10, want: 0},
		{name: "balance exceeds reserve by one raw unit", giveBalance: 11, want: 1},
		{name: "ordinary balance", giveBalance: 100, want: 90},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cfg.SweepableAmount(types.NewCryptoAmountFromInt64(tt.giveBalance))
			if got.String() != types.NewCryptoAmountFromInt64(tt.want).String() {
				t.Fatalf("SweepableAmount(%d) = %s, want %d", tt.giveBalance, got.String(), tt.want)
			}
		})
	}
}

func TestSweepConfig_RepeatedSweepDoesNotConsumeReserve(t *testing.T) {
	cfg := SweepConfig{RetainAmount: types.NewCryptoAmountFromInt64(1)}

	firstSweep := cfg.SweepableAmount(types.NewCryptoAmountFromInt64(100_000_000))
	if firstSweep.String() != "99999999" {
		t.Fatalf("first sweep = %s, want 99999999", firstSweep.String())
	}

	remaining := types.NewCryptoAmountFromInt64(100_000_000).Sub(firstSweep)
	if secondSweep := cfg.SweepableAmount(remaining); !secondSweep.IsZero() {
		t.Fatalf("second sweep = %s, want 0", secondSweep.String())
	}
}
