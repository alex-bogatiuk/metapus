package printing

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/types"
)

func TestFormatCryptoUnitsIsExactForTheTokenScale(t *testing.T) {
	tests := []struct {
		give     string
		decimals int
		want     string
	}{
		{give: "1500000", decimals: 6, want: "1,50"},
		{give: "1234567", decimals: 6, want: "1,234567"},
		{give: "100500000", decimals: 6, want: "100,50"},
		{give: "0", decimals: 6, want: "0,00"},
		{give: "15", decimals: 1, want: "1,5"},
		{give: "1234567", decimals: 0, want: "1 234 567"},
		// Beyond int64 and float64 precision: 123456789012.345678901234567891 ETH.
		{give: "123456789012345678901234567891", decimals: 18, want: "123 456 789 012,345678901234567891"},
		{give: "5", decimals: -1, want: "—"},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, FormatCryptoUnits(types.MustCryptoAmount(tt.give), tt.decimals), "%s/%d", tt.give, tt.decimals)
	}
}
