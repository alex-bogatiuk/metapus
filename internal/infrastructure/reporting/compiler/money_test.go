package compiler

import (
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"math"
	"math/big"
	"testing"
)

func TestNormalizeReportMoneyNeverRounds(t *testing.T) {
	for _, text := range []string{"9007199254740993", "9223372036854775807", "-9223372036854775808"} {
		n, ok := new(big.Int).SetString(text, 10)
		require.True(t, ok)
		actual, err := normalizeMoney(pgtype.Numeric{Int: n, Valid: true})
		require.NoError(t, err)
		require.Equal(t, text, actual)
	}
	for _, value := range []any{float64(1), pgtype.Numeric{Int: big.NewInt(15), Exp: -1, Valid: true}, pgtype.Numeric{Int: big.NewInt(math.MaxInt64), Exp: 1, Valid: true}, pgtype.Numeric{NaN: true, Valid: true}} {
		_, err := normalizeMoney(value)
		require.Error(t, err)
	}
}

func TestNormalizeCryptoReportMoneyKeepsItsSeparateArbitraryPrecisionContract(t *testing.T) {
	for _, text := range []string{"0", "9223372036854775808", "115792089237316195423570985008687907853269984665640564039457584007913129639935"} {
		n, ok := new(big.Int).SetString(text, 10)
		require.True(t, ok)
		actual, err := normalizeCryptoMoney(pgtype.Numeric{Int: n, Valid: true})
		require.NoError(t, err)
		require.Equal(t, text, actual)
	}
	for _, value := range []any{float64(1), int64(-1), pgtype.Numeric{Int: big.NewInt(15), Exp: -1, Valid: true}, pgtype.Numeric{NaN: true, Valid: true}} {
		_, err := normalizeCryptoMoney(value)
		require.Error(t, err)
	}
}
