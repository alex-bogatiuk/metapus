package cryptodto

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/domain/catalogs/blockchain_network"
	"metapus/internal/domain/crypto"
)

func TestLegacyNetworkUpdateDoesNotReenableFileCredentials(t *testing.T) {
	n := blockchain_network.NewBlockchainNetwork("ETH", "Ethereum", crypto.ChainFamilyEVM, "1", "ETH", 18)
	n.RPCConfigSource = "database"
	request := UpdateBlockchainNetworkRequest{}
	request.ApplyTo(n)
	require.Equal(t, "database", n.RPCConfigSource)
}
