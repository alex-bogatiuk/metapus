package cryptodto

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/blockchain_network"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
)

func TestLegacyNetworkUpdateDoesNotReenableFileCredentials(t *testing.T) {
	n := blockchain_network.NewBlockchainNetwork("ETH", "Ethereum", crypto.ChainFamilyEVM, "1", "ETH", 18)
	n.RPCConfigSource = "database"
	request := UpdateBlockchainNetworkRequest{}
	request.ApplyTo(n)
	require.Equal(t, "database", n.RPCConfigSource)
}
