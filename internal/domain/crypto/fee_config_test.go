package crypto

import (
	"testing"

	"metapus/internal/core/types"
)

func TestEffectiveFee_Calculate(t *testing.T) {
	tests := []struct {
		give   string
		fee    EffectiveFee
		amount int64
		want   int64
	}{
		{
			give:   "zero config → zero fee",
			fee:    EffectiveFee{},
			amount: 1_000_000,
			want:   0,
		},
		{
			give: "percent only: 1% of 100 USDT",
			fee: EffectiveFee{
				PercentBP: 100, // 1%
			},
			amount: 100_000_000, // 100 USDT (6 decimals)
			want:   1_000_000,   // 1 USDT
		},
		{
			give: "fixed only: 2 USDT",
			fee: EffectiveFee{
				FixedFee: types.NewCryptoAmountFromInt64(2_000_000),
			},
			amount: 100_000_000,
			want:   2_000_000,
		},
		{
			give: "compound: 1% + 1 USDT on 500 USDT",
			fee: EffectiveFee{
				FixedFee:  types.NewCryptoAmountFromInt64(1_000_000), // 1 USDT
				PercentBP: 100,                                       // 1%
			},
			amount: 500_000_000, // 500 USDT
			want:   6_000_000,   // 1 + 5 = 6 USDT
		},
		{
			give: "min floor applies: calculated < minFee",
			fee: EffectiveFee{
				FixedFee:  types.NewCryptoAmountFromInt64(1_000_000), // 1 USDT
				PercentBP: 100,                                       // 1%
				MinFee:    types.NewCryptoAmountFromInt64(5_000_000), // 5 USDT min
			},
			amount: 50_000_000, // 50 USDT → 1 + 0.5 = 1.5 < 5
			want:   5_000_000,  // min kicks in
		},
		{
			give: "min floor does not apply: calculated >= minFee",
			fee: EffectiveFee{
				PercentBP: 100,                                       // 1%
				MinFee:    types.NewCryptoAmountFromInt64(2_000_000), // 2 USDT min
			},
			amount: 500_000_000, // 500 USDT → 5 USDT > 2 USDT min
			want:   5_000_000,
		},
		{
			give: "max cap applies: calculated > maxFee",
			fee: EffectiveFee{
				PercentBP: 50, // 0.5%
				MaxFee:    types.NewCryptoAmountFromInt64(10_000_000), // 10 USDT max
			},
			amount: 5_000_000_000, // 5000 USDT → 25 USDT > 10 max
			want:   10_000_000,     // cap kicks in
		},
		{
			give: "max cap does not apply: calculated <= maxFee",
			fee: EffectiveFee{
				PercentBP: 50,                                         // 0.5%
				MaxFee:    types.NewCryptoAmountFromInt64(100_000_000), // 100 USDT max
			},
			amount: 500_000_000, // 500 USDT → 2.5 USDT < 100 max
			want:   2_500_000,
		},
		{
			give: "both min and max: min applies",
			fee: EffectiveFee{
				PercentBP: 10,                                        // 0.1%
				MinFee:    types.NewCryptoAmountFromInt64(3_000_000), // 3 USDT min
				MaxFee:    types.NewCryptoAmountFromInt64(50_000_000), // 50 USDT max
			},
			amount: 10_000_000, // 10 USDT → 0.01 USDT < 3 min
			want:   3_000_000,
		},
		{
			give: "both min and max: max applies",
			fee: EffectiveFee{
				PercentBP: 500,                                        // 5%
				MinFee:    types.NewCryptoAmountFromInt64(1_000_000),  // 1 USDT min
				MaxFee:    types.NewCryptoAmountFromInt64(20_000_000), // 20 USDT max
			},
			amount: 1_000_000_000, // 1000 USDT → 50 USDT > 20 max
			want:   20_000_000,
		},
		{
			give: "both min and max: in range, neither applies",
			fee: EffectiveFee{
				PercentBP: 100,                                        // 1%
				MinFee:    types.NewCryptoAmountFromInt64(1_000_000),  // 1 USDT min
				MaxFee:    types.NewCryptoAmountFromInt64(50_000_000), // 50 USDT max
			},
			amount: 500_000_000, // 500 USDT → 5 USDT (in [1, 50])
			want:   5_000_000,
		},
		{
			give: "maxFee=0 means no cap",
			fee: EffectiveFee{
				PercentBP: 1000, // 10%
			},
			amount: 10_000_000_000, // 10000 USDT → 1000 USDT
			want:   1_000_000_000,
		},
		{
			give: "minFee=0 means no floor",
			fee: EffectiveFee{
				PercentBP: 1, // 0.01%
			},
			amount: 1_000_000, // 1 USDT → 100 sun (0.0001 USDT)
			want:   100,
		},
		{
			give: "full compound with clamp: fixed 1 + 1% on 50 USDT, min 2",
			fee: EffectiveFee{
				FixedFee:  types.NewCryptoAmountFromInt64(1_000_000),
				PercentBP: 100,
				MinFee:    types.NewCryptoAmountFromInt64(2_000_000),
			},
			amount: 50_000_000, // 50 USDT → 1 + 0.5 = 1.5 < 2 → min=2
			want:   2_000_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			amount := types.NewCryptoAmountFromInt64(tt.amount)
			got := tt.fee.Calculate(amount)
			want := types.NewCryptoAmountFromInt64(tt.want)

			if got.Cmp(want) != 0 {
				t.Errorf("Calculate(%d) = %s, want %s", tt.amount, got.String(), want.String())
			}
		})
	}
}

func TestEffectiveFee_IsZero(t *testing.T) {
	tests := []struct {
		give string
		fee  EffectiveFee
		want bool
	}{
		{
			give: "all zero",
			fee:  EffectiveFee{},
			want: true,
		},
		{
			give: "has percent",
			fee:  EffectiveFee{PercentBP: 100},
			want: false,
		},
		{
			give: "has fixed",
			fee:  EffectiveFee{FixedFee: types.NewCryptoAmountFromInt64(1)},
			want: false,
		},
		{
			give: "has minFee only",
			fee:  EffectiveFee{MinFee: types.NewCryptoAmountFromInt64(1000)},
			want: false,
		},
		{
			give: "has maxFee only — still zero (maxFee doesn't generate fee)",
			fee:  EffectiveFee{MaxFee: types.NewCryptoAmountFromInt64(1000)},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			if got := tt.fee.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidFeeDirection(t *testing.T) {
	tests := []struct {
		give FeeDirection
		want bool
	}{
		{FeeDirectionProcessing, true},
		{FeeDirectionWithdrawal, true},
		{FeeDirectionPayout, true},
		{FeeDirectionSettlement, true},
		{FeeDirectionRefund, true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := IsValidFeeDirection(tt.give); got != tt.want {
			t.Errorf("IsValidFeeDirection(%q) = %v, want %v", tt.give, got, tt.want)
		}
	}
}
