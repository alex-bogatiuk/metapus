package crypto_repo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Stand: 0x6F0A… was accepted into a TRON whitelist as TL6LqU….
func TestTronDestinationAcceptsOnlyBase58(t *testing.T) {
	normalized, err := normalizeTronDestination("TYVdZHXMnBxxhwr9tPqrZH8LrLd11sCoNM")
	require.NoError(t, err)
	require.Equal(t, "TYVdZHXMnBxxhwr9tPqrZH8LrLd11sCoNM", normalized)

	_, err = normalizeTronDestination("0x6F0Ab5AA45Cc5ee5449C6960f8EDabA678E6e9fd")
	require.ErrorContains(t, err, "Ethereum address")
	_, err = normalizeTronDestination("41f713be7dc86b27a261d845644963a75ade8f4858")
	require.ErrorContains(t, err, "starts with T")
	_, err = normalizeTronDestination("TXXXinvalidaddress")
	require.ErrorContains(t, err, "invalid TRON address")
}
