package evm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeAddressEIP55(t *testing.T) {
	want := "0xCaC524BcA292aaade2DF8A05cC58F0a65B1B3bB9"
	for _, input := range []string{
		"0xcac524bca292aaade2df8a05cc58f0a65b1b3bb9",
		"0xCAC524BCA292AAADE2DF8A05CC58F0A65B1B3BB9",
		want,
	} {
		got, err := NormalizeAddress(input)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	require.Error(t, ValidateAddress("0xCaC524BcA292aaade2DF8A05cC58F0a65B1B3bB8"))
	require.Error(t, ValidateAddress("0x0000000000000000000000000000000000000000"))
	require.Error(t, ValidateAddress(" 0xcac524bca292aaade2df8a05cc58f0a65b1b3bb9"))
}
